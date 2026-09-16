package slice

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const pruneNow int64 = 1800000000

func pruneSlice(id string, days int) *Slice {
	return &Slice{ID: id, Type: Context, Scope: Project, Content: []byte("body-" + id), CreatedAt: pruneNow - int64(days)*86400,
		Meta: SliceMeta{Origin: OriginSessionAuto, ProjectSlug: "test/repo", SourceSession: "session-1"}}
}

func pruneEntries(slices ...*Slice) map[string]*storedEntry {
	out := map[string]*storedEntry{}
	for _, s := range slices {
		out[s.ID] = entryFromSlice(s)
	}
	return out
}

func prunePlan(t *testing.T, opts PruneOptions, slices ...*Slice) PruneResult {
	t.Helper()
	opts.Now = pruneNow
	if opts.Scope == Session {
		opts.Scope = Project
	}
	opts, err := opts.normalized()
	if err != nil {
		t.Fatal(err)
	}
	r, err := planPrune(pruneEntries(slices...), opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestPruneRulesAndStablePlan(t *testing.T) {
	dupA, dupB := pruneSlice("a", 60), pruneSlice("b", 60)
	dupB.Content = dupA.Content
	dupB.Meta.SourceSession = "session-2"
	old := pruneSlice("old", 120)
	old.Stats.LastUsed = pruneNow - 100*86400
	old.Stats.Hits = 5
	probation := pruneSlice("probation", 120)
	probation.Type = Result
	probation.Meta.ResultStatus = ResultStatusProbation
	probation.Stats.Rejected = 1
	slices := []*Slice{dupB, old, probation, dupA}
	r := prunePlan(t, PruneOptions{}, slices...)
	if r.Checked != 4 || r.Kept != 1 || r.Removed != 0 || len(r.Candidates) != 3 {
		t.Fatalf("plan: %+v", r)
	}
	if r.Candidates[0].ID != "b" || r.Candidates[0].RetainedID != "a" || !reflect.DeepEqual(r.Candidates[0].Reasons, []string{"exact_duplicate"}) {
		t.Fatalf("duplicate: %+v", r.Candidates[0])
	}
	if !reflect.DeepEqual(r.Candidates[2].Reasons, []string{"stale_unused", "stale_probation", "stale_rejected"}) {
		t.Fatalf("probation: %+v", r.Candidates[2])
	}
	for i, j := 0, len(slices)-1; i < j; i, j = i+1, j-1 {
		slices[i], slices[j] = slices[j], slices[i]
	}
	if again := prunePlan(t, PruneOptions{}, slices...); !reflect.DeepEqual(r, again) {
		t.Fatal("plan depends on input order")
	}
	if next := prunePlan(t, PruneOptions{}, dupA); len(next.Candidates) != 0 {
		t.Fatal("surviving representative selected on repeat")
	}
	// All-stale duplicates have no surviving representative: no dangling IDs.
	dupA.CreatedAt = old.CreatedAt
	dupB.CreatedAt = old.CreatedAt
	r = prunePlan(t, PruneOptions{}, dupA, dupB)
	if len(r.Candidates) != 2 || r.Candidates[0].RetainedID != "" || r.Candidates[1].RetainedID != "" {
		t.Fatalf("stale group: %+v", r)
	}
}

func TestPruneProtectsAmbiguousAndValuableSlices(t *testing.T) {
	cases := map[string]func(*Slice){
		"unknown_age":            func(s *Slice) { s.CreatedAt = 0 },
		"future_age":             func(s *Slice) { s.CreatedAt = pruneNow + 1 },
		"new":                    func(s *Slice) { s.CreatedAt = pruneNow - 86400 },
		"recent_hit":             func(s *Slice) { s.Stats.LastUsed = pruneNow - 86400; s.Stats.Hits = 1 },
		"hit_unknown_time":       func(s *Slice) { s.Stats.Hits = 1 },
		"injection_unknown_time": func(s *Slice) { s.Stats.Injected = 1 },
		"curated":                func(s *Slice) { s.Meta.Origin = OriginUserCurated },
		"feedback":               func(s *Slice) { s.Stats.UserFeedback = 1 },
		"useful":                 func(s *Slice) { s.Stats.Useful = 1 },
		"verified":               func(s *Slice) { s.Type = Result; s.Meta.ResultStatus = ResultStatusVerified },
		"unknown_status":         func(s *Slice) { s.Meta.ResultStatus = "future-state" },
		"unknown_type":           func(s *Slice) { s.Type = 99 },
		"unknown_origin":         func(s *Slice) { s.Meta.Origin = "future-origin" },
		"negative_use_time":      func(s *Slice) { s.Stats.LastUsed = -1 },
	}
	for name, modify := range cases {
		t.Run(name, func(t *testing.T) {
			s := pruneSlice(name, 180)
			modify(s)
			if r := prunePlan(t, PruneOptions{}, s); len(r.Candidates) != 0 {
				t.Fatalf("protected slice selected: %+v", r)
			}
		})
	}
}

func TestPruneDuplicateApplicabilityAndScope(t *testing.T) {
	a := pruneSlice("a", 60)
	cases := map[string]func(*Slice){
		"project":    func(s *Slice) { s.Meta.ProjectSlug = "other/repo" },
		"scope":      func(s *Slice) { s.Scope = User },
		"type":       func(s *Slice) { s.Type = Prompt },
		"origin":     func(s *Slice) { s.Meta.Origin = OriginImport },
		"deps":       func(s *Slice) { s.Meta.Deps = map[string]string{"x": "existing-fingerprint"} },
		"commit":     func(s *Slice) { s.Meta.BaseCommit = "different" },
		"context":    func(s *Slice) { s.Meta.ContextHash = "different" },
		"model":      func(s *Slice) { s.Meta.Model = "different" },
		"whitespace": func(s *Slice) { s.Content = append(append([]byte{}, s.Content...), ' ') },
	}
	for name, modify := range cases {
		t.Run(name, func(t *testing.T) {
			b := pruneSlice("b", 60)
			b.Content = a.Content
			modify(b)
			if r := prunePlan(t, PruneOptions{}, a, b); len(r.Candidates) != 0 {
				t.Fatalf("incompatible duplicates: %+v", r)
			}
		})
	}
	user := pruneSlice("user", 180)
	user.Scope = User
	session := pruneSlice("session", 180)
	session.Scope = Session
	project := pruneSlice("project", 180)
	r := prunePlan(t, PruneOptions{Scope: User}, user, session, project)
	if r.Checked != 1 || len(r.Candidates) != 1 || r.Candidates[0].ID != "user" {
		t.Fatalf("scope: %+v", r)
	}
}

func TestPruneMissingDependencies(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "present"), []byte("exists"), 0600); err != nil {
		t.Fatal(err)
	}
	opts := PruneOptions{Project: "test/repo", ProjectRoot: root}
	missing := pruneSlice("missing", 60)
	missing.Meta.Deps = map[string]string{"gone/file": "old"}
	present := pruneSlice("present", 60)
	present.Meta.Deps = map[string]string{"present": "old"}
	escape := pruneSlice("escape", 180)
	escape.Meta.Deps = map[string]string{"../outside": "old"}
	other := pruneSlice("other", 180)
	other.Meta.ProjectSlug = "other/repo"
	other.Meta.Deps = missing.Meta.Deps
	r := prunePlan(t, opts, missing, present, escape, other)
	if r.Checked != 3 || len(r.Candidates) != 1 || r.Candidates[0].ID != "missing" || r.Candidates[0].Reasons[0] != "missing_dependency" {
		t.Fatalf("paths: %+v", r)
	}
	if len(r.SkippedChecks) != 1 {
		t.Fatal("ambiguous path check not reported")
	}
	if r := prunePlan(t, PruneOptions{}, missing); len(r.Candidates) != 0 || len(r.SkippedChecks) != 1 {
		t.Fatalf("unmapped: %+v", r)
	}
	// Intermediate non-directory is not evidence that the captured file vanished.
	missing.Meta.Deps = map[string]string{"present/child": "old", "gone": "old"}
	missing.CreatedAt = escape.CreatedAt
	if r := prunePlan(t, opts, missing); len(r.Candidates) != 0 {
		t.Fatalf("ambiguous path pruned: %+v", r)
	}
	missing.Meta.Deps = map[string]string{"C:/foreign/file": "old"}
	if r := prunePlan(t, opts, missing); len(r.Candidates) != 0 {
		t.Fatal("foreign path treated as a missing local dependency")
	}
}

