package semantix

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestBridgeAuditJournalDumpsAdmittedInjections verifies the Issue #326
// injection audit journal: every admitted L2 injection is appended verbatim
// (query + targets + bytes + full [semantix-reuse] block), and a shadow/off
// retrieval mode — which never admits — writes nothing.
func TestBridgeAuditJournalDumpsAdmittedInjections(t *testing.T) {
	dir := writeKernelDir(t, admissionFixtureSlices(), nil)
	auditDir := t.TempDir()
	b := NewBridge(Config{Enabled: true, Inject: true, ProjectDir: dir,
		SessionsDir: t.TempDir(), Budget: 4096, AuditDir: auditDir})
	b.SetLabel("audit-session")
	res := b.InjectDetailed(context.Background(), "修复 go 测试")
	if res.Text == "" || len(res.Targets) == 0 {
		t.Fatalf("InjectDetailed() = %+v, want admitted slices", res)
	}
	// A second, distinct query either misses or injects; either way the
	// journal must contain the first entry already and Close must stay clean.
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	entries := readAuditJournal(t, auditDir)
	if len(entries) != 1 {
		t.Fatalf("audit journal entries = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Text != res.Text {
		t.Fatalf("journal text != injected text (%d vs %d bytes)", len(e.Text), len(res.Text))
	}
	if e.Bytes == 0 || e.Bytes != len(res.Text) {
		t.Fatalf("journal bytes = %d, text len = %d", e.Bytes, len(res.Text))
	}
	if len(e.Targets) != len(res.Targets) {
		t.Fatalf("journal targets = %v, want %v", e.Targets, res.Targets)
	}
	if e.Query == "" || e.QueryHash == "" {
		t.Fatalf("journal query fields empty: %+v", e)
	}
	if e.Seq != 1 || e.At == 0 {
		t.Fatalf("journal seq/at = %d/%d", e.Seq, e.At)
	}
	if e.Session != "audit-session" {
		t.Fatalf("journal session = %q", e.Session)
	}
}

func TestBridgeAuditJournalSilentWhenNoDirOrShadow(t *testing.T) {
	t.Run("no audit dir config", func(t *testing.T) {
		dir := writeKernelDir(t, admissionFixtureSlices(), nil)
		b := NewBridge(Config{Enabled: true, Inject: true, ProjectDir: dir})
		if res := b.InjectDetailed(context.Background(), "修复 go 测试"); res.Text == "" {
			t.Fatal("expected an admitted injection")
		}
		_ = b.Close()
		// No AuditDir configured: the runner must not see a journal anywhere.
		if entries, err := os.ReadDir(filepath.Join(dir, "..")); err == nil {
			for _, entry := range entries {
				if entry.Name() == "inject-audit.jsonl" {
					t.Fatal("journal created without AuditDir configured")
				}
			}
		}
	})
	t.Run("shadow mode writes nothing", func(t *testing.T) {
		dir := writeKernelDir(t, admissionFixtureSlices(), nil)
		auditDir := t.TempDir()
		b := NewBridge(Config{Enabled: true, Mode: "shadow", Inject: true,
			ProjectDir: dir, AuditDir: auditDir})
		res := b.InjectDetailed(context.Background(), "修复 go 测试")
		if res.Text != "" {
			t.Fatalf("shadow mode must not inject, got %d bytes", len(res.Text))
		}
		_ = b.Close()
		if entries := readAuditJournal(t, auditDir); len(entries) != 0 {
			t.Fatalf("shadow mode wrote %d audit entries", len(entries))
		}
	})
}

// readAuditJournal returns the decoded entries of the journal, tolerating an
// absent file (len 0).
func readAuditJournal(t *testing.T, auditDir string) []auditEntry {
	t.Helper()
	f, err := os.Open(filepath.Join(auditDir, "inject-audit.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []auditEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var e auditEntry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("journal line: %v", err)
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
