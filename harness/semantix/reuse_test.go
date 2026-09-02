package semantix

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"semantix/harness/event"
	kernelevent "semantix/kernel/event"
	"semantix/kernel/slice"
)

// --- ReuseSummary / source aggregation (pure logic) ------------------------

func TestReuseSummaryLine(t *testing.T) {
	if s := (ReuseSummary{}).Line(); s != "" {
		t.Errorf("zero summary Line() = %q, want empty", s)
	}
	got := (ReuseSummary{Hits: 3, SavingsUSD: 0.0042, Sources: []string{"boot-1", "boot-2"}}).Line()
	for _, want := range []string{"📦 3 slices", "💰 $0.0042 saved", "🗂 from: boot-1, boot-2"} {
		if !strings.Contains(got, want) {
			t.Errorf("Line() %q missing %q", got, want)
		}
	}
}

func TestTopSources(t *testing.T) {
	got := topSources([]string{"boot-2", "boot-1", "boot-1", "boot-3", "boot-1", "boot-3", "boot-4", "boot-5"})
	want := []string{"boot-1", "boot-3", "boot-2"}
	if len(got) != len(want) {
		t.Fatalf("sources = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sources[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Empty sessions skipped; no sessions → empty.
	if got := topSources([]string{""}); len(got) != 0 {
		t.Errorf("empty sessions: %v", got)
	}
	if got := topSources(nil); len(got) != 0 {
		t.Errorf("nil sessions: %v", got)
	}
}

func TestReuseSummaryLineTaskTime(t *testing.T) {
	// Completed task: ⏱ + duration + (done).
	got := (ReuseSummary{Hits: 1, TaskElapsedSeconds: 83.2, TaskComplete: true}).Line()
	if !strings.Contains(got, "⏱ 1m23s (done)") {
		t.Errorf("completed task Line() = %q, want ⏱ 1m23s (done)", got)
	}
	// In-progress task: ⏱ + duration, no (done).
	got = (ReuseSummary{Hits: 1, TaskElapsedSeconds: 42}).Line()
	if !strings.Contains(got, "⏱ 42s") || strings.Contains(got, "(done)") {
		t.Errorf("in-progress task Line() = %q, want ⏱ 42s without (done)", got)
	}
	// Absent task time: no ⏱ segment.
	got = (ReuseSummary{Hits: 1}).Line()
	if strings.Contains(got, "⏱") {
		t.Errorf("absent task time Line() = %q, want no ⏱", got)
	}
}

func TestFormatTaskDuration(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{42, "42s"},
		{59.4, "59s"},
		{60, "1m0s"},
		{83.2, "1m23s"},
		{3600, "1h0m"},
		{3661, "1h1m"},
	}
	for _, c := range cases {
		if got := formatTaskDuration(c.in); got != c.want {
			t.Errorf("formatTaskDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatUSD(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0.0042, "0.0042"},
		{0.12, "0.12"},
		{1.5, "1.5"},
		{0, "0"},
	}
	for _, c := range cases {
		if got := formatUSD(c.in); got != c.want {
			t.Errorf("formatUSD(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- Bridge.Reuse over the in-process kernel store -------------------------

// writeKernelDir lays out a minimal kernel project directory: a slice store
// with the given slices plus a usage log with the given event lines.
// Returns the project dir (pass as Config.ProjectDir).
func writeKernelDir(t *testing.T, slicesIn []*slice.Slice, usageLines []string) string {
	t.Helper()
	dir := t.TempDir()
	kernelDir := filepath.Join(dir, ".semantix")
	if err := os.MkdirAll(kernelDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := slice.NewFileStore(filepath.Join(kernelDir, "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range slicesIn {
		if err := store.Put(s); err != nil {
			t.Fatal(err)
		}
	}
	closeSliceStore(store)
	if usageLines != nil {
		if err := os.WriteFile(filepath.Join(kernelDir, "usage.jsonl"),
			[]byte(strings.Join(usageLines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// reuseFixtureSlices: three project slices sharing query terms; boot-1 wins
// the frequency ranking (2 hits) over boot-2 (1 hit). Scores vary so the
// top-3 ordering stays deterministic for the fixture content.
func reuseFixtureSlices() []*slice.Slice {
	return []*slice.Slice{
		{ID: "a", Type: slice.Prompt, Scope: slice.Project, Content: []byte("修复 go 测试失败问题排查"), Meta: slice.SliceMeta{SourceSession: "boot-1"}},
		{ID: "b", Type: slice.Result, Scope: slice.Project, Content: []byte("go 测试修复步骤记录"), Meta: slice.SliceMeta{SourceSession: "boot-1"}},
		{ID: "c", Type: slice.Context, Scope: slice.Project, Content: []byte("测试失败修复经验总结"), Meta: slice.SliceMeta{SourceSession: "boot-2"}},
	}
}

func admissionFixtureSlices() []*slice.Slice {
	return []*slice.Slice{
		{ID: "ctx-strong", Type: slice.Context, Scope: slice.Project, Content: []byte("修复 go 测试失败"), Meta: slice.SliceMeta{SourceSession: "boot-1"}},
		{ID: "ctx-runner", Type: slice.Context, Scope: slice.Project, Content: []byte("修复 release process"), Meta: slice.SliceMeta{SourceSession: "boot-2"}},
		{ID: "memory-other", Type: slice.Memory, Scope: slice.Project, Content: []byte("部署 kubernetes 服务"), Meta: slice.SliceMeta{SourceSession: "boot-1"}},
		{ID: "prompt-blocked", Type: slice.Prompt, Scope: slice.Project, Content: []byte("修复 go 测试失败"), Meta: slice.SliceMeta{SourceSession: "boot-3"}},
		{ID: "result-blocked", Type: slice.Result, Scope: slice.Project, Content: []byte("修复 go 测试失败"), Meta: slice.SliceMeta{SourceSession: "boot-4"}},
	}
}

// usageL3Event is a usage-log line fully served by L3 reuse: billed cost is
// zero and the entire would-be cost counts as savings (0.27 USD/1M tokens ×
// 20000 tokens = 0.0054).
const usageL3Event = `{"turn":1,"tokens_in":20000,"l3_reuse":true,"at":1}`

// TestBridgeReuseHitsAndSources: the in-process lookup reads the project
// store, aggregates hits and top source sessions; the usage side reports the
// first cumulative savings snapshot.
func TestBridgeReuseHitsAndSources(t *testing.T) {
	dir := writeKernelDir(t, reuseFixtureSlices(), []string{usageL3Event})
	b := NewBridge(Config{Enabled: true, ProjectDir: dir, Budget: 4096})
	sum := b.Reuse(context.Background(), "修复 go 测试")
	if sum.Hits != 3 {
		t.Errorf("Hits = %d, want 3", sum.Hits)
	}
	want := []string{"boot-1", "boot-2"}
	if len(sum.Sources) != 2 || sum.Sources[0] != want[0] || sum.Sources[1] != want[1] {
		t.Errorf("Sources = %v, want %v", sum.Sources, want)
	}
	if sum.SavingsUSD != 0.0054 {
		t.Errorf("SavingsUSD = %v, want 0.0054 (first cumulative snapshot)", sum.SavingsUSD)
	}
}

func TestBridgeReuseForwardsKernelCacheObservation(t *testing.T) {
	dir := writeKernelDir(t, reuseFixtureSlices(), nil)
	var events []event.Event
	b := NewBridge(Config{Enabled: true, ProjectDir: dir})
	b.SetLabel("reuse-observation")
	b.Sink(event.FuncSink(func(e event.Event) { events = append(events, e) }))
	if got := b.Reuse(context.Background(), "修复 go 测试"); got.Hits == 0 {
		t.Fatal("Reuse returned no hits")
	}
	if len(events) != 1 || events[0].Kind != event.KernelCache || events[0].KernelCache == nil {
		t.Fatalf("forwarded events = %+v, want one kernel cache event", events)
	}
	if events[0].KernelCache.Op != "hit" || events[0].KernelCache.Layer != "L2" || len(events[0].KernelCache.SliceIDs) != 3 {
		t.Fatalf("kernel cache payload = %+v", events[0].KernelCache)
	}
}

func TestBridgeInjectRecordsStatsAndEvent(t *testing.T) {
	dir := writeKernelDir(t, admissionFixtureSlices(), nil)
	sessionsDir := t.TempDir()
	b := NewBridge(Config{Enabled: true, Inject: true, ProjectDir: dir, SessionsDir: sessionsDir, Budget: 4096})
	b.SetLabel("inject-stats")
	result := b.InjectDetailed(context.Background(), "修复 go 测试")
	if result.Text == "" || len(result.Targets) == 0 {
		t.Fatalf("InjectDetailed() = %+v, want injected slices", result)
	}
	if result.Diagnostics == nil || result.Diagnostics.MessageRole != "user" {
		t.Fatalf("injection message role = %+v, want user", result.Diagnostics)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := slice.NewFileStore(filepath.Join(dir, ".semantix", "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeSliceStore(store)
	for _, id := range result.Targets {
		got, err := store.Get(id)
		if err != nil || got == nil {
			t.Fatalf("Get(%q) = %v, %v", id, got, err)
		}
		if got.Stats.Injected != 1 || got.Stats.LastUsed == 0 {
			t.Fatalf("stats for %q = %+v, want one injection and LastUsed", id, got.Stats)
		}
	}

	raw, err := os.ReadFile(filepath.Join(sessionsDir, "inject-stats.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		e, err := kernelevent.FromJSON([]byte(line))
		if err == nil && e.Kind == kernelevent.SliceInject {
			found = true
		}
	}
	if !found {
		t.Fatalf("session JSONL missing SliceInject: %s", raw)
	}
}

func TestBridgeStrictAdmissionRejectsWrongTypesAndSmallLibrary(t *testing.T) {
	dir := writeKernelDir(t, reuseFixtureSlices(), nil)
	b := NewBridge(Config{Enabled: true, Mode: "strict", ProjectDir: dir})
	result := b.InjectDetailed(context.Background(), "修复 go 测试")
	if result.Text != "" || result.Diagnostics == nil {
		t.Fatalf("strict small-library result = %+v", result)
	}
	reasons := map[string]bool{}
	for _, candidate := range result.Diagnostics.Candidates {
		reasons[candidate.Reason] = true
	}
	if !reasons["type_not_allowed"] || !reasons["library_too_small"] {
		t.Fatalf("candidate reasons = %v, want type and library guards", reasons)
	}
}

func TestBridgeStrictAdmissionInjectsOnlyContextWithStrongEvidence(t *testing.T) {
	dir := writeKernelDir(t, admissionFixtureSlices(), nil)
	b := NewBridge(Config{Enabled: true, Mode: "strict", ProjectDir: dir})
	result := b.InjectDetailed(context.Background(), "修复 go 测试")
	if result.Text == "" || result.Diagnostics == nil || !result.Diagnostics.Injected {
		t.Fatalf("strict strong-evidence result = %+v", result)
	}
	if len(result.Targets) != 1 || result.Targets[0] != "ctx-strong" {
		t.Fatalf("targets = %v, want only ctx-strong", result.Targets)
	}
	if strings.Contains(result.Text, "prompt-blocked") || strings.Contains(result.Text, "result-blocked") {
		t.Fatalf("wrong-type slice leaked into injection:\n%s", result.Text)
	}
}

func TestBridgeShadowRetrievalEmitsDiagnosticsWithoutInjection(t *testing.T) {
	dir := writeKernelDir(t, reuseFixtureSlices(), nil)
	var events []event.Event
	b := NewBridge(Config{Enabled: true, Mode: "shadow", Inject: true, ProjectDir: dir, Budget: 4096})
	b.SetLabel("shadow-observation")
	b.Sink(event.FuncSink(func(e event.Event) { events = append(events, e) }))

	result := b.InjectDetailed(context.Background(), "修复 go 测试")
	if result.Text != "" {
		t.Fatalf("shadow Text = %q, want empty provider-visible injection", result.Text)
	}
	if result.Diagnostics == nil || result.Diagnostics.Mode != "shadow" || len(result.Diagnostics.Candidates) == 0 {
		t.Fatalf("shadow diagnostics = %+v", result.Diagnostics)
	}
	if result.Diagnostics.LibrarySize != 3 || result.Diagnostics.QueryBefore.SHA256 == "" || result.Diagnostics.QueryAfter.SHA256 == "" {
		t.Fatalf("shadow diagnostics missing replay context: %+v", result.Diagnostics)
	}
	if result.Diagnostics.Injected || result.Diagnostics.Bytes != 0 || result.Diagnostics.MessageRole != "" {
		t.Fatalf("shadow reported provider injection: %+v", result.Diagnostics)
	}
	if len(events) != 1 || events[0].KernelCache == nil || events[0].KernelCache.Op != "shadow" || events[0].KernelCache.Retrieval == nil {
		t.Fatalf("shadow events = %+v", events)
	}

	store, err := slice.NewFileStore(filepath.Join(dir, ".semantix", "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeSliceStore(store)
	for _, candidate := range result.Diagnostics.Candidates {
		got, err := store.Get(candidate.ID)
		if err != nil || got == nil {
			t.Fatalf("Get(%q) = %v, %v", candidate.ID, got, err)
		}
		if got.Stats.Injected != 0 {
			t.Fatalf("shadow mutated injection stats for %q: %+v", candidate.ID, got.Stats)
		}
	}
}

func TestBridgeRetrievalModeCompatibilityAndFailClosed(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want RetrievalMode
	}{
		{"legacy strict", Config{Enabled: true, Inject: true}, RetrievalStrict},
		{"legacy off", Config{Enabled: true}, RetrievalOff},
		{"explicit shadow", Config{Enabled: true, Inject: true, Mode: "shadow"}, RetrievalShadow},
		{"explicit strict", Config{Enabled: true, Mode: "strict"}, RetrievalStrict},
		{"disabled", Config{Enabled: false, Inject: true, Mode: "strict"}, RetrievalOff},
		{"unknown", Config{Enabled: true, Inject: true, Mode: "typo"}, RetrievalOff},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NewBridge(tc.cfg).RetrievalMode(); got != tc.want {
				t.Fatalf("RetrievalMode() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBridgeReuseSavingsDelta: the usage side attributes only the growth
// between snapshots; the first snapshot takes the full cumulative value.
func TestBridgeReuseSavingsDelta(t *testing.T) {
	ctx := context.Background()
	dir1 := writeKernelDir(t, reuseFixtureSlices(), []string{usageL3Event})
	dir2 := writeKernelDir(t, reuseFixtureSlices(), []string{usageL3Event, usageL3Event})

	// First snapshot: full cumulative savings is attributed.
	b := NewBridge(Config{Enabled: true, ProjectDir: dir1})
	if got := b.Reuse(ctx, "x").SavingsUSD; got != 0.0054 {
		t.Errorf("first snapshot savings = %v, want 0.0054", got)
	}
	// No growth (same log): nothing to report.
	if got := b.Reuse(ctx, "x").SavingsUSD; got != 0 {
		t.Errorf("no-growth delta = %v, want 0", got)
	}
	// Growth 0.0054 → 0.0108 (log now has two L3 events): only the delta.
	b.cfg.ProjectDir = dir2
	if got := b.Reuse(ctx, "x").SavingsUSD; got != 0.0054 {
		t.Errorf("growth delta = %v, want 0.0054", got)
	}
	// A fresh bridge starts from zero again (new project dir).
	b2 := NewBridge(Config{Enabled: true, ProjectDir: dir2})
	if got := b2.Reuse(ctx, "x").SavingsUSD; got != 0.0108 {
		t.Errorf("fresh bridge savings = %v, want 0.0108", got)
	}
}

// TestBridgeReuseFailsSoft: kernel disabled or store missing degrades to a
// zero summary — the panel hides and the agent never blocks on the kernel.
func TestBridgeReuseFailsSoft(t *testing.T) {
	// Disabled bridge.
	b := NewBridge(Config{Enabled: false})
	if sum := b.Reuse(context.Background(), "x"); sum.Hits != 0 || sum.SavingsUSD != 0 || len(sum.Sources) != 0 {
		t.Errorf("disabled bridge summary = %+v, want zero", sum)
	}
	// Missing kernel store (empty project dir).
	b2 := NewBridge(Config{Enabled: true, ProjectDir: t.TempDir()})
	if sum := b2.Reuse(context.Background(), "x"); sum.Hits != 0 || sum.SavingsUSD != 0 {
		t.Errorf("missing store summary = %+v, want zero", sum)
	}
	// Empty query: no kernel reads at all.
	dir := writeKernelDir(t, reuseFixtureSlices(), []string{usageL3Event})
	b3 := NewBridge(Config{Enabled: true, ProjectDir: dir})
	if sum := b3.Reuse(context.Background(), ""); sum.Hits != 0 {
		t.Errorf("empty query summary = %+v, want zero", sum)
	}
}

// TestBridgeReuseEmptySources: legacy slices without source_session leave
// the provenance empty (panel drops the 🗂 segment) while hits still count.
func TestBridgeReuseEmptySources(t *testing.T) {
	slicesIn := []*slice.Slice{
		{ID: "a", Type: slice.Prompt, Scope: slice.Project, Content: []byte("修复 go 测试")},
		{ID: "b", Type: slice.Result, Scope: slice.Project, Content: []byte("go 测试排查")},
	}
	dir := writeKernelDir(t, slicesIn, nil)
	b := NewBridge(Config{Enabled: true, ProjectDir: dir})
	sum := b.Reuse(context.Background(), "修复 go 测试")
	if sum.Hits != 2 {
		t.Errorf("Hits = %d, want 2", sum.Hits)
	}
	if len(sum.Sources) != 0 {
		t.Errorf("Sources = %v, want empty (legacy slices)", sum.Sources)
	}
}

// TestBridgeInjectDegradedShrinksBlock (Issue #270 step 2): the
// degrade_inject tier halves the block budget. The degraded block must
// never exceed the full-budget one (whole-slice truncation is monotone
// in budget), so a tight window still gets reuse context without the
// full injection cost.
func TestBridgeInjectDegradedShrinksBlock(t *testing.T) {
	dir := writeKernelDir(t, admissionFixtureSlices(), nil)
	b := NewBridge(Config{Enabled: true, Inject: true, ProjectDir: dir, Budget: 4096})
	defer b.Close()
	full := b.InjectDetailed(context.Background(), "修复 go 测试")
	degraded := b.InjectDegraded(context.Background(), "修复 go 测试")
	if full.Text == "" {
		t.Fatal("full injection unexpectedly empty")
	}
	if len(degraded) > len(full.Text) {
		t.Fatalf("degraded block (%d bytes) exceeds full block (%d bytes)", len(degraded), len(full.Text))
	}
}