func TestPruneRejectsSymlinkDependencies(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	s := pruneSlice("linked", 180)
	s.Meta.Deps = map[string]string{"link/missing": "old"}
	if r := prunePlan(t, PruneOptions{Project: "test/repo", ProjectRoot: root}, s); len(r.Candidates) != 0 {
		t.Fatal("followed a symlink")
	}
}

func TestPruneMetadataOnlyAndRawSize(t *testing.T) {
	s := pruneSlice("old", 180)
	s.Content = []byte("BODY_SECRET_MUST_NOT_APPEAR")
	s.Meta.ResultVerificationEvidence = "EVIDENCE_SECRET_MUST_NOT_APPEAR"
	s.Meta.SourceSession = "session\x1b[31m sk-123456789012345678901234"
	s.Embedding = []float32{0.123, 0.456, 0.789}
	r := prunePlan(t, PruneOptions{}, s)
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"BODY_SECRET", "EVIDENCE_SECRET", "123456789012345678901234", "0.123"} {
		if bytes.Contains(b, []byte(secret)) {
			t.Fatalf("leaked %s", secret)
		}
	}
	raw, _ := json.Marshal(dtoFromEntry(entryFromSlice(s)))
	if r.EstimatedReclaimableBytes != int64(len(raw)+1) {
		t.Fatalf("size estimate excludes stored data: %d", r.EstimatedReclaimableBytes)
	}
	if !strings.Contains(r.Candidates[0].SourceSession, "REDACTED") {
		t.Fatal("source not redacted")
	}
}
