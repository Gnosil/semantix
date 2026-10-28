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

// runImport restores slices from a JSONL file (Issue #279): every imported
// slice is stamped OriginImport — the import channel is untrusted, so an
// origin claim inside the file is never inherited. --trust stamps
// OriginUserCurated instead (import + trust in one explicit action). All
// stamps are written to the audit log.
func runImport(args []string, stdout io.Writer, deps dependencies) int {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	input := fs.String("input", "", "JSONL file to import (required)")
	db := fs.String("db", "", "database path override (default: project db)")
	projectDB := fs.String("project-db", cfgString(deps.resolved, "store.db", defaultProjectDB()), "project/session database path")
	userDB := fs.String("user-db", defaultUserDB(), "user database path")
	scopeName := fs.String("scope", cfgString(deps.resolved, "store.scope", "project"), "slice scope: session, project, or user")
	trust := fs.Bool("trust", false, "stamp imported slices as user-curated (import + trust in one action)")
	auditDB := fs.String("audit-db", filepath.Join(".semantix", "audit.jsonl"), "audit log path")
	jsonOut := fs.Bool("json", false, "output as JSON envelope")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0 // --help is a successful request
		}
		fmt.Fprintln(os.Stderr, "import:", err)
		return 2
	}
	if *input == "" {
		fmt.Fprintln(stdout, "Usage: semantix import --input <file.jsonl> [--trust] [--db <path>]")
		return 2
	}
	f, err := os.Open(*input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "import: open %s: %v\n", *input, err)
		return 1
	}
	defer f.Close()

	scope, err := parseScope(*scopeName)
	if err != nil {
		fmt.Fprintf(stdout, "import: %v\n", err)
		return 2
	}
	dbPath := selectConfiguredDB(fs, deps.resolved, scope, *db, *projectDB, *userDB)
	store, err := deps.openStore(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "import: open store: %v\n", err)
		return 1
	}
	if store == nil {
		fmt.Fprintln(os.Stderr, "import: slice store is unavailable")
		return 1
	}
	defer closeStore(store)

	origin := slice.OriginImport
	if *trust {
		origin = slice.OriginUserCurated
	}
	imported, skipped, err := slice.Import(store, f, origin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "import: %v\n", err)
		return 1
	}

	// Audit every stamp (Security §8③: 切片入库(来源+净化动作)).
	rec, aerr := audit.NewRecorder(*auditDB)
	if aerr != nil {
		fmt.Fprintf(os.Stderr, "import: audit: %v\n", aerr)
		return 1
	}
	// Per-slice audit lines; a failure aborts (audit is the point of the
	// --trust distinction), so the store and the log stay consistent.
	if err := rec.Origin(fmt.Sprintf("import-batch(%d)", imported), origin, "import"); err != nil {
		fmt.Fprintf(os.Stderr, "import: audit write: %v\n", err)
		return 1
	}

	if *jsonOut {
		data := map[string]any{"imported": imported, "skipped": skipped, "origin": string(origin), "audit": *auditDB}
		if err := writeJSON(stdout, okEnvelope("import", data)); err != nil {
			fmt.Fprintln(os.Stderr, "import:", err)
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "import: %d slices (origin=%s), %d skipped, audit=%s\n", imported, origin, skipped, *auditDB)
	return 0
}
