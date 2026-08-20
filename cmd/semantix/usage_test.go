package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"semantix/kernel/usage"
)

func TestRunUsageSummary(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "usage.jsonl")
	r, err := usage.NewRecorder(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []usage.Event{
		{Turn: 1, TokensIn: 1000, TokensOut: 200, CacheHitToken: 600, SliceHits: 2},
		{Turn: 2, TokensIn: 800, TokensOut: 150, L3Reuse: true},
	} {
		if err := r.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	if code := runUsage([]string{"--db", logPath}, &out, productionDependencies()); code != 0 {
		t.Fatalf("usage exit code = %d, want 0", code)
	}
	s := out.String()
	for _, want := range []string{
		"events\t2", "tokens_in\t1800", "cache_hit_tokens\t600",
		"l3_reuses\t1", "savings_usd\t", "savings_rate\t",
		// Iconic summary (Issue #152).
		"💰 节省成本", "📈 节省率", "🧠 L3 复用", "📦 命中切片", "slice_hits\t2", "█",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("output missing %q:\n%s", want, s)
		}
	}
}

func TestRunUsageMissingDB(t *testing.T) {
	var out bytes.Buffer
	if code := runUsage([]string{"--db", filepath.Join(t.TempDir(), "nope.jsonl")}, &out, productionDependencies()); code != 1 {
		t.Fatalf("missing db is a runtime/IO error, must exit 1, got %d", code)
	}
}

// TestRunUsageJSON verifies --json emits a jq-parseable envelope whose data
// appends the new slice_hits field while keeping the older fields.
func TestRunUsageJSON(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "usage.jsonl")
	r, err := usage.NewRecorder(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Append(usage.Event{Turn: 1, TokensIn: 1000, TokensOut: 200, CacheHitToken: 600, SliceHits: 4}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := runUsage([]string{"--db", logPath, "--json"}, &out, productionDependencies()); code != 0 {
		t.Fatalf("usage --json exit code = %d, want 0", code)
	}
	var env struct {
		OK      bool   `json:"ok"`
		Command string `json:"command"`
		Data    struct {
			Events    int `json:"events"`
			SliceHits int `json:"slice_hits"`
		} `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("--json output not parseable: %v\n%s", err, out.String())
	}
	if !env.OK || env.Command != "usage" {
		t.Fatalf("envelope = %+v", env)
	}
	if env.Data.SliceHits != 4 || env.Data.Events != 1 {
		t.Fatalf("data = %+v, want slice_hits=4 events=1", env.Data)
	}
}

func TestRunUsageWithEvolve(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "usage.jsonl")
	r, err := usage.NewRecorder(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Append(usage.Event{Turn: 1, TokensIn: 1000, TokensOut: 200, CacheHitToken: 900}); err != nil {
		t.Fatal(err)
	}
	evolveDir := filepath.Join(dir, "evolve")
	var out bytes.Buffer
	if code := runUsage([]string{"--db", logPath, "--evolve-db", evolveDir}, &out, productionDependencies()); code != 0 {
		t.Fatalf("usage --evolve exit code = %d, want 0", code)
	}
	s := out.String()
	if !strings.Contains(s, "evolve_epoch\t1") {
		t.Fatalf("evolve output missing epoch:\n%s", s)
	}
	if !strings.Contains(s, "evolve_tau_l2\t") {
		t.Fatalf("evolve output missing tau:\n%s", s)
	}
	// State file persisted (0600 on POSIX; Windows ignores the perm bits).
	if runtime.GOOS != "windows" {
		st, err := os.Stat(filepath.Join(evolveDir, "params.json"))
		if err != nil {
			t.Fatalf("evolve state not persisted: %v", err)
		}
		if st.Mode().Perm() != 0o600 {
			t.Fatalf("evolve state perms = %o, want 600", st.Mode().Perm())
		}
	}
	// Replay semantics (Issue #220): the same log yields the same state —
	// rerunning must NOT advance the epoch, only new events do.
	var out2 bytes.Buffer
	if code := runUsage([]string{"--db", logPath, "--evolve-db", evolveDir}, &out2, productionDependencies()); code != 0 {
		t.Fatalf("second usage --evolve exit code = %d", code)
	}
	if !strings.Contains(out2.String(), "evolve_epoch\t1") {
		t.Fatalf("replay over the same log must be idempotent:\n%s", out2.String())
	}
	if err := r.Append(usage.Event{Turn: 2, TokensIn: 1000, TokensOut: 100, CacheHitToken: 500}); err != nil {
		t.Fatal(err)
	}
	var out3 bytes.Buffer
	if code := runUsage([]string{"--db", logPath, "--evolve-db", evolveDir}, &out3, productionDependencies()); code != 0 {
		t.Fatalf("third usage --evolve exit code = %d", code)
	}
	if !strings.Contains(out3.String(), "evolve_epoch\t2") {
		t.Fatalf("a new event must advance the epoch:\n%s", out3.String())
	}
}

// TestFeedEvolveAdjustsTauAfterHistory proves the loop is no longer inert
// (Issue #220): with enough high-hit turns the engine leaves the cold-start
// window (FreezeEpochs) and actually moves TauL2 off its default.
func TestFeedEvolveAdjustsTauAfterHistory(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "usage.jsonl")
	r, err := usage.NewRecorder(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 65; i++ {
		if err := r.Append(usage.Event{Turn: uint64(i), TokensIn: 1000, TokensOut: 100, CacheHitToken: 900}); err != nil {
			t.Fatal(err)
		}
	}
	evolveDir := filepath.Join(dir, "evolve")
	var out bytes.Buffer
	if err := feedEvolve(evolveDir, logPath, &out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "evolve_epoch\t65") {
		t.Fatalf("epoch must count consumed turns:\n%s", s)
	}
	// 65 signals at 0.9 hit ratio: hit EWMA ≥ HitTarget with zero pollution
	// after the 60-epoch cold start → exactly one -TauStep relaxation
	// (0.55 → 0.50), then the freeze window blocks further movement.
	if !strings.Contains(s, "evolve_tau_l2\t0.500") {
		t.Fatalf("tau must relax after sustained hits:\n%s", s)
	}
	// The persisted snapshot carries the adjusted value for consumers.
	st, err := loadEvolveState(evolveDir)
	if err != nil || st == nil {
		t.Fatalf("loadEvolveState: st=%v err=%v", st, err)
	}
	if diff := st.Params.TauL2 - 0.50; diff < -1e-9 || diff > 1e-9 {
		t.Fatalf("persisted TauL2 = %v, want 0.50", st.Params.TauL2)
	}
	// Deterministic replay: a second pass reproduces the identical output.
	var again bytes.Buffer
	if err := feedEvolve(evolveDir, logPath, &again); err != nil {
		t.Fatal(err)
	}
	if again.String() != s {
		t.Fatalf("replay must be deterministic:\nfirst:\n%s\nsecond:\n%s", s, again.String())
	}
}

// TestLoadEvolveState covers the consumer-side contract: missing file is a
// silent no-op, corrupt file is a visible error.
func TestLoadEvolveState(t *testing.T) {
	dir := t.TempDir()
	if st, err := loadEvolveState(dir); st != nil || err != nil {
		t.Fatalf("missing file: st=%v err=%v, want nil/nil", st, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "params.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadEvolveState(dir); err == nil {
		t.Fatal("corrupt file must surface an error")
	}
}
