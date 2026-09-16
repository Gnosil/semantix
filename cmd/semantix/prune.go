package main

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"semantix/kernel/slice"
)

func runPrune(args []string, stdout, stderr io.Writer, deps dependencies) error {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	fs.SetOutput(stderr)
	scope := fs.String("scope", "project", "slice scope: project or user")
	apply := fs.Bool("apply", false, "archive and atomically delete candidates (requires all store handles closed)")
	dryRun := fs.Bool("dry-run", false, "read-only preview (the default)")
	older := fs.Int("older-than-days", 90, "minimum age and inactivity in days (1..3650000)")
	recent := fs.Int("recent-days", 30, "protect slices created or used within this many days (1..3650000)")
	project := fs.String("project", "", "limit to an exact project slug")
	root := fs.String("project-root", "", "check captured dependencies under this root; requires --project")
	db := fs.String("db", "", "database path override")
	jsonOut := fs.Bool("json", false, "write metadata-only JSON envelope")
	if err := fs.Parse(args); err != nil {
		return usageWrap(err)
	}
	if fs.NArg() != 0 {
		return usagef("prune: unexpected positional arguments")
	}
	if *scope != "project" && *scope != "user" {
		return usagef("prune: --scope must be project or user")
	}
	if *apply && *dryRun {
		return usagef("prune: --apply and --dry-run cannot be combined")
	}
	if *older < 1 || *older > 3650000 || *recent < 1 || *recent > 3650000 {
		return usagef("prune: day windows must be between 1 and 3650000")
	}
	if *root != "" && *project == "" {
		return usagef("prune: --project-root requires --project")
	}
	sc := slice.Project
	if *scope == "user" {
		sc = slice.User
	}
	path := *db
	if path == "" && sc == slice.Project {
		path = cfgString(deps.resolved, "store.db", defaultProjectDB())
	}
	if path == "" {
		path = defaultUserDB()
	}
	res, err := slice.PruneFile(path, slice.PruneOptions{Scope: sc, Apply: *apply, OlderThanDays: *older, RecentDays: *recent, Project: *project, ProjectRoot: *root})
	if err != nil {
		if *jsonOut {
			return failJSON(stdout, "prune", err)
		}
		return err
	}
	if *jsonOut {
		err = writeEnvelope(stdout, "prune", res)
	} else {
		err = renderPrune(stdout, res)
	}
	if err != nil && res.Removed > 0 {
		return fmt.Errorf("prune: deletion committed (%d records); report delivery failed; archive %q: %w", res.Removed, res.ArchivePath, err)
	}
	return err
}

func renderPrune(w io.Writer, r slice.PruneResult) error {
	var b strings.Builder
	fmt.Fprintf(&b, "prune: scope=%s dry_run=%t checked=%d candidates=%d kept=%d removed=%d\n", r.Scope, r.DryRun, r.Checked, len(r.Candidates), r.Kept, r.Removed)
	fmt.Fprintf(&b, "estimated_reclaimable_bytes=%d (%s)\n", r.EstimatedReclaimableBytes, r.SpaceEstimate)
	for _, c := range r.Candidates {
		fmt.Fprintf(&b, "  id=%q type=%s reasons=%s session=%q created_at=%d last_used=%d bytes=%d", c.ID, c.Type, strings.Join(c.Reasons, ","), c.SourceSession, c.CreatedAt, c.LastUsed, c.EstimatedReclaimableBytes)
		if c.RetainedID != "" {
			fmt.Fprintf(&b, " retained_id=%q", c.RetainedID)
		}
		b.WriteByte('\n')
	}
	for _, s := range r.SkippedChecks {
		fmt.Fprintf(&b, "  skipped %s: %s\n", s.Check, s.Reason)
	}
	if r.ArchivePath != "" {
		fmt.Fprintf(&b, "archive=%q\n", r.ArchivePath)
	}
	_, err := io.WriteString(w, b.String())
	return err
}
