package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"semantix/kernel/fingerprint"
	"semantix/kernel/slice"
)

func runExtract(args []string, stdout, stderr io.Writer, deps dependencies) error {
	flags := flag.NewFlagSet("extract", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "", "session JSONL path, or - for stdin")
	scopeValue := flags.String("scope", cfgString(deps.resolved, "store.scope", "project"), "slice scope: session, project, or user")
	dbOverride := flags.String("db", "", "database path override")
	projectDB := flags.String("project-db", cfgString(deps.resolved, "store.db", defaultProjectDB()), "project/session database path")
	userDB := flags.String("user-db", defaultUserDB(), "user database path")
	session := flags.String("session", "", "source session identifier")
	project := flags.String("project", "", "project slug")
	taskType := flags.String("task-type", "", "task type metadata")
	language := flags.String("language", "", "language metadata")
	fingerprintPaths := flags.String("fingerprint", "", "comma-separated relative paths to fingerprint (sha256) into each slice's Deps")
	l3Safe := flags.Bool("l3-safe", false, "mark dependency-free Result slices as explicitly L3-reusable (opt-in; ignored when --fingerprint is set)")
	embedder := flags.String("embedder", "hash", "embedder for stored slices: hash (default, zero-dependency) | model (remote OpenAI-compatible API; see SEMANTIX_EMBED_* env)")
	tStepSplit := flags.Bool("t-step-split", false, "split tool sequences into subtask-level T-slices at verification (test) boundaries")
	distill := flags.Bool("distill", false, "additionally distill the four-layer knowledge cards (repo-ops, plan-skeleton, outcome) from the transcript")
	consolidate := flags.Bool("consolidate", false, "after storing, merge near-duplicate Context slices into overview cards (ConsolidateContext, default threshold)")
	if err := flags.Parse(args); err != nil {
		return usageWrap(err)
	}
	if *input == "" {
		return usagef("--input is required")
	}
	if flags.NArg() != 0 {
		return usagef("unexpected arguments: %v", flags.Args())
	}

	scope, err := parseScope(*scopeValue)
	if err != nil {
		return err
	}
	data, err := readInput(*input)
	if err != nil {
		return err
	}

	extractor := deps.newExtractor()
	if *tStepSplit {
		extractor = slice.NewExtractorWithOptions(slice.ExtractOptions{TStepSplit: true})
	}
	if extractor == nil {
		return errors.New("slice extractor is unavailable; merge the U4 implementation first")
	}
	meta := slice.SliceMeta{
		SourceSession: *session,
		TaskType:      *taskType,
		Language:      *language,
		ProjectSlug:   *project,
		Origin:        slice.OriginUserCurated, // Issue #279: explicit user action
	}
	if *fingerprintPaths != "" {
		var paths []string
		for _, p := range strings.Split(*fingerprintPaths, ",") {
			if p = strings.TrimSpace(p); p != "" {
				paths = append(paths, p)
			}
		}
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
		depsMap, err := fingerprint.Capture(wd, paths)
		if err != nil {
			return fmt.Errorf("capture fingerprints: %w", err)
		}
		meta.Deps = depsMap
		// Record mtimes alongside the sha256 digests (U16): the L3 gate uses
		// mtimes as its cheap fast-fail check before re-reading content.
		mtimes := make(map[string]int64, len(paths))
		for _, p := range paths {
			st, err := os.Stat(filepath.Join(wd, p))
			if err != nil {
				return fmt.Errorf("stat %s: %w", p, err)
			}
			mtimes[p] = st.ModTime().Unix()
		}
		meta.Mtimes = mtimes
	} else if *l3Safe {
		// Dependency-free explicit opt-in (U16): without captured Deps the
		// L3 gate requires this flag before reusing a Result slice.
		meta.L3Safe = true
	}
	items, err := extractor.Extract(data, meta)
	if err != nil {
		return fmt.Errorf("extract slices: %w", err)
	}
	if *distill {
		cards, err := slice.Distill(data, meta)
		if err != nil {
			return fmt.Errorf("distill cards: %w", err)
		}
		items = append(items, cards...)
	}

	emb, err := buildEmbedder(*embedder, stderr)
	if err != nil {
		return err
	}
	if err := embedItems(emb, items, *embedder); err != nil {
		return fmt.Errorf("embed slices: %w", err)
	}

	dbPath := selectDB(scope, *dbOverride, *projectDB, *userDB)
	if dir := filepath.Dir(dbPath); dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create database directory: %w", err)
		}
	}
	store, err := deps.openStore(dbPath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	if store == nil {
		return errors.New("slice store is unavailable; merge the U4 implementation first")
	}
	defer closeStore(store)

	stored := 0
	storedItems := make([]*slice.Slice, 0, len(items))
	for i, item := range items {
		if item == nil {
			return fmt.Errorf("extractor returned nil slice at position %d", i)
		}
		if item.Type == slice.Context && scope != slice.Project {
			continue
		}
		item.Scope = scope
		if err := store.Put(item); err != nil {
			return fmt.Errorf("store slice %q: %w", item.ID, err)
		}
		stored++
		storedItems = append(storedItems, item)
	}

	rawBytes, storedBytes, compressionRatio := extractionCompression(storedItems)
	fmt.Fprintf(stdout, "extracted=%d stored=%d scope=%s db=%s raw_bytes=%d stored_bytes=%d compression_ratio=%.4f\n",
		len(items), stored, scopeName(scope), dbPath, rawBytes, storedBytes, compressionRatio)
	if *consolidate {
		// Layer-B wiring (semantic-four-layer-distill spec §2.3): fold
		// near-duplicate Context cards into overview cards as part of the
		// routine extract path, not only at gc time. Same maintenance-window
		// caveat as gc --consolidate-context: run between sessions, not
		// while a live session is injecting.
		res, err := slice.ConsolidateContext(store, slice.ConsolidateOptions{})
		if err != nil {
			return fmt.Errorf("consolidate context: %w", err)
		}
		fmt.Fprintf(stdout, "consolidated checked=%d groups=%d merged=%d\n", res.Checked, res.Groups, res.Merged)
	}
	// Water-level hint (never an error, never triggers eviction here — the
	// hot write path stays O(1); the cap is enforced at gc / gateway boot).
	if maxSlices := cfgInt(deps.resolved, "store.max_slices", 5000); maxSlices > 0 {
		if all, err := store.ListAll(); err == nil && len(all)*10 >= maxSlices*9 {
			fmt.Fprintf(stderr, "semantix: library at %d/%d slices (>=90%%) — run `semantix gc` to rescore and archive low-value slices\n",
				len(all), maxSlices)
		}
	}
	return nil
}

func extractionCompression(items []*slice.Slice) (rawBytes, storedBytes int, ratio float64) {
	for _, item := range items {
		if item == nil {
			continue
		}
		stored := len(item.Content)
		raw := stored
		meta := item.Meta
		if meta.CompressionVersion != "" && meta.OriginalBytes > 0 &&
			meta.StoredBytes == stored && meta.OriginalBytes >= meta.StoredBytes {
			raw = meta.OriginalBytes
		}
		rawBytes += raw
		storedBytes += stored
	}
	if rawBytes > 0 {
		ratio = float64(rawBytes-storedBytes) / float64(rawBytes)
	}
	return rawBytes, storedBytes, ratio
}

func readInput(path string) ([]byte, error) {
	if path == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read input %q: %w", path, err)
	}
	return data, nil
}
