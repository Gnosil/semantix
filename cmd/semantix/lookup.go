package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"semantix/kernel/inject"
	"semantix/kernel/lookup"
	"semantix/kernel/slice"
)

// runLookup exposes the semantix_lookup tool contract over the CLI
// (U8 tool side): given a query, print top-k reuse slices as JSON.
func runLookup(args []string, stdout, stderr io.Writer, deps dependencies) error {
	flags := flag.NewFlagSet("lookup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	query := flags.String("query", "", "task description to match against stored slices")
	scopeValue := flags.String("scope", cfgString(deps.resolved, "store.scope", "project"), "slice scope: session, project, or user")
	limit := flags.Int("limit", cfgInt(deps.resolved, "retrieval.limit", 5), "maximum number of slices (capped at 50)")
	dbOverride := flags.String("db", cfgString(deps.resolved, "store.db", ""), "database path override")
	// H1 harness protocol entry point (semantix_lookup calls
	// `semantix lookup --json`): the envelope is the machine-readable form.
	// Without --json the legacy bare-array output is kept for compat.
	jsonOut := flags.Bool("json", false, "output as JSON envelope (H1 protocol)")
	zf := addZoneFlags(flags)
	if err := flags.Parse(args); err != nil {
		return usageWrap(err)
	}
	if err := zf.validate(); err != nil {
		return usagef("%v", err)
	}
	if *limit > 50 { // match kernel lookup.Execute hard cap
		*limit = 50
	}
	if *limit <= 0 { // kernel Execute treats <=0 as default
		*limit = 5
	}
	if *query == "" && flags.NArg() > 0 {
		*query = flags.Arg(0)
	}
	if *query == "" {
		return usagef("lookup: --query is required")
	}

	store, err := deps.openStore(storePath(*dbOverride, *scopeValue))
	if err != nil {
		return err
	}
	idx := deps.newIndex()
	// index is session-local: rebuild from the store so lookup sees
	// previously extracted slices.
	if err := indexFromStore(store, idx); err != nil {
		return err
	}

	scope, err := parseScope(*scopeValue)
	if err != nil {
		return err
	}
	hits, err := idx.Search(*query, *limit, scope)
	if err != nil {
		return err
	}
	top1 := 0.0
	if len(hits) > 0 {
		top1 = hits[0].Score
	}
	zones := zf.zones()
	out := make([]lookup.Result, 0, len(hits))
	for _, h := range hits {
		out = append(out, lookup.Result{
			ID:            h.Slice.ID,
			Type:          h.Slice.Type.String(),
			Scope:         h.Slice.Scope.String(),
			Score:         h.Score,
			Zone:          zones.Classify(h.Score, top1).String(),
			Content:       string(h.Slice.Content),
			SourceSession: h.Slice.Meta.SourceSession,
		})
	}
	// Reuse accounting (评分器原料，spec slice-value-eviction §3): only clear
	// hits count — grey/miss are not reuse signals, crediting them would
	// reward weakly-related slices. Best-effort: stats must never fail a
	// read path.
	hitDeltas := map[string]slice.SliceStats{}
	nowUnix := time.Now().Unix()
	for _, r := range out {
		if r.Zone == "hit" {
			hitDeltas[r.ID] = slice.SliceStats{Hits: 1, LastUsed: nowUnix}
		}
	}
	if err := slice.ApplyStats(store, hitDeltas); err != nil {
		fmt.Fprintf(stderr, "semantix: stats write-back: %v\n", err)
	}
	if *jsonOut {
		return writeJSON(stdout, okEnvelope("lookup", out))
	}
	// 默认（无 --json）为 vibe-coder 可读文本（U30）：zone 图标 + 来源会话 +
	// 命中摘要行。机器可读形态是 --json 信封（H1 协议入口）。
	rows := make([]hitRow, len(out))
	for i, r := range out {
		content := stripESC(strings.Join(strings.Fields(r.Content), " "))
		fmt.Fprintf(stdout, "%s\n   %s\n", formatHitLine(i+1, verdictIcon(r.Zone), fmt.Sprintf("%.6f", r.Score), r.Zone, r.ID, r.Scope, r.SourceSession), content)
		rows[i] = hitRow{Zone: r.Zone, SourceSession: r.SourceSession}
	}
	writeHitsSummary(stdout, rows)
	return nil
}

// runInject assembles an L2 injection block for a query (U8 compose side):
// top-k reuse slices in canonical order, budget-capped, marker-wrapped.
func runInject(args []string, stdout, stderr io.Writer, deps dependencies) error {
	flags := flag.NewFlagSet("inject", flag.ContinueOnError)
	flags.SetOutput(stderr)
	query := flags.String("query", "", "current user turn to match against stored slices")
	scopeValue := flags.String("scope", cfgString(deps.resolved, "store.scope", "project"), "slice scope: session, project, or user")
	k := flags.Int("k", cfgInt(deps.resolved, "retrieval.limit", 5), "top-k slices to consider")
	budget := flags.Int("budget", cfgInt(deps.resolved, "inject.budget", inject.DefaultBudget), "max injection block bytes")
	dbOverride := flags.String("db", cfgString(deps.resolved, "store.db", ""), "database path override")
	zf := addZoneFlags(flags)
	if err := flags.Parse(args); err != nil {
		return usageWrap(err)
	}
	if err := zf.validate(); err != nil {
		return usagef("%v", err)
	}
	if *query == "" && flags.NArg() > 0 {
		*query = flags.Arg(0)
	}
	if *query == "" {
		return usagef("inject: --query is required")
	}

	store, err := deps.openStore(storePath(*dbOverride, *scopeValue))
	if err != nil {
		return err
	}
	idx := deps.newIndex()
	if err := indexFromStore(store, idx); err != nil {
		return err
	}
	scope, err := parseScope(*scopeValue)
	if err != nil {
		return err
	}

	z := zf.zones()
	inj, err := (&inject.Injector{Index: idx, Scope: scope, K: *k, Budget: *budget, Zones: &z}).Build(*query)
	if err != nil {
		return err
	}
	// Injection accounting stays on the caller side — Injector.Build itself
	// must remain read-only (kernel decision chain has no side effects).
	if inj != nil && len(inj.Slices) > 0 {
		deltas := make(map[string]slice.SliceStats, len(inj.Slices))
		nowUnix := time.Now().Unix()
		for _, sl := range inj.Slices {
			deltas[sl.ID] = slice.SliceStats{Injected: 1, LastUsed: nowUnix}
		}
		if err := slice.ApplyStats(store, deltas); err != nil {
			fmt.Fprintf(stderr, "semantix: stats write-back: %v\n", err)
		}
	}
	fmt.Fprintf(stdout, "%s\n", stripESC(inj.Text))
	return nil
}

// storePath resolves the store path for a scope (mirrors search.go).
func storePath(override, scope string) string {
	if override != "" {
		return override
	}
	switch scope {
	case "session":
		return defaultProjectDB()
	case "user":
		return defaultUserDB()
	default:
		return defaultProjectDB()
	}
}

// indexFromStore rebuilds an in-memory index from the persistent store,
// covering every scope. One ListAll pass instead of a List per scope: each
// List used to re-read and re-parse the whole file, tripling open cost; the
// index buckets DF/avgdl by Slice.Scope itself, so mixed-order insertion is
// equivalent.
func indexFromStore(store slice.Store, idx slice.Index) error {
	items, err := store.ListAll()
	if err != nil {
		return err
	}
	for _, sl := range items {
		if err := idx.Insert(sl); err != nil {
			return err
		}
	}
	return nil
}
