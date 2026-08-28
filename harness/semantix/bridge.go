// Package semantix bridges the semantix harness to the semantix kernel:
// session events are mirrored to kernel-compatible session JSONL, and kernel
// retrieval (lookup/inject) is exposed to the agent via subprocess calls.
package semantix

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"semantix/harness/event"
	"semantix/kernel/bm25"
	kernelevent "semantix/kernel/event"
	"semantix/kernel/evolve"
	"semantix/kernel/inject"
	"semantix/kernel/slice"
	"semantix/kernel/usage"
	"semantix/kernel/zone"
)

// Config is the kernel wiring configuration for one build. It mirrors
// config.SemantixConfig without importing internal/config (keeps this
// package a leaf dependency).
type Config struct {
	// Enabled mirrors session events to the kernel's session JSONL sink.
	Enabled bool
	// Binary is the kernel CLI path; empty defaults to "semantix" on PATH.
	// Retained for the legacy semantix_lookup tool; the reuse panel and
	// injection read the kernel in-process (U39) and never spawn the CLI.
	Binary string
	// Inject appends the [semantix-reuse] block to the system prompt region.
	Inject bool
	// Budget caps the L2 injection block size in bytes (default 4096).
	Budget int
	// SessionsDir is where the session JSONL mirror is written; empty uses
	// <controller session dir>/sessions.
	SessionsDir string
	// ProjectDir is the kernel project directory the slice store and usage
	// log resolve against (kernel CLI semantics: <dir>/.semantix/...).
	// Empty uses the process working directory.
	ProjectDir string
	// CostMissUSD / CostHitUSD are the usage cost model prices (USD per 1M
	// tokens at cache miss / hit) for the reuse panel savings delta.
	// Zero keeps the kernel defaults (usage.DefaultCost*PerMTok).
	CostMissUSD float64
	CostHitUSD  float64
}

// Bridge aggregates the kernel wiring for one harness build. It is optional:
// a nil Bridge (or one built with Enabled=false) makes the harness run
// without the kernel — every failure path degrades fail-open, never blocking
// the agent main loop.
type Bridge struct {
	cfg    Config
	events *kernelevent.SyncBus

	mu    sync.Mutex
	hs    *HarnessSink // lazily created once a session label is known
	dir   string       // resolved sessions dir ("" = not yet)
	label string       // controller session label (first real session id)
	// lastSavings is the last observed cumulative usage savings, used to
	// attribute the incremental per-turn delta in Reuse.
	lastSavings float64
	evolution   *EvolutionLoop
	statsWG     sync.WaitGroup
	closing     bool
}

// NewBridge builds a Bridge from cfg.
func NewBridge(cfg Config) *Bridge {
	if cfg.Budget <= 0 {
		cfg.Budget = 4096
	}
	bus := kernelevent.NewSyncBus()
	b := &Bridge{cfg: cfg, events: bus}
	bus.Subscribe(b.mirrorKernel)
	b.evolution = NewEvolutionLoop(bus, evolve.New(evolve.Config{}))
	return b
}

// AttachEvolution connects the live scheduler and prefetcher to the online loop.
func (b *Bridge) AttachEvolution(scheduler, prefetcher EvolutionTuner) {
	if b != nil && b.evolution != nil {
		b.evolution.Attach(scheduler, prefetcher)
	}
}

// InjectResult carries the stable block and the canonical slice identities it
// represents, allowing prefetch feedback to retain the existing targets wire.
type InjectResult struct {
	Text    string
	Targets []string
}

// Events is the in-process kernel event bus shared by the harness and kernel
// services. ResourceCatalog uses it even when the legacy session mirror is off.
func (b *Bridge) Events() kernelevent.Bus {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.events == nil {
		b.events = kernelevent.NewSyncBus()
	}
	return b.events
}

// Enabled reports whether the kernel is wired in.
func (b *Bridge) Enabled() bool { return b != nil && b.cfg.Enabled }

