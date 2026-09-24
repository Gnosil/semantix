package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"semantix/kernel/audit"
	"semantix/kernel/slice"
)

// runVerifyResult promotes evidence produced outside automatic transcript
// extraction, such as an official evaluator or an explicit user confirmation.
func runVerifyResult(args []string, stdout io.Writer, deps dependencies) int {
	fs := flag.NewFlagSet("verify-result", flag.ContinueOnError)
	db := fs.String("db", "", "database path override (default: project db)")
	projectDB := fs.String("project-db", cfgString(deps.resolved, "store.db", defaultProjectDB()), "project/session database path")
	userDB := fs.String("user-db", defaultUserDB(), "user database path")
	scopeName := fs.String("scope", "project", "slice scope: session, project, or user")
	method := fs.String("method", "", "evidence channel: command, official, or user")
	evidence := fs.String("evidence", "", "verification command, evaluator result, or confirmation reference")
	auditDB := fs.String("audit-db", filepath.Join(".semantix", "audit.jsonl"), "audit log path")
	jsonOut := fs.Bool("json", false, "output as JSON envelope")
	args = reorderPositional(args)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		fmt.Fprintln(os.Stderr, "verify-result:", err)
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stdout, "Usage: semantix verify-result <slice-id> --method command|official|user --evidence <text> [--db <path>]")
		return 2
	}
	*method = strings.ToLower(strings.TrimSpace(*method))
	*evidence = strings.TrimSpace(*evidence)
	if (*method != "command" && *method != "official" && *method != "user") || *evidence == "" {
		fmt.Fprintln(stdout, "verify-result: --method must be command, official, or user and --evidence must be non-empty")
		return 2
	}
	scope, err := parseScope(*scopeName)
	if err != nil {
		fmt.Fprintln(stdout, "verify-result:", err)
		return 2
	}
	store, err := deps.openStore(selectDB(scope, *db, *projectDB, *userDB))
	if err != nil || store == nil {
		fmt.Fprintln(os.Stderr, "verify-result: open store:", err)
		return 1
	}
	defer closeStore(store)
	id := fs.Arg(0)
	sl, err := store.Get(id)
	if err != nil || sl == nil {
		fmt.Fprintf(stdout, "verify-result: Result slice %q not found\n", id)
		return 1
	}
	if sl.Type != slice.Result {
		fmt.Fprintf(stdout, "verify-result: slice %q has type %s, want result\n", id, sl.Type)
		return 2
	}
	if *method == "user" && (sl.Meta.SourceSession == "" || sl.Meta.ProjectSlug == "" || !sl.Meta.Origin.Valid()) {
		fmt.Fprintln(stdout, "verify-result: user confirmation requires source session, project, and valid origin provenance")
		return 2
	}
	sl.Meta.ResultStatus = slice.ResultStatusVerified
	sl.Meta.ResultVerifiedBy = *method
	sl.Meta.ResultVerificationEvidence = *evidence
	if err := store.Put(sl); err != nil {
		fmt.Fprintln(os.Stderr, "verify-result: put:", err)
		return 1
	}
	rec, err := audit.NewRecorder(*auditDB)
	if err != nil {
		fmt.Fprintln(os.Stderr, "verify-result: audit:", err)
		return 1
	}
	if err := rec.ResultVerify(id, *method, *evidence); err != nil {
		fmt.Fprintln(os.Stderr, "verify-result: audit:", err)
		return 1
	}
	if *jsonOut {
		if err := writeJSON(stdout, okEnvelope("verify-result", map[string]any{
			"slice_id": id, "status": string(slice.ResultStatusVerified), "method": *method, "evidence": *evidence,
		})); err != nil {
			return 1
		}
		return 0
	}
	fmt.Fprintf(stdout, "verify-result: %s probation -> verified method=%s (audit: %s)\n", id, *method, *auditDB)
	return 0
}
