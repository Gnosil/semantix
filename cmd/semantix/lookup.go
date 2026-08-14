package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"semantix/kernel/inject"
	"semantix/kernel/lookup"
	"semantix/kernel/slice"
)

// runLookup exposes the semantix_lookup tool contract over the CLI
// (U8 tool side): given a query, print top-k reuse slices as JSON.
func runLookup(args []string, stdout, stderr io.Writer, deps dependencies) error {
	cfgPath, cfgExplicit := explicitConfigPath(args, defaultGetenv)
	cfg, err := loadConfigFor(cfgPath, cfgExplicit, defaultGetenv)
	if err != nil {
		return err
	}

	flags := flag.NewFlagSet("lookup", flag.ContinueOnError)
	flags.SetOutput(stderr)
	query := flags.String("query", "", "task description to match against stored slices")
	scopeValue := flags.String("scope", cfg.Store.Scope, "slice scope: session, project, or user")
	limit := flags.Int("limit", cfg.Retrieval.LookupLimit, "maximum number of slices (capped at 50)")
	dbOverride := flags.String("db", "", "database path override")
	// Accepted for compatibility with the harness tool contract
	// (semantix_lookup calls `semantix lookup --json`); output is JSON anyway.
	_ = flags.Bool("json", false, "output as JSON (default output is already JSON)")
	_ = flags.String("config", cfgPath, "config file path (default ./semantix.toml)")
	zf := addZoneFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := zf.validate(); err != nil {
		return err
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
		return fmt.Errorf("lookup: --query is required")
	}

	store, err := deps.openStore(storePath(*dbOverride, *scopeValue, cfg.Store.DB))
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
			ID:      h.Slice.ID,
			Type:    h.Slice.Type.String(),
			Scope:   h.Slice.Scope.String(),
			Score:   h.Score,
			Zone:    zones.Classify(h.Score, top1).String(),
			Content: string(h.Slice.Content),
		})
	}
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// runInject assembles an L2 injection block for a query (U8 compose side):
// top-k reuse slices in canonical order, budget-capped, marker-wrapped.
func runInject(args []string, stdout, stderr io.Writer, deps dependencies) error {
	cfgPath, cfgExplicit := explicitConfigPath(args, defaultGetenv)
	cfg, err := loadConfigFor(cfgPath, cfgExplicit, defaultGetenv)
	if err != nil {
		return err
	}

	flags := flag.NewFlagSet("inject", flag.ContinueOnError)
	flags.SetOutput(stderr)
	query := flags.String("query", "", "current user turn to match against stored slices")
	scopeValue := flags.String("scope", cfg.Store.Scope, "slice scope: session, project, or user")
	k := flags.Int("k", cfg.Inject.TopK, "top-k slices to consider")
	budget := flags.Int("budget", cfg.Inject.Budget, "max injection block bytes")
	dbOverride := flags.String("db", "", "database path override")
	_ = flags.String("config", cfgPath, "config file path (default ./semantix.toml)")
	zf := addZoneFlags(flags)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := zf.validate(); err != nil {
		return err
	}
	if *query == "" && flags.NArg() > 0 {
		*query = flags.Arg(0)
	}
	if *query == "" {
		return fmt.Errorf("inject: --query is required")
	}

	store, err := deps.openStore(storePath(*dbOverride, *scopeValue, cfg.Store.DB))
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
	fmt.Fprintf(stdout, "%s\n", stripESC(inj.Text))
	return nil
}

// storePath resolves the store path for a scope. projectDefault comes from
// config (store.db); user scope keeps the built-in user library path.
func storePath(override, scope, projectDefault string) string {
	if override != "" {
		return override
	}
	if scope == "user" {
		return defaultUserDB()
	}
	return projectDefault
}

// indexFromStore rebuilds an in-memory index from the persistent store,
// covering every scope (session/project/user).
func indexFromStore(store slice.Store, idx slice.Index) error {
	for _, scope := range []slice.Scope{slice.Session, slice.Project, slice.User} {
		items, err := store.List(scope)
		if err != nil {
			return err
		}
		for _, sl := range items {
			if err := idx.Insert(sl); err != nil {
				return err
			}
		}
	}
	return nil
}
