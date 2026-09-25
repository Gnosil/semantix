package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

func TestExtractRetainsIdenticalCardHistoryAcrossSessions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "session.jsonl")
	const transcript = `{"role":"user","content":"Fix parser crash"}
{"role":"assistant","tool_calls":[{"id":"test","name":"bash","arguments":{"command":"go test ./..."}}]}
{"type":"tool","tool_call_id":"test","name":"bash","content":"ok","verification":"passed"}
{"role":"assistant","content":"Parser fixed"}
`
	if err := os.WriteFile(input, []byte(transcript), 0600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(dir, "memory.jsonl")
	extract := func(session string) {
		t.Helper()
		var out, stderr bytes.Buffer
		code := run([]string{"extract", "--distill", "--consolidate", "--input", input, "--project-db", db, "--session", session, "--project", "demo/repo", "--base-commit", "revision-a"}, &out, &stderr, productionDependencies())
		if code != 0 {
			t.Fatalf("extract exit=%d: %s", code, stderr.String())
		}
	}
	readOps := func() (*slice.Slice, slice.Store) {
		t.Helper()
		st, err := slice.NewFileStore(db)
		if err != nil {
			t.Fatal(err)
		}
		all, err := st.ListAll()
		if err != nil {
			closeStore(st)
			t.Fatal(err)
		}
		for _, sl := range all {
			if bytes.HasPrefix(sl.Content, []byte("Repo operations")) {
				return sl, st
			}
		}
		closeStore(st)
		t.Fatal("missing observed repo-ops card")
		return nil, nil
	}
	extract("session-a")
	original, st := readOps()
	original.Stats = slice.SliceStats{Hits: 4, Injected: 3, Harmful: 1, UserFeedback: -1, LastUsed: 123}
	original.Weight = .4
	if err := st.Put(original); err != nil {
		t.Fatal(err)
	}
	closeStore(st)
	extract("session-b")
	extract("session-b") // extraction replay is not a new source or new feedback
	got, st := readOps()
	defer closeStore(st)
	if got.ID != original.ID || got.Meta.SourceSession != "session-b" || !reflect.DeepEqual(got.Meta.SourceSessions, []string{"session-a", "session-b"}) {
		t.Errorf("identical card lost observed sessions: %+v", got.Meta)
	}
	if got.Stats != original.Stats || got.Weight != original.Weight {
		t.Errorf("extraction reset/elevated feedback: stats=%+v weight=%v; want %+v/%v", got.Stats, got.Weight, original.Stats, original.Weight)
	}
}

func TestExtractDoesNotBorrowIncompatibleCardHistory(t *testing.T) {
	for _, mismatch := range []string{"origin", "project", "commit", "deps", "verification", "content"} {
		t.Run(mismatch, func(t *testing.T) {
			input := filepath.Join(t.TempDir(), "empty.jsonl")
			if err := os.WriteFile(input, []byte("{}\n"), 0600); err != nil {
				t.Fatal(err)
			}
			incoming := &slice.Slice{ID: "same-id", Type: slice.Result, Scope: slice.Project, Content: []byte("fixed parser"), Weight: 1, Meta: slice.SliceMeta{SourceSession: "new", Origin: slice.OriginUserCurated, ProjectSlug: "repo", BaseCommit: "revision", ResultStatus: slice.ResultStatusProbation}}
			old := *incoming
			old.Meta.SourceSession = "old"
			old.Meta.SourceSessions = []string{"old", "older"}
			old.Stats = slice.SliceStats{Injected: 9, Harmful: 2, UserFeedback: -1}
			old.Weight = .2
			switch mismatch {
			case "origin":
				old.Meta.Origin = slice.OriginImport
			case "project":
				old.Meta.ProjectSlug = "other"
			case "commit":
				old.Meta.BaseCommit = "other"
			case "deps":
				old.Meta.Deps = map[string]string{"parser.go": "changed"}
			case "verification":
				old.Meta.ResultStatus = slice.ResultStatusVerified
				old.Meta.ResultVerifiedBy = "command"
				old.Meta.ResultVerificationEvidence = "go test ./..."
			case "content":
				old.Content = []byte("different content")
			}
			store := newFakeStore(&old)
			deps := dependencies{newExtractor: func() slice.Extractor { return &fakeExtractor{items: []*slice.Slice{incoming}} }, openStore: func(string) (slice.Store, error) { return store, nil }}
			var out, stderr bytes.Buffer
			if code := run([]string{"extract", "--input", input, "--db", filepath.Join(t.TempDir(), "store.db")}, &out, &stderr, deps); code != 0 {
				t.Fatalf("extract exit=%d: %s", code, stderr.String())
			}
			got := store.items["same-id"]
			if got.Meta.SourceSession != "new" || len(got.Meta.SourceSessions) != 0 || got.Stats != (slice.SliceStats{}) || got.Weight != 1 || got.Meta.ResultStatus != slice.ResultStatusProbation {
				t.Fatalf("borrowed incompatible %s evidence: %+v", mismatch, got)
			}
		})
	}
}