// InjectEnabled reports whether L2 injection is wired on (used to decide
// whether speculative prefetch warm-up is worth starting).
func (b *Bridge) InjectEnabled() bool { return b != nil && b.cfg.Enabled && b.cfg.Inject }

// Sink wraps inner so every event is also mirrored into the kernel session
// JSONL. Returns inner unchanged when the kernel is not enabled (zero-cost
// no-op on the hot path).
func (b *Bridge) Sink(inner event.Sink) event.Sink {
	if !b.Enabled() {
		return inner
	}
	return &mirrorSink{bridge: b, inner: inner}
}

// SetLabel records the controller's session label; the JSONL mirror file is
// created on the first event after this is set. The first label wins: a
// controller keeps one session id per build.
func (b *Bridge) SetLabel(label string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.label == "" && label != "" {
		b.label = label
	}
}

// Inject runs the kernel's L2 injector in-process for query and returns the
// [semantix-reuse] block, or "" when the kernel store is unavailable (soft
// degrade — the harness never blocks on the kernel). Semantics match the
// kernel CLI `semantix inject` defaults (U39 in-process data source).
func (b *Bridge) Inject(ctx context.Context, query string) string {
	return b.inject(ctx, query, b.cfg.Budget)
}

// InjectDegraded builds a halved-budget injection block (Issue #270 step
// 2): the harness calls this when the window budget crosses the
// degrade_inject tier — shrink the injection instead of dropping it.
func (b *Bridge) InjectDegraded(ctx context.Context, query string) string {
	budget := b.cfg.Budget / 2
	if budget <= 0 {
		budget = 1 // never fall back to the full DefaultBudget via the <=0 path
	}
	return b.inject(ctx, query, budget)
}

// inject is the shared injection path with an explicit block budget.
func (b *Bridge) inject(ctx context.Context, query string, budget int) string {
	return b.injectResult(ctx, query, budget).Text
}

func (b *Bridge) InjectDetailed(ctx context.Context, query string) InjectResult {
	return b.injectResult(ctx, query, b.cfg.Budget)
}

func (b *Bridge) injectResult(ctx context.Context, query string, budget int) InjectResult {
	if !b.Enabled() || !b.cfg.Inject {
		return InjectResult{}
	}
	store, idx, err := b.kernelIndex()
	if err != nil {
		return InjectResult{}
	}
	closeSliceStore(store)
	z := zone.Default()
	inj, err := (&inject.Injector{
		Index:  idx,
		Scope:  slice.Project,
		K:      5,
		Budget: budget,
		Zones:  &z,
	}).Build(query)
	if err != nil || inj == nil || len(inj.Slices) == 0 {
		return InjectResult{}
	}
	targets := make([]string, 0, len(inj.Slices))
	for _, sl := range inj.Slices {
		if sl != nil {
			targets = append(targets, sl.ID)
		}
	}
	sort.Strings(targets)
	b.recordInjection(targets, inj.Bytes)
	return InjectResult{Text: inj.Text, Targets: targets}
}

func (b *Bridge) recordInjection(ids []string, bytes int) {
	if len(ids) == 0 {
		return
	}
	ids = append([]string(nil), ids...)
	now := time.Now().UTC()
	projectDB := filepath.Join(b.projectDir(), ".semantix", "project.db")
	b.mu.Lock()
	if b.closing {
		b.mu.Unlock()
		return
	}
	b.statsWG.Add(1)
	session := b.label
	b.mu.Unlock()

	data, err := json.Marshal(kernelevent.SliceInjectPayload{SliceIDs: ids, Bytes: bytes})
	if err == nil {
		b.events.Emit(kernelevent.Event{Kind: kernelevent.SliceInject, SessionID: session, At: now, Data: data})
	}
	go func() {
		defer b.statsWG.Done()
		store, err := slice.NewFileStore(projectDB)
		if err != nil {
			return
		}
		defer closeSliceStore(store)
		deltas := make(map[string]slice.SliceStats, len(ids))
		for _, id := range ids {
			deltas[id] = slice.SliceStats{Injected: 1, LastUsed: now.Unix()}
		}
		_ = slice.ApplyStats(store, deltas)
	}()
}

