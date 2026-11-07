package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

type searchResult struct {
	ID      string  `json:"id"`
	Type    int     `json:"type"`
	Scope   string  `json:"scope"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
	Zone    string  `json:"zone"`
	// SourceSession is provenance shown in the text output only (U30);
	// json:"-" keeps search --json output byte-identical (backward compat).
	SourceSession string `json:"-"`
}

func runSearch(args []string, stdout, stderr io.Writer, deps dependencies) error {
	flags := flag.NewFlagSet("search", flag.ContinueOnError)
	flags.SetOutput(stderr)
	queryFlag := flags.String("query", "", "search query (alternatively pass it as positional text)")
	scopeValue := flags.String("scope", cfgString(deps.resolved, "store.scope", "project"), "slice scope: session, project, or user")
	limit := flags.Int("limit", cfgInt(deps.resolved, "retrieval.limit", 10), "maximum number of results")
	dbOverride := flags.String("db", "", "database path override")
	projectDB := flags.String("project-db", cfgString(deps.resolved, "store.db", defaultProjectDB()), "project/session database path")
	userDB := flags.String("user-db", defaultUserDB(), "user database path")
	jsonOutput := flags.Bool("json", false, "write JSON envelope")
	rf := addRetrieverFlags(flags, deps, "retrieval.retriever", "hybrid")
	zf := addZoneFlags(flags)
	if err := flags.Parse(args); err != nil {
		return usageWrap(err)
	}
	if err := zf.applyConfigZones(flags, deps.resolved); err != nil {
		return usagef("%v", err)
	}
	if err := zf.validate(); err != nil {
		return usagef("%v", err)
	}
	if *limit <= 0 {
		return usagef("--limit must be greater than zero")
	}
	opt, err := rf.options(flags, deps)
	if err != nil {
		return err
	}

	query := strings.TrimSpace(*queryFlag)
	if query == "" {
		query = strings.TrimSpace(strings.Join(flags.Args(), " "))
	} else if flags.NArg() != 0 {
		return usagef("use either --query or positional query text, not both")
	}
	if query == "" {
		return usagef("query is required")
	}

	scope, err := parseScope(*scopeValue)
	if err != nil {
		return err
	}
	dbPath := selectConfiguredDB(flags, deps.resolved, scope, *dbOverride, *projectDB, *userDB)
	store, err := deps.openStore(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	if store == nil {
		return errors.New("slice store is unavailable; merge the U4 implementation first")
	}
	defer closeStore(store)

	items, err := store.List(scope)
	if err != nil {
		return fmt.Errorf("list %s slices: %w", scopeName(scope), err)
	}
	index := deps.newIndex()
	if index == nil {
		return errors.New("search index is unavailable")
	}
	for i, item := range items {
		if item == nil {
			return fmt.Errorf("store returned nil slice at position %d", i)
		}
		if err := index.Insert(item); err != nil {
			return fmt.Errorf("index slice %q: %w", item.ID, err)
		}
	}

	hits, err := retrieveHits(store, items, index, query, *limit, scope, opt, stderr)
	if err != nil {
		return err
	}
	results := make([]searchResult, len(hits))
	top1 := 0.0
	if len(hits) > 0 {
		top1 = hits[0].Score
	}
	zones := zf.zones()
	for i, hit := range hits {
		results[i] = searchResult{
			ID:            hit.Slice.ID,
			Type:          int(hit.Slice.Type),
			Scope:         scopeName(hit.Slice.Scope),
			Content:       string(hit.Slice.Content),
			Score:         hit.Score,
			Zone:          zones.ClassifyHit(hit, top1).String(),
			SourceSession: hit.Slice.Meta.SourceSession,
		}
	}

	if *jsonOutput {
		return writeJSON(stdout, okEnvelope("search", results))
	}
	rows := make([]hitRow, len(results))
	for i, result := range results {
		content := stripESC(strings.Join(strings.Fields(result.Content), " "))
		fmt.Fprintf(stdout, "%s\n   %s\n", formatHitLine(i+1, verdictIcon(result.Zone), fmt.Sprintf("%.6f", result.Score), result.Zone, result.ID, result.Scope, result.SourceSession), content)
		rows[i] = hitRow{Zone: result.Zone, SourceSession: result.SourceSession}
	}
	writeHitsSummary(stdout, rows)
	return nil
}
