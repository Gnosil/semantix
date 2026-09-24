package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"semantix/kernel/slice"
)

func seedPruneCLI(t *testing.T, path string, scope slice.Scope) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	st, err := slice.NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(st)
	for _, id := range []string{"old", "recent"} {
		age := 180
		if id == "recent" {
			age = 1
		}
		s := &slice.Slice{ID: id, Type: slice.Context, Scope: scope, Content: []byte("SECRET_BODY_" + id), CreatedAt: time.Now().Add(-time.Duration(age) * 24 * time.Hour).Unix(),
			Meta: slice.SliceMeta{Origin: slice.OriginSessionAuto, SourceSession: "session\n\x1b[31mtest"}}
		if err := st.Put(s); err != nil {
			t.Fatal(err)
		}
	}
}

func decodePruneCLI(t *testing.T, b []byte) slice.PruneResult {
	t.Helper()
	var env struct {
		OK      bool              `json:"ok"`
		Command string            `json:"command"`
		Data    slice.PruneResult `json:"data"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		t.Fatalf("decode: %v: %s", err, b)
	}
	if !env.OK || env.Command != "prune" {
		t.Fatalf("envelope: %s", b)
	}
	return env.Data
}

func TestPruneCLIReadOnlyApplyAndJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.jsonl")
	seedPruneCLI(t, path, slice.Project)
	base, _ := os.ReadFile(path)
	journal, _ := os.ReadFile(path + ".journal")
	var out, stderr bytes.Buffer
	deps := productionDependencies()
	if code := run([]string{"prune", "--db", path, "--json"}, &out, &stderr, deps); code != 0 {
		t.Fatalf("preview %d: %s", code, &stderr)
	}
	r := decodePruneCLI(t, out.Bytes())
	if !r.DryRun || r.Removed != 0 || len(r.Candidates) != 1 || r.Candidates[0].ID != "old" {
		t.Fatalf("plan: %+v", r)
	}
	if bytes.Contains(out.Bytes(), []byte("SECRET_BODY")) {
		t.Fatal("body leaked")
	}
	b, _ := os.ReadFile(path)
	j, _ := os.ReadFile(path + ".journal")
	if !bytes.Equal(base, b) || !bytes.Equal(journal, j) {
		t.Fatal("bare prune mutated store")
	}
	first := append([]byte{}, out.Bytes()...)
	out.Reset()
	if code := run([]string{"prune", "--db", path, "--dry-run", "--json"}, &out, &stderr, deps); code != 0 {
		t.Fatal(stderr.String())
	}
	if !bytes.Equal(first, out.Bytes()) {
		t.Fatal("explicit preview differs from default")
	}
	out.Reset()
	if code := run([]string{"prune", "--db", path, "--apply", "--json"}, &out, &stderr, deps); code != 0 {
		t.Fatal(stderr.String())
	}
	applied := decodePruneCLI(t, out.Bytes())
	if applied.Removed != 1 || applied.DryRun || !reflect.DeepEqual(r.Candidates, applied.Candidates) {
		t.Fatalf("apply: %+v", applied)
	}
	if _, err := os.Stat(applied.ArchivePath); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := run([]string{"prune", "--db", path}, &out, &stderr, deps); code != 0 {
		t.Fatal(stderr.String())
	}
	if !strings.Contains(out.String(), "candidates=0") || !strings.Contains(out.String(), "logical live-record bytes") {
		t.Fatalf("text: %s", &out)
	}
}

func TestPruneCLIScopeAndDBSelection(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	home := filepath.Join(dir, "home")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	projectDB, userDB, override := filepath.Join(dir, "configured.db"), defaultUserDB(), filepath.Join(dir, "override.db")
	seedPruneCLI(t, projectDB, slice.Project)
	seedPruneCLI(t, userDB, slice.User)
	seedPruneCLI(t, override, slice.User)
	deps := configDBDeps(projectDB)
	for _, tc := range []struct {
		args  []string
		scope string
		want  int
	}{
		{nil, "project", 2},
		{[]string{"--scope", "user"}, "user", 2},
		{[]string{"--scope", "user", "--db", override}, "user", 2},
		{[]string{"--scope", "project", "--db", override}, "project", 0},
	} {
		var out, stderr bytes.Buffer
		args := append([]string{"--json"}, tc.args...)
		// run resolves real process configuration; this unit supplies its own
		// resolved configuration just like the existing GC config tests.
		if err := runPrune(args, &out, &stderr, deps); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		r := decodePruneCLI(t, out.Bytes())
		if r.Scope != tc.scope || r.Checked != tc.want {
			t.Fatalf("%v: %+v", args, r)
		}
	}
}

func TestPruneCLIMissingStoreAndUsage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent", "s.jsonl")
	for _, args := range [][]string{
		{"--scope", "session"}, {"--apply", "--dry-run"}, {"--older-than-days", "0"}, {"--recent-days", "-1"},
		{"--older-than-days", "3650001"}, {"--project-root", dir}, {"unexpected"}, {"--bogus"},
	} {
		var out, stderr bytes.Buffer
		all := append([]string{"prune", "--json", "--db", path}, args...)
		if code := run(all, &out, &stderr, productionDependencies()); code != 2 {
			t.Fatalf("%v: code=%d %s", args, code, &stderr)
		}
		var env envelope
		if err := json.Unmarshal(out.Bytes(), &env); err != nil || env.OK || env.Error == nil || env.Error.Code != 2 {
			t.Fatalf("usage envelope: %s %v", &out, err)
		}
	}
	var out, stderr bytes.Buffer
	if code := run([]string{"prune", "--db", path, "--json"}, &out, &stderr, productionDependencies()); code != 0 {
		t.Fatal(stderr.String())
	}
	if r := decodePruneCLI(t, out.Bytes()); r.Checked != 0 || r.Candidates == nil {
		t.Fatalf("empty: %+v", r)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatal("missing-store preview created files")
	}
}

func TestPruneCLIPathMappingAndPrivateText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	st, err := slice.NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &slice.Slice{ID: "missing", Type: slice.Context, Scope: slice.Project, Content: []byte("SECRET_BODY"), CreatedAt: time.Now().Add(-60 * 24 * time.Hour).Unix(),
		Meta: slice.SliceMeta{Origin: slice.OriginSessionAuto, ProjectSlug: "repo", SourceSession: "session\n\x1b[31mtext", Deps: map[string]string{"missing-file": "fingerprint"}}}
	if err := st.Put(s); err != nil {
		t.Fatal(err)
	}
	closeStore(st)
	var out, stderr bytes.Buffer
	if code := run([]string{"prune", "--db", path, "--project", "repo", "--project-root", dir}, &out, &stderr, productionDependencies()); code != 0 {
		t.Fatal(stderr.String())
	}
	text := out.String()
	for _, want := range []string{"missing_dependency", "type=context", "session=", "created_at=", "bytes="} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %s: %s", want, text)
		}
	}
	if strings.ContainsAny(text, "\x1b") || strings.Contains(text, "SECRET_BODY") || strings.Contains(text, "session\n") {
		t.Fatalf("unsafe output: %q", text)
	}
}

type brokenPruneWriter struct{}

func (brokenPruneWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestPruneCLICommittedReportFailureAndRuntimeJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	seedPruneCLI(t, path, slice.Project)
	var stderr bytes.Buffer
	err := runPrune([]string{"--db", path, "--apply"}, brokenPruneWriter{}, &stderr, productionDependencies())
	if !errors.Is(err, io.ErrClosedPipe) || !strings.Contains(err.Error(), "deletion committed") {
		t.Fatalf("ambiguous outcome: %v", err)
	}
	if err := os.WriteFile(path+".journal", []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := run([]string{"prune", "--db", path, "--json"}, &out, &stderr, productionDependencies()); code != 1 {
		t.Fatalf("code %d", code)
	}
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil || env.OK || env.Error == nil || env.Error.Code != 1 {
		t.Fatalf("runtime envelope: %s %v", &out, err)
	}
}
