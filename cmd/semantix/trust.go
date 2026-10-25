package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"semantix/kernel/audit"
	"semantix/kernel/slice"
)

// runTrust upgrades a slice's provenance tag (Issue #279): only upgrades
// are allowed (target level must be higher than the current level), the
// action is written to the audit log, and the slice regains injection /
// L3 pass-through eligibility under the configured floor.
func runTrust(args []string, stdout io.Writer, deps dependencies) int {
	fs := flag.NewFlagSet("trust", flag.ContinueOnError)
	db := fs.String("db", "", "database path override (default: project db)")
	projectDB := fs.String("project-db", cfgString(deps.resolved, "store.db", defaultProjectDB()), "project/session database path")
	userDB := fs.String("user-db", defaultUserDB(), "user database path")
	scopeName := fs.String("scope", cfgString(deps.resolved, "store.scope", "project"), "slice scope: session, project, or user")
	originName := fs.String("origin", string(slice.OriginUserCurated), "target origin: user-curated (default) | session-auto | prefetch")
	auditDB := fs.String("audit-db", filepath.Join(".semantix", "audit.jsonl"), "audit log path")
	jsonOut := fs.Bool("json", false, "output as JSON envelope")
	// Go's flag package stops at the first non-flag argument; the slice id
	// is positional per Issue #279 (`trust <id> [--flags]`), so move
	// positional arguments to the end before parsing.
	args = reorderPositional(args)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0 // --help is a successful request
		}
		fmt.Fprintln(os.Stderr, "trust:", err)
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stdout, "Usage: semantix trust <slice-id> [--origin user-curated] [--db <path>]")
		return 2
	}
	id := fs.Arg(0)
	target := slice.Origin(*originName)
	if !target.Valid() {
		fmt.Fprintf(stdout, "trust: invalid --origin %q (want user-curated, session-auto, or prefetch)\n", *originName)
		return 2
	}
	scope, err := parseScope(*scopeName)
	if err != nil {
		fmt.Fprintf(stdout, "trust: %v\n", err)
		return 2
	}
	dbPath := selectConfiguredDB(fs, deps.resolved, scope, *db, *projectDB, *userDB)
	store, err := deps.openStore(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trust: open store: %v\n", err)
		return 1
	}
	if store == nil {
		fmt.Fprintln(os.Stderr, "trust: slice store is unavailable")
		return 1
	}
	defer closeStore(store)

	sl, err := store.Get(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trust: get %s: %v\n", id, err)
		return 1
	}
	if sl == nil {
		fmt.Fprintf(stdout, "trust: slice %q not found\n", id)
		return 1
	}
	from := sl.Meta.Origin
	if target.Level() <= from.Level() {
		// Only upgrades are meaningful (Issue #279): a downgrade has no
		// security value and would silently undo a previous decision.
		fmt.Fprintf(stdout, "trust: %q is already at level %d (origin %q); only upgrades are allowed\n",
			id, from.Level(), from)
		return 2
	}
	sl.Meta.Origin = target
	if err := store.Put(sl); err != nil {
		fmt.Fprintf(os.Stderr, "trust: put %s: %v\n", id, err)
		return 1
	}

	rec, err := audit.NewRecorder(*auditDB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trust: audit: %v\n", err)
		return 1
	}
	if err := rec.Trust(id, from, target); err != nil {
		fmt.Fprintf(os.Stderr, "trust: audit write: %v\n", err)
		return 1
	}

	if *jsonOut {
		data := map[string]any{
			"slice_id":    id,
			"from_origin": string(from),
			"to_origin":   string(target),
			"audit":       *auditDB,
		}
		if err := writeJSON(stdout, okEnvelope("trust", data)); err != nil {
			fmt.Fprintln(os.Stderr, "trust:", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "trust: %s %s -> %s (audit: %s)\n", id, from, target, *auditDB)
	return 0
}
