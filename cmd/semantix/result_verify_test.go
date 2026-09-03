package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

func resultProbationFixture(t *testing.T, typ slice.SliceType, completeProvenance bool) string {
	t.Helper()
	db := filepath.Join(t.TempDir(), "lib.db")
	store, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore(store)
	meta := slice.SliceMeta{ResultStatus: slice.ResultStatusProbation}
	if completeProvenance {
		meta.SourceSession = "session-1"
		meta.ProjectSlug = "owner/repo"
		meta.Origin = slice.OriginSessionAuto
	}
	if err := store.Put(&slice.Slice{ID: "result-1", Type: typ, Scope: slice.Project, Content: []byte("fixed"), Meta: meta}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestVerifyResultPromotesOfficialResolvedAndAudits(t *testing.T) {
	db := resultProbationFixture(t, slice.Result, true)
	auditPath := filepath.Join(t.TempDir(), "audit.jsonl")
	var out bytes.Buffer
	code := run([]string{"verify-result", "result-1", "--method", "official", "--evidence", "swebench:resolved", "--db", db, "--audit-db", auditPath}, &out, &emptyStderr{}, productionDependencies())
	if code != 0 {
		t.Fatalf("verify-result code=%d out=%s", code, out.String())
	}
	got := readSlice(t, db, "result-1")
	if got.Meta.ResultStatus != slice.ResultStatusVerified || got.Meta.ResultVerifiedBy != "official" || got.Meta.ResultVerificationEvidence != "swebench:resolved" {
		t.Fatalf("verified metadata = %+v", got.Meta)
	}
	raw, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"action":"result_verify"`) || !strings.Contains(string(raw), `"method":"official"`) {
		t.Fatalf("audit=%s", raw)
	}
}

func TestVerifyResultRejectsNonResultAndIncompleteUserProvenance(t *testing.T) {
	for _, tc := range []struct {
		name string
		typ  slice.SliceType
		full bool
	}{
		{name: "non-result", typ: slice.Context, full: true},
		{name: "user-missing-provenance", typ: slice.Result, full: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := resultProbationFixture(t, tc.typ, tc.full)
			var out bytes.Buffer
			code := run([]string{"verify-result", "result-1", "--method", "user", "--evidence", "confirmed", "--db", db}, &out, &emptyStderr{}, productionDependencies())
			if code == 0 {
				t.Fatalf("unexpected promotion: %s", out.String())
			}
			if got := readSlice(t, db, "result-1"); got.Meta.EffectiveResultStatus() != slice.ResultStatusProbation {
				t.Fatalf("status=%s, want probation", got.Meta.ResultStatus)
			}
		})
	}
}
