package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/slice"
	"semantix/kernel/usage"
)

// writeUsageLog appends events to a fresh usage log at path.
func writeUsageLog(t *testing.T, path string, events []usage.Event) {
	t.Helper()
	r, err := usage.NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if err := r.Append(e); err != nil {
			t.Fatal(err)
		}
	}
}

// writeSliceStore puts slices into a fresh store at db.
func writeSliceStore(t *testing.T, db string, sls []*slice.Slice) {
	t.Helper()
	store, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, sl := range sls {
		if err := store.Put(sl); err != nil {
			t.Fatal(err)
		}
	}
}

// sampleSlices builds a small cross-session library: two sessions, mostly
// overlapping queries (clear hits), one unique query (miss).
func sampleSlices() []*slice.Slice {
	return []*slice.Slice{
		{ID: "a1", Type: slice.Prompt, Scope: slice.Project, Content: []byte("修复 go 测试失败"), Meta: slice.SliceMeta{SourceSession: "s1"}},
		{ID: "a2", Type: slice.Prompt, Scope: slice.Project, Content: []byte("修复 go 测试失败"), Meta: slice.SliceMeta{SourceSession: "s2"}},
		{ID: "b1", Type: slice.Prompt, Scope: slice.Project, Content: []byte("加 CI 配置"), Meta: slice.SliceMeta{SourceSession: "s1"}},
		{ID: "b2", Type: slice.Prompt, Scope: slice.Project, Content: []byte("加 CI 配置"), Meta: slice.SliceMeta{SourceSession: "s2"}},
		{ID: "c1", Type: slice.Prompt, Scope: slice.Project, Content: []byte("部署到服务器"), Meta: slice.SliceMeta{SourceSession: "s3"}},
	}
}

