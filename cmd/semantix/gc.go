package main

import (
	"flag"
	"fmt"
	"io"
	"math"

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
		Params: slice.ScoreParams{
			HalfLifeDays: cfgFloat(deps.resolved, "score.half_life_days", 30),
			GraceDays:    cfgFloat(deps.resolved, "score.grace_days", 7),
		},
		ArchivePath: archivePath,
	})
	if err != nil {
		if *jsonOutput {
			return failJSON(stdout, "gc", err)
		}
		return err
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
			"capacity":        res.Capacity,
			"archived":        res.Archived,
			"weights_updated": res.RescoredWeights,
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
