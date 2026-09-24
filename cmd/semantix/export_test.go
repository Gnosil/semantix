package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExportCLI: `semantix export --db <db> --out <file>` must write every
// stored slice as one JSON line — the sanctioned full read for the offline
// trainer (Get/List strip embeddings; Export keeps raw bytes intact).
func TestExportCLI(t *testing.T) {
	deps, db := buildMaintenanceDeps(t)
	if err := os.WriteFile(db, []byte(
		`{"ID":"e1","Type":0,"Scope":1,"Content":"YWJj","Weight":0.5,"created_at":100}`+"\n"+
			`{"ID":"e2","Type":1,"Scope":1,"Content":"ZGVm","Weight":0.7,"created_at":200}`+"\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "export.jsonl")
	var stdout, stderr bytes.Buffer
	code := run([]string{"export", "--db", db, "--out", out}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("export code = %d, stderr = %q", code, stderr.String())
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("exported %d lines, want 2: %q", len(lines), raw)
	}
	ids := map[string]bool{}
	for _, line := range lines {
		var row struct {
			ID string `json:"ID"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatalf("line not valid JSON: %v (%q)", err, line)
		}
		ids[row.ID] = true
	}
	if !ids["e1"] || !ids["e2"] {
		t.Errorf("exported ids = %v, want e1+e2", ids)
	}
	if !strings.Contains(stdout.String(), "exported=2") {
		t.Errorf("stdout = %q, want exported=2 summary", stdout.String())
	}
}

// TestExportCLIStdout: without --out the JSONL streams to stdout, so the
// trainer can pipe it without a temp file.
func TestExportCLIStdout(t *testing.T) {
	deps, db := buildMaintenanceDeps(t)
	if err := os.WriteFile(db, []byte(
		`{"ID":"e1","Type":0,"Scope":1,"Content":"YWJj","Weight":0.5,"created_at":100}`+"\n",
	), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"export", "--db", db}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("export code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"ID":"e1"`) {
		t.Errorf("stdout missing slice JSON: %q", stdout.String())
	}
}
