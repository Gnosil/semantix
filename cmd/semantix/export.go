package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"semantix/kernel/slice"
)

// runExport writes the whole library as JSONL — the sanctioned full read
// for offline consumers (the local retrieval model trainer, backups):
// Get/List/ListAll strip embeddings by contract, while slice.Export keeps
// raw vector bytes intact and replays the journal through the store open.
// Mirror image of `semantix import`; the round-trip is lossless.
func runExport(args []string, stdout io.Writer, deps dependencies) int {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	out := fs.String("out", "", "output JSONL path (default: stdout)")
	db := fs.String("db", "", "database path override (default: project db)")
	projectDB := fs.String("project-db", cfgString(deps.resolved, "store.db", defaultProjectDB()), "project/session database path")
	userDB := fs.String("user-db", defaultUserDB(), "user database path")
	scopeName := fs.String("scope", "project", "slice scope: session, project, or user")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, "export:", err)
		return 2
	}
	scope, err := parseScope(*scopeName)
	if err != nil {
		fmt.Fprintf(stdout, "export: %v\n", err)
		return 2
	}
	dbPath := selectDB(scope, *db, *projectDB, *userDB)
	store, err := deps.openStore(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "export: open store: %v\n", err)
		return 1
	}
	if store == nil {
		fmt.Fprintln(os.Stderr, "export: slice store is unavailable")
		return 1
	}
	defer closeStore(store)

	var w io.Writer = stdout
	if *out != "" {
		f, err := os.OpenFile(*out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			fmt.Fprintf(os.Stderr, "export: create %s: %v\n", *out, err)
			return 1
		}
		defer f.Close()
		w = f
	}
	count, skipped, err := slice.Export(store, w)
	if err != nil {
		fmt.Fprintf(os.Stderr, "export: %v\n", err)
		return 1
	}
	if *out != "" {
		fmt.Fprintf(stdout, "export: exported=%d skipped=%d -> %s\n", count, skipped, *out)
	} else if skipped > 0 {
		fmt.Fprintf(os.Stderr, "export: exported=%d skipped=%d\n", count, skipped)
	}
	return 0
}