func TestExtractPropagatesHistoryReadFailure(t *testing.T) {
	input := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(input, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store := newFakeStore()
	store.getErr = errors.New("history disk read failed")
	deps := dependencies{
		newExtractor: func() slice.Extractor {
			return &fakeExtractor{items: []*slice.Slice{{ID: "new-card", Content: []byte("evidence")}}}
		},
		openStore: func(string) (slice.Store, error) { return store, nil },
	}
	var out, stderr bytes.Buffer
	if code := run([]string{"extract", "--input", input, "--db", filepath.Join(t.TempDir(), "store.db")}, &out, &stderr, deps); code != 1 || !strings.Contains(stderr.String(), "history disk read failed") {
		t.Fatalf("read error swallowed: exit=%d stderr=%q", code, stderr.String())
	}
	if len(store.items) != 0 {
		t.Fatal("write proceeded despite unavailable existing history")
	}
}

func TestExtractOrigin(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		want  slice.Origin
		valid bool
	}{
		{"manual_default", nil, slice.OriginUserCurated, true},
		{"manual_explicit", []string{"--origin", "user-curated"}, slice.OriginUserCurated, true},
		{"automatic", []string{"--origin", "session-auto"}, slice.OriginSessionAuto, true},
		{"reject_import", []string{"--origin", "import"}, "", false},
		{"reject_prefetch", []string{"--origin", "prefetch"}, "", false},
		{"reject_empty", []string{"--origin", ""}, "", false},
		{"reject_unknown", []string{"--origin", "trusted"}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			input, db := filepath.Join(dir, "session.jsonl"), filepath.Join(dir, "memory.db")
			transcript := "{\"role\":\"user\",\"content\":\"Fix parser crash\"}\n{\"role\":\"assistant\",\"content\":\"Parser fixed\"}\n"
			if err := os.WriteFile(input, []byte(transcript), 0600); err != nil {
				t.Fatal(err)
			}
			args := append([]string{"extract", "--distill", "--input", input, "--db", db, "--session", "observed-session"}, tc.args...)
			var out, stderr bytes.Buffer
			code := run(args, &out, &stderr, productionDependencies())
			if !tc.valid {
				if code == 0 || !strings.Contains(stderr.String(), "origin") {
					t.Fatalf("invalid origin accepted: exit=%d stderr=%q", code, stderr.String())
				}
				if _, err := os.Stat(db); !os.IsNotExist(err) {
					t.Fatal("invalid origin opened/wrote a store")
				}
				return
			}
			if code != 0 {
				t.Fatalf("extract exit=%d: %s", code, stderr.String())
			}
			st := openTestStore(t, db)
			all, err := st.ListAll()
			if err != nil || len(all) == 0 {
				t.Fatalf("stored slices=%d err=%v", len(all), err)
			}
			for _, item := range all {
				if item.Meta.Origin != tc.want {
					t.Errorf("slice %s origin=%q want %q", item.ID, item.Meta.Origin, tc.want)
				}
			}
		})
	}
}

func TestExtractAutoOriginDoesNotBorrowManualHistory(t *testing.T) {
	dir := t.TempDir()
	input, db := filepath.Join(dir, "session.jsonl"), filepath.Join(dir, "memory.db")
	if err := os.WriteFile(input, []byte("{\"role\":\"user\",\"content\":\"Fix parser crash\"}\n{\"role\":\"assistant\",\"content\":\"Parser fixed\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	extract := func(origin, session string) {
		t.Helper()
		var out, stderr bytes.Buffer
		if code := run([]string{"extract", "--input", input, "--db", db, "--origin", origin, "--session", session}, &out, &stderr, productionDependencies()); code != 0 {
			t.Fatalf("extract exit=%d: %s", code, stderr.String())
		}
	}
	extract("user-curated", "manual")
	st, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	all, err := st.ListAll()
	if err != nil || len(all) == 0 {
		t.Fatalf("slices=%d err=%v", len(all), err)
	}
	item := all[0]
	item.Stats.Injected = 9
	item.Meta.SourceSessions = []string{"manual", "manual-other"}
	if err := st.Put(item); err != nil {
		t.Fatal(err)
	}
	closeStore(st)
	extract("session-auto", "automatic")
	st = openTestStore(t, db)
	got, err := st.Get(item.ID)
	if err != nil || got == nil {
		t.Fatalf("Get=%v err=%v", got, err)
	}
	if got.Meta.Origin != slice.OriginSessionAuto || got.Stats.Injected != 0 || len(got.Meta.SourceSessions) != 0 || got.Meta.SourceSession != "automatic" {
		t.Fatalf("automatic extraction borrowed manual trust/history: %+v", got)
	}
}