// RecordPrefetch emits one terminal outcome for a warmed result. lead is
// the time between warm-up completion and the outcome decision: positive
// for hits (completed before consumption — Markov timeliness, Issue #272);
// for wastes it carries the survival time, not a consumption lead.
func (b *Bridge) RecordPrefetch(hit bool, targets, probeTargets []string, turn int, lead time.Duration) {
	if b == nil || !b.Enabled() || len(targets) == 0 {
		return
	}
	targets = append([]string(nil), targets...)
	sort.Strings(targets)
	probeTargets = canonicalPrefetchTargets(probeTargets)
	kind := kernelevent.PrefetchWaste
	leadMs := int64(lead / time.Millisecond)
	var payload any = kernelevent.PrefetchWastePayload{Targets: targets, ProbeTargets: probeTargets, LeadMs: leadMs}
	if hit {
		kind = kernelevent.PrefetchHit
		payload = kernelevent.PrefetchHitPayload{Targets: targets, ProbeTargets: probeTargets, LeadMs: leadMs}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	b.mu.Lock()
	session := b.label
	b.mu.Unlock()
	b.events.Emit(kernelevent.Event{Kind: kind, SessionID: session, Turn: turn, At: time.Now().UTC(), Data: data})
}

func canonicalPrefetchTargets(targets []string) []string {
	if len(targets) == 0 {
		return nil
	}
	targets = append([]string(nil), targets...)
	sort.Strings(targets)
	out := targets[:0]
	for _, target := range targets {
		if target != "" && (len(out) == 0 || out[len(out)-1] != target) {
			out = append(out, target)
		}
	}
	return out
}

// Reuse gathers the per-turn reuse panel data (U33/H4a) in-process: the
// project-store hits for query (kernel/lookup semantics, limit 5) plus the
// incremental cost savings since the last usage snapshot. Kernel store
// unavailable degrades to a zero summary — the panel hides and the agent
// main loop never blocks on the kernel.
func (b *Bridge) Reuse(ctx context.Context, query string) ReuseSummary {
	if !b.Enabled() || query == "" {
		return ReuseSummary{}
	}
	store, idx, err := b.kernelIndex()
	if err != nil {
		return ReuseSummary{}
	}
	defer closeSliceStore(store)
	hits, err := idx.Search(query, 5, slice.Project)
	if err != nil {
		return ReuseSummary{}
	}
	sessions := make([]string, 0, len(hits))
	for _, h := range hits {
		if h.Slice != nil {
			sessions = append(sessions, h.Slice.Meta.SourceSession)
		}
	}
	sum := ReuseSummary{Hits: len(hits), Sources: topSources(sessions)}
	if s, err := usage.Summarize(b.usagePath(), b.costMiss(), b.costHit()); err == nil {
		b.mu.Lock()
		prev := b.lastSavings
		b.lastSavings = s.SavingsUSD
		b.mu.Unlock()
		if delta := s.SavingsUSD - prev; delta > 0 {
			sum.SavingsUSD = delta
		}
	}
	return sum
}

// kernelIndex opens the project slice store and rebuilds the in-memory index
// covering every scope (kernel CLI lookup/inject parity). Rebuilt per call:
// the store is a small JSONL file and indexing is millisecond-scale; caching
// is deferred to the kernel wiring follow-up (U40).
func (b *Bridge) kernelIndex() (slice.Store, slice.Index, error) {
	store, err := slice.NewFileStore(filepath.Join(b.projectDir(), ".semantix", "project.db"))
	if err != nil {
		return nil, nil, err
	}
	idx := bm25.New()
	for _, scope := range []slice.Scope{slice.Session, slice.Project, slice.User} {
		items, err := store.List(scope)
		if err != nil {
			closeSliceStore(store)
			return nil, nil, err
		}
		for _, sl := range items {
			if err := idx.Insert(sl); err != nil {
				closeSliceStore(store)
				return nil, nil, err
			}
		}
	}
	return store, idx, nil
}

func closeSliceStore(store slice.Store) {
	if closer, ok := store.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

// projectDir resolves the kernel project directory for in-process store and
// usage-log reads (kernel CLI semantics: <dir>/.semantix/...).
func (b *Bridge) projectDir() string {
	if b.cfg.ProjectDir != "" {
		return b.cfg.ProjectDir
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

// usagePath is the kernel usage log the reuse panel savings delta reads.
func (b *Bridge) usagePath() string {
	return filepath.Join(b.projectDir(), ".semantix", "usage.jsonl")
}

// costMiss / costHit resolve the usage cost model prices, falling back to the
// kernel defaults when the build did not configure them.
func (b *Bridge) costMiss() float64 {
	if b.cfg.CostMissUSD > 0 {
		return b.cfg.CostMissUSD
	}
	return usage.DefaultCostMissPerMTok
}

func (b *Bridge) costHit() float64 {
	if b.cfg.CostHitUSD > 0 {
		return b.cfg.CostHitUSD
	}
	return usage.DefaultCostHitPerMTok
}

// mirrorSink forwards every event to inner and mirrors the session-relevant
// subset into the kernel JSONL via the bridge's HarnessSink.
type mirrorSink struct {
	bridge *Bridge
	inner  event.Sink
}

func (s *mirrorSink) Emit(e event.Event) {
	s.inner.Emit(e)
	s.bridge.mirror(e)
}

// mirror lazily creates the HarnessSink on the first session event after a
// label is known, then forwards. Failures are non-fatal: a write error is
// surfaced once via the inner sink and never blocks emission.
func (b *Bridge) mirror(e event.Event) {
	hs := b.sessionSink()
	if hs == nil {
		return
	}
	hs.Emit(e)
}

func (b *Bridge) mirrorKernel(e kernelevent.Event) {
	if e.Kind != kernelevent.SliceHit && e.Kind != kernelevent.SliceInject &&
		e.Kind != kernelevent.PrefetchHit && e.Kind != kernelevent.PrefetchWaste && e.Kind != kernelevent.EvolutionTick {
		return
	}
	hs := b.sessionSink()
	if hs != nil {
		hs.EmitKernel(e)
	}
}

func (b *Bridge) sessionSink() *HarnessSink {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.hs != nil {
		return b.hs
	}
	if b.label == "" {
		return nil
	}
	hs, err := NewHarnessSink(dirOrFallback(b.cfg.SessionsDir), b.label, "")
	if err != nil {
		return nil
	}
	b.hs = hs
	return hs
}

// EndTurn flushes and closes the mirror's open turn. The agent calls this on
// every Run return path because synchronous runs never emit TurnDone and a
// headless process can exit without running Close.
func (b *Bridge) EndTurn() {
	if b == nil || !b.Enabled() {
		return
	}
	if hs := b.sessionSink(); hs != nil {
		hs.EndTurn()
	}
}

// Close flushes and closes the mirror sink, if created.
func (b *Bridge) Close() error {
	b.mu.Lock()
	b.closing = true
	b.mu.Unlock()
	b.statsWG.Wait()
	if b.evolution != nil {
		b.evolution.Close()
	}
	b.mu.Lock()
	hs := b.hs
	b.hs = nil
	b.mu.Unlock()
	if hs == nil {
		return nil
	}
	return hs.Close()
}

// dirOrFallback returns dir, falling back to a per-build default when empty.
// (The harness resolves <session dir>/sessions before SetLabel and passes it
// through SessionsDir; the fallback keeps the bridge self-contained.)
func dirOrFallback(dir string) string {
	if dir != "" {
		return dir
	}
	return ".semantix/sessions"
}

// itoa is a tiny integer formatter avoiding strconv in a hot path.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
