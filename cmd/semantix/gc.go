package main

import (
	"flag"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"semantix/kernel/slice"
)

// runGC is the scoring + eviction pass over the slice library (U26 + 架构
// §3.2 ④⑤):
//   - --retention-days N: remove slices created more than N days ago
//     (slices with unknown creation time are never expired by retention)
//   - --min-weight W:     remove slices whose value weight is below W
//   - --max-slices M:     cap the library; worst-scored slices are evicted
//     down to M (default from config store.max_slices; 0 disables)
//   - --no-rescore:       skip recomputing value weights before the pass
//   - --no-archive:       hard-delete instead of archiving evictions to
//     <db>.archive.jsonl (the archive restores via `semantix import`)
//   - --dry-run:          report the full plan without persisting anything
//
// Bare `gc` removes nothing by criteria but still rescores weights, applies
// the configured cap and folds the journal (downgrade path). Exit code
// follows U19 §4.3.
func runGC(args []string, stdout, stderr io.Writer, deps dependencies) error {
	flags := flag.NewFlagSet("gc", flag.ContinueOnError)
	flags.SetOutput(stderr)
	retentionDays := flags.Int("retention-days", 0, "remove slices older than N days (0 disables)")
	minWeight := flags.Float64("min-weight", 0, "remove slices with weight below W (0 disables)")
	maxSlices := flags.Int("max-slices", cfgInt(deps.resolved, "store.max_slices", 5000), "library size cap (0 disables)")
	noRescore := flags.Bool("no-rescore", false, "skip recomputing value weights")
	noArchive := flags.Bool("no-archive", false, "hard-delete evictions instead of archiving")
	dryRun := flags.Bool("dry-run", false, "report the plan without persisting")
	consolidateCtx := flags.Bool("consolidate-context", false, "merge near-duplicate Context slices into union cards (runs with GC; library-maintenance-time only)")
	consolidateThreshold := flags.Float64("consolidate-threshold", 0.6, "token-set Jaccard above which Context slices are near-duplicates")
	scoreParamsPath := flags.String("score-params", "", "load the full ScoreParams from a JSON snapshot (offline fitter write-back; overrides [score] config keys)")
	dbOverride := flags.String("db", cfgString(deps.resolved, "store.db", ""), "database path override")
	jsonOutput := flags.Bool("json", false, "write JSON envelope output")
	if err := flags.Parse(args); err != nil {
		// Parse failures (unknown flag, bad value) are usage errors: exit 2
		// per U19 §4.3. ErrHelp unwraps to flag.ErrHelp so run() still exits 0.
		return usageError{err: err}
	}
	if flags.NArg() != 0 {
		return usageErrf("gc: unexpected arguments: %v", flags.Args())
	}
	if *retentionDays < 0 {
		return usageErrf("gc: --retention-days must be >= 0 (got %d)", *retentionDays)
	}
	// NaN/Inf would silently disable the criterion; validate like the zone
	// flags in flags.go (NaN comparisons are always false).
	if math.IsNaN(*minWeight) || math.IsInf(*minWeight, 0) || *minWeight < 0 {
		return usageErrf("gc: --min-weight must be a finite value >= 0 (got %v)", *minWeight)
	}
	if *maxSlices < 0 {
		return usageErrf("gc: --max-slices must be >= 0 (got %d)", *maxSlices)
	}

	params := slice.ScoreParams{
		HalfLifeDays: cfgFloat(deps.resolved, "score.half_life_days", 30),
		GraceDays:    cfgFloat(deps.resolved, "score.grace_days", 7),
	}
	if *scoreParamsPath != "" {
		p, perr := loadScoreParams(*scoreParamsPath)
		if perr != nil {
			// A broken trainer snapshot must fail loudly, not silently fall
			// back to defaults (the fitter's gate depends on this).
			return usageErrf("gc: --score-params: %v", perr)
		}
		params = p
	}

	db := storePath(*dbOverride, "project")
	store, err := deps.openStore(db)
	if err != nil {
		if *jsonOutput {
			return failJSON(stdout, "gc", err)
		}
		return err
	}
	defer closeStore(store)

	archivePath := ""
	if !*noArchive {
		archivePath = db + ".archive.jsonl"
	}
	res, err := slice.GC(store, slice.GCOptions{
		RetentionDays: *retentionDays,
		MinWeight:     *minWeight,
		DryRun:        *dryRun,
		MaxSlices:     *maxSlices,
		Rescore:       !*noRescore,
		Params:        params,
		ArchivePath:   archivePath,
	})
	if err != nil {
		if *jsonOutput {
			return failJSON(stdout, "gc", err)
		}
		return err
	}
	// Context consolidation (W4): fold near-duplicate Context slices into
	// union cards. Runs inside the GC maintenance window — never mid-flight
	// (injection byte-stability); DryRun mirrors the GC plan semantics.
	var cres slice.ConsolidateResult
	if *consolidateCtx {
		cres, err = slice.ConsolidateContext(store, slice.ConsolidateOptions{
			Threshold: *consolidateThreshold,
			DryRun:    *dryRun,
		})
		if err != nil {
			if *jsonOutput {
				return failJSON(stdout, "gc", err)
			}
			return err
		}
	}
	if *jsonOutput {
		expired, lowScore, overCap := res.Expired, res.LowScore, res.OverCap
		if expired == nil {
			expired = []string{}
		}
		if lowScore == nil {
			lowScore = []string{}
		}
		if overCap == nil {
			overCap = []string{}
		}
		data := map[string]interface{}{
			"checked":         res.Checked,
			"removed":         res.Removed,
			"dry_run":         *dryRun,
			"expired":         expired,
			"low_score":       lowScore,
			"evicted":         overCap,
			"evicted_by_type": res.EvictedByType, // nil → null when no cap eviction
			"capacity":        res.Capacity,
			"archived":        res.Archived,
			"weights_updated": res.RescoredWeights,
		}
		if *consolidateCtx {
			data["context_groups"] = cres.Groups
			data["context_merged"] = cres.Merged
			data["context_created"] = cres.Created
			data["context_removed"] = cres.Removed
		}
		return writeEnvelope(stdout, "gc", data)
	}
	if *dryRun {
		fmt.Fprintf(stdout, "gc: dry-run checked=%d would_remove=%d capacity=%d weights_updated=%d\n",
			res.Checked, res.Removed, res.Capacity, res.RescoredWeights)
	} else {
		fmt.Fprintf(stdout, "gc: checked=%d removed=%d capacity=%d archived=%d weights_updated=%d\n",
			res.Checked, res.Removed, res.Capacity, res.Archived, res.RescoredWeights)
	}
	if *consolidateCtx {
		fmt.Fprintf(stdout, "gc: context groups=%d merged=%d\n", cres.Groups, cres.Merged)
		for _, id := range cres.Created {
			fmt.Fprintf(stdout, "  merged -> %s\n", id)
		}
	}
	if len(res.EvictedByType) > 0 {
		keys := make([]string, 0, len(res.EvictedByType))
		for k := range res.EvictedByType {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s:%d", k, res.EvictedByType[k]))
		}
		fmt.Fprintf(stdout, "  by_type %s\n", strings.Join(parts, ","))
	}
	for _, id := range res.Expired {
		fmt.Fprintf(stdout, "  expired  %s\n", id)
	}
	for _, id := range res.LowScore {
		fmt.Fprintf(stdout, "  low-score %s\n", id)
	}
	for _, id := range res.OverCap {
		fmt.Fprintf(stdout, "  over-cap %s\n", id)
	}
	return nil
}