func TestRunDashboardANSIBlocks(t *testing.T) {
	dir := t.TempDir()
	usagePath := filepath.Join(dir, "usage.jsonl")
	writeUsageLog(t, usagePath, []usage.Event{
		{Turn: 1, TokensIn: 1000, TokensOut: 200, CacheHitToken: 600},
		{Turn: 2, TokensIn: 800, TokensOut: 150, InjectedTokens: 300},
		{Turn: 3, TokensIn: 900, TokensOut: 180, L3Reuse: true},
	})
	db := filepath.Join(dir, "project.db")
	writeSliceStore(t, db, sampleSlices())

	var out, errOut bytes.Buffer
	if code := runDashboard([]string{"--db", db, "--usage", usagePath}, &out, &errOut, productionDependencies()); code != 0 {
		t.Fatalf("dashboard exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	s := out.String()
	for _, want := range []string{
		"semantix dashboard",
		"💰 Cost savings",
		"🎯 Cache hit rate",
		"🗂 Zone distribution",
		"📦 Slice library",
		"█", "░", // bar characters (U31 acceptance)
		"2 / 3 turns", // 1 L3 + 1 L2 of 3 turns
		"5 / 5000 slices",    // library total / capacity water level
		"3 cross-session sessions",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("output missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "\x1b[") {
		t.Fatalf("non-TTY output must not contain ANSI escapes:\n%q", s)
	}
}

func TestRunDashboardJSON(t *testing.T) {
	dir := t.TempDir()
	usagePath := filepath.Join(dir, "usage.jsonl")
	writeUsageLog(t, usagePath, []usage.Event{
		{Turn: 1, TokensIn: 1000, TokensOut: 200, CacheHitToken: 600},
		{Turn: 2, TokensIn: 800, TokensOut: 150, L3Reuse: true},
	})
	db := filepath.Join(dir, "project.db")
	writeSliceStore(t, db, sampleSlices())

	var out, errOut bytes.Buffer
	if code := runDashboard([]string{"--db", db, "--usage", usagePath, "--json"}, &out, &errOut, productionDependencies()); code != 0 {
		t.Fatalf("dashboard --json exit code = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("JSON output must not contain ANSI escapes:\n%q", out.String())
	}

	// §4.2 envelope, jq-parseable.
	var env envelope
	if err := json.Unmarshal(out.Bytes(), &env); err != nil {
		t.Fatalf("envelope not JSON-parseable: %v\n%s", err, out.String())
	}
	if !env.OK || env.Command != "dashboard" || env.Error != nil {
		t.Fatalf("envelope = ok:%v command:%q error:%+v, want ok dashboard", env.OK, env.Command, env.Error)
	}
	raw, err := json.Marshal(env.Data)
	if err != nil {
		t.Fatal(err)
	}
	var data dashboardPayload
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("data not parseable: %v\n%s", err, raw)
	}

	if data.HitRate.Events != 2 || data.HitRate.L3Hits != 1 || data.HitRate.L2Hits != 0 || data.HitRate.Hits != 1 {
		t.Fatalf("hit_rate = %+v, want events=2 l3=1 l2=0 hits=1", data.HitRate)
	}
	if data.Savings.SavingsUSD <= 0 || data.Savings.SavingsRate <= 0 {
		t.Fatalf("savings = %+v, want positive", data.Savings)
	}
	if data.Zones.Total != 5 {
		t.Fatalf("zones = %+v, want total 5", data.Zones)
	}
	// sampleSlices: two identical pairs (4 hits) + one unique query (1 miss),
	// deterministic under BM25 — pin the split so a classifyTop1 that always
	// returns Miss cannot pass.
	if data.Zones.Hit != 4 || data.Zones.Grey != 0 || data.Zones.Miss != 1 {
		t.Fatalf("zones = %+v, want hit=4 grey=0 miss=1", data.Zones)
	}
	if data.Library.Slices != 5 || data.Library.CrossSessions != 3 {
		t.Fatalf("library = %+v, want slices=5 cross_sessions=3", data.Library)
	}
}

func TestRunDashboardMissingDataFailsOpen(t *testing.T) {
	dir := t.TempDir()
	missingDB := filepath.Join(dir, "no.db")
	missingUsage := filepath.Join(dir, "no.jsonl")

	var out, errOut bytes.Buffer
	if code := runDashboard([]string{"--db", missingDB, "--usage", missingUsage}, &out, &errOut, productionDependencies()); code != 0 {
		t.Fatalf("missing data must fail open with exit 0, got %d; stderr:\n%s", code, errOut.String())
	}
	s := out.String()
	for _, want := range []string{"semantix dashboard", "💰 Cost savings", "🎯 Cache hit rate", "📦 Slice library", "0 / 0 turns"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing-data output missing %q:\n%s", want, s)
		}
	}
	if !strings.Contains(s, "no slices in library") {
		t.Fatalf("missing-data output should show empty zone block:\n%s", s)
	}
	// Read-only guarantee: the dashboard must not have created the store.
	if _, err := os.Stat(missingDB); !os.IsNotExist(err) {
		t.Fatalf("dashboard created the missing store %s (must stay read-only)", missingDB)
	}
}

func TestRunDashboardConfigDefaultDB(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "cfg.db")
	writeSliceStore(t, db, sampleSlices())
	cfg := filepath.Join(dir, "semantix.toml")
	if err := os.WriteFile(cfg, []byte("[store]\ndb = "+strconvQuote(db)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	// No --db: the U20 config store.db must be picked up.
	code := runDashboard([]string{"--config", cfg, "--usage", filepath.Join(dir, "usage.jsonl")}, &out, &errOut, productionDependencies())
	if code != 0 {
		t.Fatalf("dashboard exit = %d, want 0; stderr:\n%s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "5 / 5000 slices") {
		t.Fatalf("config store.db not used as dashboard default:\n%s", out.String())
	}

	// --db flag must override the config value.
	other := filepath.Join(dir, "other.db")
	writeSliceStore(t, other, sampleSlices()[:1])
	out.Reset()
	if code := runDashboard([]string{"--config", cfg, "--db", other, "--usage", filepath.Join(dir, "usage.jsonl")}, &out, &errOut, productionDependencies()); code != 0 {
		t.Fatalf("dashboard --db override exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "1 / 5000 slices") {
		t.Fatalf("--db override not applied:\n%s", out.String())
	}
}

func TestRunDashboardInvalidConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(cfg, []byte("store = { db = 123 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if code := runDashboard([]string{"--config", cfg}, &out, &errOut, productionDependencies()); code != 2 {
		t.Fatalf("invalid config must exit 2, got %d", code)
	}
	if !strings.Contains(errOut.String(), "dashboard:") {
		t.Fatalf("stderr missing dashboard error prefix:\n%s", errOut.String())
	}
	// Missing explicit config file is an IO error → exit 1.
	out.Reset()
	errOut.Reset()
	if code := runDashboard([]string{"--config", filepath.Join(dir, "nope.toml")}, &out, &errOut, productionDependencies()); code != 1 {
		t.Fatalf("missing explicit config must exit 1, got %d", code)
	}
}

func TestRunDashboardBadFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	for _, args := range [][]string{
		{"--width", "0"},
		{"--width", "65"},
		{"--bogus"},
		{"positional"},
	} {
		if code := runDashboard(args, &out, &errOut, productionDependencies()); code != 2 {
			t.Fatalf("runDashboard(%v) exit = %d, want 2", args, code)
		}
		out.Reset()
		errOut.Reset()
	}
}

func TestBarAndBarLen(t *testing.T) {
	if got := bar(0.5, 10); got != "█████░░░░░" {
		t.Fatalf("bar(0.5,10) = %q", got)
	}
	if got := bar(1.5, 10); got != "██████████" {
		t.Fatalf("bar(1.5,10) = %q", got)
	}
	if got := bar(-1, 10); got != "░░░░░░░░░░" {
		t.Fatalf("bar(-1,10) = %q", got)
	}
	if got := bar(0, 0); got != "" {
		t.Fatalf("bar(0,0) = %q, want empty", got)
	}
	if got := barLen(3, 10); got != "███" {
		t.Fatalf("barLen(3,10) = %q", got)
	}
	if got := barLen(0, 10); got != "" {
		t.Fatalf("barLen(0,10) = %q, want empty", got)
	}
	if got := barLen(99, 10); got != "██████████" {
		t.Fatalf("barLen(99,10) = %q", got)
	}
}

// strconvQuote wraps s in Go double quotes (config TOML string).
func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
