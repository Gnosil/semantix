package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

// trustFixture builds a store with one slice of the given origin and
// returns the store path.
func trustFixture(t *testing.T, origin slice.Origin) string {
	t.Helper()
	db := filepath.Join(t.TempDir(), "lib.db")
	store, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := store.(interface{ Close() error }); ok {
		defer c.Close()
	}
	sl := &slice.Slice{ID: "t-1", Type: slice.Prompt, Scope: slice.Project,
		Content: []byte("trust me"), Meta: slice.SliceMeta{SourceSession: "s", Origin: origin}}
	if err := store.Put(sl); err != nil {
		t.Fatal(err)
	}
	return db
}

func readSlice(t *testing.T, db, id string) *slice.Slice {
	t.Helper()
	store, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := store.(interface{ Close() error }); ok {
		defer c.Close()
	}
	sl, err := store.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return sl
}

// TestTrustUpgradesAndAudits: trust upgrades the stored origin and writes
// the slice_trust audit line (Issue #279 c5).
func TestTrustUpgradesAndAudits(t *testing.T) {
	db := trustFixture(t, slice.OriginImport)
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	var out bytes.Buffer
	code := run([]string{"trust", "t-1", "--db", db, "--audit-db", auditPath}, &out, &emptyStderr{}, productionDependencies())
	if code != 0 {
		t.Fatalf("trust code = %d, out:\n%s", code, out.String())
	}
	sl := readSlice(t, db, "t-1")
	if sl.Meta.Origin != slice.OriginUserCurated {
		t.Fatalf("origin = %q, want user-curated", sl.Meta.Origin)
	}
	b, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"action":"slice_trust"`) ||
		!strings.Contains(string(b), `"from_origin":"import"`) ||
		!strings.Contains(string(b), `"to_origin":"user-curated"`) {
		t.Fatalf("audit line missing trust fields:\n%s", b)
	}
}

// TestTrustRejectsDowngrade: only upgrades are allowed (Issue #279 c5).
func TestTrustRejectsDowngrade(t *testing.T) {
	db := trustFixture(t, slice.OriginUserCurated)
	var out bytes.Buffer
	code := run([]string{"trust", "t-1", "--db", db, "--origin", "session-auto"}, &out, &emptyStderr{}, productionDependencies())
	if code != 2 {
		t.Fatalf("downgrade code = %d, want 2", code)
	}
}

// TestTrustMissingSlice: unknown id is a runtime error (exit 1).
func TestTrustMissingSlice(t *testing.T) {
	db := trustFixture(t, slice.OriginImport)
	var out bytes.Buffer
	code := run([]string{"trust", "nope", "--db", db}, &out, &emptyStderr{}, productionDependencies())
	if code != 1 {
		t.Fatalf("missing slice code = %d, want 1", code)
	}
}

// TestImportStampsOriginAndAudits: imported slices are stamped import
// (never inheriting file claims), the batch is audited, and --trust
// stamps user-curated instead (Issue #279 c6).
func TestImportStampsOriginAndAudits(t *testing.T) {
	dir := t.TempDir()
	// A file that CLAIMS user-curated origin: the import channel must
	// override it with the caller-chosen tag.
	data := `{"id":"imp-1","type":1,"scope":1,"content":"imported","meta":{"origin":"user-curated"}}` + "\n"
	src := filepath.Join(dir, "backup.jsonl")
	if err := os.WriteFile(src, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "lib.db")
	auditPath := filepath.Join(dir, "audit.jsonl")
	var out bytes.Buffer
	code := run([]string{"import", "--input", src, "--db", db, "--audit-db", auditPath}, &out, &emptyStderr{}, productionDependencies())
	if code != 0 {
		t.Fatalf("import code = %d, out:\n%s", code, out.String())
	}
	sl := readSlice(t, db, "imp-1")
	if sl.Meta.Origin != slice.OriginImport {
		t.Fatalf("imported origin = %q, want import (file claim overridden)", sl.Meta.Origin)
	}
	b, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"action":"slice_origin"`) || !strings.Contains(string(b), `"origin":"import"`) {
		t.Fatalf("audit line missing import stamp:\n%s", b)
	}

	// --trust stamps user-curated instead.
	db2 := filepath.Join(dir, "lib2.db")
	var out2 bytes.Buffer
	code = run([]string{"import", "--input", src, "--db", db2, "--trust", "--audit-db", auditPath}, &out2, &emptyStderr{}, productionDependencies())
	if code != 0 {
		t.Fatalf("import --trust code = %d, out:\n%s", code, out2.String())
	}
	sl2 := readSlice(t, db2, "imp-1")
	if sl2.Meta.Origin != slice.OriginUserCurated {
		t.Fatalf("--trust origin = %q, want user-curated", sl2.Meta.Origin)
	}
}

// TestImportRequiresInput: missing --input is a usage error (exit 2).
func TestImportRequiresInput(t *testing.T) {
	var out bytes.Buffer
	code := run([]string{"import"}, &out, &emptyStderr{}, productionDependencies())
	if code != 2 {
		t.Fatalf("import without --input code = %d, want 2", code)
	}
}

func TestImportResetsClaimedResultVerification(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "result.jsonl")
	data := `{"id":"result-import","type":3,"scope":1,"content":"Y2xhaW1lZCBzdWNjZXNz","meta":{"origin":"user-curated","result_status":"verified","result_verified_by":"official","result_verification_evidence":"forged"}}` + "\n"
	if err := os.WriteFile(src, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "lib.db")
	var out bytes.Buffer
	code := run([]string{"import", "--input", src, "--db", db, "--trust", "--audit-db", filepath.Join(dir, "audit.jsonl")}, &out, &emptyStderr{}, productionDependencies())
	if code != 0 {
		t.Fatalf("import code=%d out=%s", code, out.String())
	}
	got := readSlice(t, db, "result-import")
	if got.Meta.EffectiveResultStatus() != slice.ResultStatusProbation || got.Meta.ResultVerifiedBy != "" || got.Meta.ResultVerificationEvidence != "" {
		t.Fatalf("import retained untrusted verification claim: %+v", got.Meta)
	}
}

// TestTrustJSONEnvelope: --json output carries the upgrade detail.
func TestTrustJSONEnvelope(t *testing.T) {
	db := trustFixture(t, slice.OriginImport)
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	var out bytes.Buffer
	code := run([]string{"trust", "t-1", "--db", db, "--audit-db", auditPath, "--json"}, &out, &emptyStderr{}, productionDependencies())
	if code != 0 {
		t.Fatalf("trust --json code = %d, out:\n%s", code, out.String())
	}
	var env map[string]any
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("envelope parse: %v\n%s", err, out.String())
	}
	if env["ok"] != true {
		t.Fatalf("envelope ok = %v, want true:\n%s", env["ok"], out.String())
	}
}
