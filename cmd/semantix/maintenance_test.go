package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

// buildMaintenanceDeps returns deps wired to a real fileStore on a temp path.
func buildMaintenanceDeps(t *testing.T) (dependencies, string) {
	t.Helper()
	db := filepath.Join(t.TempDir(), "library.jsonl")
	deps := dependencies{
		newExtractor: func() slice.Extractor { return nil },
		openStore:    func(path string) (slice.Store, error) { return slice.NewFileStore(path) },
		newIndex:     func() slice.Index { return nil },
	}
	return deps, db
}

func TestExportImportCLIRoundTrip(t *testing.T) {
	deps, db := buildMaintenanceDeps(t)
	// Seed a library via the real extractor so slices carry Meta + CreatedAt.
	input := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(input, []byte(`{"role":"user","content":"查一下"}
{"role":"assistant","content":"好的。"}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"extract", "--input", input, "--db", db}, &stdout, &stderr, productionDependencies()); code != 0 {
		t.Fatalf("seed extract code = %d, stderr = %q", code, stderr.String())
	}

	backup := filepath.Join(t.TempDir(), "backup.jsonl")
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"export", "--output", backup, "--db", db}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("export code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "export:") {
		t.Fatalf("export stdout = %q", stdout.String())
	}

	// Import into a fresh library and confirm slices (incl. Meta) survive.
	dst := filepath.Join(t.TempDir(), "restored.jsonl")
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"import", "--input", backup, "--db", dst}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("import code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "imported=") {
		t.Fatalf("import stdout = %q", stdout.String())
	}
	store, err := slice.NewFileStore(dst)
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("import restored nothing")
	}
	for _, s := range items {
		if s.Meta.SourceSession == "" && s.CreatedAt == 0 {
			t.Fatalf("restored slice %s lost Meta/CreatedAt", s.ID)
		}
	}
}

// Import is idempotent at the CLI level: running it twice yields no dupes.
func TestImportCLIIdempotent(t *testing.T) {
	deps, db := buildMaintenanceDeps(t)
	backup := filepath.Join(t.TempDir(), "backup.jsonl")
	if err := os.WriteFile(backup, []byte("{\"id\":\"x\",\"type\":0,\"scope\":1,\"content\":\"Z29vZA==\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		var stdout, stderr bytes.Buffer
		if code := run([]string{"import", "--input", backup, "--db", db}, &stdout, &stderr, deps); code != 0 {
			t.Fatalf("import %d code = %d, stderr = %q", i, code, stderr.String())
		}
	}
	store, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ListAll()
	if err != nil || len(items) != 1 {
		t.Fatalf("after double import len = %d (%v), want 1", len(items), err)
	}
}

// A failed export must not truncate or destroy an existing backup, and must
// not leave stray temp files. Two failure modes: (a) store open failure —
// happens before the temp file is created; (b) rename failure — the temp file
// is created and written, then the atomic replace fails, so the temp cleanup
// path is exercised.
func TestExportFailurePreservesExistingBackup(t *testing.T) {
	t.Run("store-open-failure", func(t *testing.T) {
		deps, _ := buildMaintenanceDeps(t)
		backup := filepath.Join(t.TempDir(), "backup.jsonl")
		if err := os.WriteFile(backup, []byte("previous backup\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		code := run([]string{"export", "--output", backup, "--db", filepath.Join(t.TempDir(), "no", "dir", "lib.jsonl")}, &stdout, &stderr, deps)
		if code != 1 {
			t.Fatalf("export code = %d, want 1 (open store failure); stderr = %q", code, stderr.String())
		}
		got, err := os.ReadFile(backup)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "previous backup\n" {
			t.Fatalf("existing backup was clobbered by failed export: %q", got)
		}
		assertNoStrayTemps(t, filepath.Dir(backup), filepath.Base(backup))
	})

	t.Run("rename-failure", func(t *testing.T) {
		deps, db := buildMaintenanceDeps(t)
		// A valid library so the export actually writes the temp file.
		if err := os.WriteFile(db, []byte("{\"id\":\"a\",\"type\":0,\"scope\":1}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		// Target is an existing directory: os.Rename(temp, dir) fails after
		// the temp file has been created and written.
		dir := t.TempDir()
		var stdout, stderr bytes.Buffer
		code := run([]string{"export", "--output", dir, "--db", db}, &stdout, &stderr, deps)
		if code != 1 {
			t.Fatalf("export code = %d, want 1 (rename failure); stderr = %q", code, stderr.String())
		}
		// The temp file is created as <dir-prefix>.tmp-* in dir's parent
		// (the shared OS temp dir); assert none of OUR temps remain.
		assertNoStrayTemps(t, filepath.Dir(dir), filepath.Base(dir))
	})
}

// assertNoStrayTemps fails if any file matching <prefix>.tmp-* remains in dir.
func assertNoStrayTemps(t *testing.T, dir, prefix string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix+".tmp-") {
			t.Fatalf("stray temp file left behind: %s", e.Name())
		}
	}
}

func TestExportJSONEnvelope(t *testing.T) {
	deps, db := buildMaintenanceDeps(t)
	if err := os.WriteFile(db, []byte("{\"id\":\"a\",\"type\":0,\"scope\":1,\"content\":\"eA==\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup.jsonl")
	var stdout, stderr bytes.Buffer
	code := run([]string{"export", "--output", backup, "--db", db, "--json"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("export --json code = %d, stderr = %q", code, stderr.String())
	}
	var env envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("envelope not valid JSON: %v\n%s", err, stdout.String())
	}
	if !env.OK || env.Command != "export" || env.Error != nil || env.Version != version {
		t.Fatalf("envelope = %+v", env)
	}
	data := env.Data.(map[string]interface{})
	if data["exported"].(float64) != 1 {
		t.Fatalf("exported = %v, want 1", data["exported"])
	}
	if data["skipped"].(float64) != 0 {
		t.Fatalf("skipped = %v, want 0", data["skipped"])
	}
}

func TestImportJSONEnvelope(t *testing.T) {
	deps, db := buildMaintenanceDeps(t)
	backup := filepath.Join(t.TempDir(), "backup.jsonl")
	if err := os.WriteFile(backup, []byte("{\"id\":\"a\",\"type\":0,\"scope\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"import", "--input", backup, "--db", db, "--json"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("import --json code = %d, stderr = %q", code, stderr.String())
	}
	var env envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("envelope not valid JSON: %v", err)
	}
	if !env.OK || env.Command != "import" || env.Error != nil {
		t.Fatalf("envelope = %+v", env)
	}
	data := env.Data.(map[string]interface{})
	if data["imported"].(float64) != 1 || data["skipped"].(float64) != 0 {
		t.Fatalf("data = %v", data)
	}
}

func TestGCCLI(t *testing.T) {
	deps, db := buildMaintenanceDeps(t)
	old := int64(1) // ancient
	if err := os.WriteFile(db, []byte(
		"{\"id\":\"old\",\"type\":0,\"scope\":1,\"created_at\":"+strconv.FormatInt(old, 10)+",\"weight\":0.9}\n"+
			"{\"id\":\"low\",\"type\":1,\"scope\":1,\"created_at\":9999999999,\"weight\":0.1}\n"+
			"{\"id\":\"keep\",\"type\":0,\"scope\":1,\"created_at\":9999999999,\"weight\":0.9}\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}

	// Dry-run removes nothing but reports candidates. --no-rescore keeps the
	// handcrafted fixture weights authoritative (a rescore would replace
	// them with computed values, which is its own test).
	var stdout, stderr bytes.Buffer
	code := run([]string{"gc", "--retention-days", "7", "--min-weight", "0.5", "--no-rescore", "--dry-run", "--db", db}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("gc dry-run code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "would_remove=2") {
		t.Fatalf("gc dry-run stdout = %q", stdout.String())
	}
	store, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if all, _ := store.ListAll(); len(all) != 3 {
		t.Fatalf("dry-run removed slices: len = %d", len(all))
	}

	// Real run removes old + low, keeps the rest.
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"gc", "--retention-days", "7", "--min-weight", "0.5", "--no-rescore", "--db", db}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("gc code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "removed=2") {
		t.Fatalf("gc stdout = %q", stdout.String())
	}
	// Re-open: a store handle is a snapshot of open time (freeze-window
	// semantics); the gc ran in its own handle, so observe like a fresh
	// process would.
	store, err = slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "keep" {
		t.Fatalf("after gc got %d slices: %v", len(items), items)
	}
}

func TestGCJSONEnvelope(t *testing.T) {
	deps, db := buildMaintenanceDeps(t)
	if err := os.WriteFile(db, []byte("{\"id\":\"old\",\"type\":0,\"scope\":1,\"created_at\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"gc", "--retention-days", "1", "--db", db, "--json"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("gc --json code = %d, stderr = %q", code, stderr.String())
	}
	var env envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("envelope not valid JSON: %v", err)
	}
	if !env.OK || env.Command != "gc" || env.Error != nil {
		t.Fatalf("envelope = %+v", env)
	}
	data := env.Data.(map[string]interface{})
	if data["removed"].(float64) != 1 || data["dry_run"].(bool) != false {
		t.Fatalf("data = %v", data)
	}
}

// Usage errors exit 2 (U19 §4.3) for the new commands — both post-parse
// validation and flag-parse failures (unknown flag / bad value).
func TestMaintenanceUsageErrorsExit2(t *testing.T) {
	deps, _ := buildMaintenanceDeps(t)
	cases := [][]string{
		{"export", "--json"},              // --json needs a file output
		{"gc", "--retention-days", "-5"},  // negative retention
		{"import", "extra-arg"},           // unexpected positional
		{"gc", "--bogus-flag"},            // unknown flag: parse failure
		{"gc", "--retention-days", "abc"}, // bad flag value: parse failure
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr, deps); code != 2 {
			t.Fatalf("run(%v) code = %d, want 2; stderr = %q", args, code, stderr.String())
		}
	}
}

// A usage error under --json still emits the §4.2 failure envelope (ok:false,
// error.code 2) so a JSON consumer gets parseable output.
func TestMaintenanceUsageErrorJSONEnvelope(t *testing.T) {
	deps, _ := buildMaintenanceDeps(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"gc", "--retention-days", "-5", "--json"}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("code = %d, want 2; stderr = %q", code, stderr.String())
	}
	var env envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("failure envelope not valid JSON: %v\n%s", err, stdout.String())
	}
	if env.OK || env.Command != "gc" || env.Error == nil || env.Error.Code != 2 {
		t.Fatalf("failure envelope = %+v", env)
	}
}

// Flag-parse failure under --json also emits the failure envelope.
func TestMaintenanceFlagParseErrorJSONEnvelope(t *testing.T) {
	deps, _ := buildMaintenanceDeps(t)
	var stdout, stderr bytes.Buffer
	code := run([]string{"gc", "--bogus-flag", "--json"}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("code = %d, want 2; stderr = %q", code, stderr.String())
	}
	var env envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("failure envelope not valid JSON: %v\n%s", err, stdout.String())
	}
	if env.OK || env.Error == nil || env.Error.Code != 2 {
		t.Fatalf("failure envelope = %+v", env)
	}
}

// Runtime failures (missing db dir) exit 1 and, with --json, emit ok:false.
func TestMaintenanceRuntimeErrorExit1(t *testing.T) {
	deps, _ := buildMaintenanceDeps(t)
	db := filepath.Join(t.TempDir(), "no", "such", "dir", "lib.jsonl")
	var stdout, stderr bytes.Buffer
	code := run([]string{"gc", "--db", db, "--json"}, &stdout, &stderr, deps)
	if code != 1 {
		t.Fatalf("gc code = %d, want 1; stderr = %q", code, stderr.String())
	}
	var env envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("failure envelope not valid JSON: %v\n%s", err, stdout.String())
	}
	if env.OK || env.Error == nil || env.Error.Code != 1 {
		t.Fatalf("failure envelope = %+v", env)
	}
}
