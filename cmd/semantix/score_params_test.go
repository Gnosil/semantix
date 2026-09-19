package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// scoreParamsFixture writes a one-slice library whose weight separates
// cleanly under different half-lives: last used 5 days ago, some use. With
// the default 30-day half-life the recency factor stays ≈0.89; with a 1-day
// half-life it collapses to ≈0.03.
func scoreParamsFixture(t *testing.T, db string) {
	t.Helper()
	ts := time.Now().Unix() - 5*86400
	line := fmt.Sprintf(
		`{"ID":"s1","Type":0,"Scope":1,"Content":"YWJj","Stats":{"Hits":2,"Injected":1,"LastUsed":%d},"Weight":0.9,"created_at":%d}`+"\n",
		ts, ts)
	if err := os.WriteFile(db, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestGCScoreParamsFile: gc --score-params <json> must load the full
// five-hyperparameter ScoreParams from the file and rescore with it — the
// offline fitter's write-back channel (docs/specs/local-retrieval-model.md
// §3 C). A 1-day half-life must crush the weight of a 5-day-old slice,
// where the built-in 30-day default barely dents it.
func TestGCScoreParamsFile(t *testing.T) {
	deps, db := buildMaintenanceDeps(t)
	scoreParamsFixture(t, db)

	paramsPath := filepath.Join(t.TempDir(), "score_params.json")
	if err := os.WriteFile(paramsPath, []byte(
		`{"half_life_days": 1, "grace_days": 0, "freq_pseudo": 3, "reject_cost": 2, "feedback_gain": 0.5}`,
	), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"gc", "--score-params", paramsPath, "--db", db}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("gc code = %d, stderr = %q", code, stderr.String())
	}
	store := openTestStore(t, db)
	items, err := store.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("len = %d, want 1", len(items))
	}
	if w := items[0].Weight; w > 0.05 {
		t.Errorf("weight = %v, want < 0.05 under half_life_days=1", w)
	}

	// Control: without the flag the default 30-day half-life keeps the
	// weight an order of magnitude higher.
	deps2, db2 := buildMaintenanceDeps(t)
	scoreParamsFixture(t, db2)
	stdout.Reset()
	stderr.Reset()
	code = run([]string{"gc", "--db", db2}, &stdout, &stderr, deps2)
	if code != 0 {
		t.Fatalf("control gc code = %d, stderr = %q", code, stderr.String())
	}
	store2 := openTestStore(t, db2)
	items2, err := store2.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if w := items2[0].Weight; w < 0.3 {
		t.Errorf("control weight = %v, want > 0.3 under default params", w)
	}
}

// TestGCScoreParamsFileInvalid: a missing or malformed params file is a
// usage error (exit 2) — silently falling back to defaults would let a
// broken trainer pass unnoticed.
func TestGCScoreParamsFileInvalid(t *testing.T) {
	for name, content := range map[string]string{
		"missing": "",                       // path never written
		"badjson": `{"half_life_days": tru`, // malformed
	} {
		t.Run(name, func(t *testing.T) {
			deps, db := buildMaintenanceDeps(t)
			scoreParamsFixture(t, db)
			paramsPath := filepath.Join(t.TempDir(), "score_params.json")
			if content != "" {
				if err := os.WriteFile(paramsPath, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr bytes.Buffer
			code := run([]string{"gc", "--score-params", paramsPath, "--db", db}, &stdout, &stderr, deps)
			if code != 2 {
				t.Fatalf("code = %d, want 2 (usage error), stderr = %q", code, stderr.String())
			}
			if !strings.Contains(stderr.String(), "score-params") {
				t.Errorf("stderr = %q, want mention of score-params", stderr.String())
			}
		})
	}
}
