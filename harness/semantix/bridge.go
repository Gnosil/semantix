// Package semantix bridges the semantix harness to the semantix kernel:
// session events are mirrored to kernel-compatible session JSONL, and kernel
// retrieval (lookup/inject) is exposed to the agent via subprocess calls.
package semantix

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	// Inject adds the [semantix-reuse] block as untrusted user-role history.
	Inject bool
	// Mode controls L2 retrieval: off | shadow | strict. Empty preserves the
	// legacy Inject boolean; an explicit value takes precedence.
	Mode string
	// Budget caps the L2 injection block size in bytes (default 4096).
	Budget int
	// GreyMode controls the grey-zone injection policy: "" / "drop" keeps
	// the fail-closed default (only zone.Hit slices injected); "audit"
	// admits grey slices under a separate unverified marker so grey-zone
	// hit-rate loss is measurable instead of silent (W3 of the efficiency
	// plan; GW4 measured 8/10 repeated tasks landing in grey).
	GreyMode string
	// SessionsDir is where the session JSONL mirror is written; empty uses
	// <controller session dir>/sessions.
	SessionsDir string
	// InjectAuditPath optionally receives each provider-visible L2 block in
	// full. This is opt-in because the file contains retrieved task context.
	InjectAuditPath string
	// ProjectDir is the kernel project directory the slice store and usage
	// log resolve against (kernel CLI semantics: <dir>/.semantix/...).
	// Empty uses the process working directory.
	ProjectDir string
	// WorkspaceDir is the live repository used for commit/dependency checks.
	// Empty falls back to ProjectDir for normal non-benchmark runs.
	WorkspaceDir string
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
	mode   RetrievalMode
	events *kernelevent.SyncBus

	mu    sync.Mutex
	hs    *HarnessSink // lazily created once a session label is known
	sink  event.Sink   // live harness sink for frontend cache observations
	dir   string       // resolved sessions dir ("" = not yet)
	label string       // controller session label (first real session id)
	// lastSavings is the last observed cumulative usage savings, used to
	// attribute the incremental per-turn delta in Reuse.
	lastSavings float64
	evolution   *EvolutionLoop
	statsWG     sync.WaitGroup
	auditMu     sync.Mutex
	closing     bool
}

// RetrievalMode controls whether L2 retrieval is disabled, observed only, or
// allowed to contribute provider-visible untrusted history.
type RetrievalMode string

const (
	RetrievalOff    RetrievalMode = "off"
	RetrievalShadow RetrievalMode = "shadow"
	RetrievalStrict RetrievalMode = "strict"

	strictMinLibrarySize    = 5
	strictMinSourceSessions = 2
	strictMinScore          = 0.70
	strictMinCoverage       = 0.25
	strictMinTopMargin      = 0.15
)

var strictAllowedTypes = map[slice.SliceType]bool{
	slice.Context: true,
	slice.Memory:  true,
	slice.Result:  true,
}

// NewBridge builds a Bridge from cfg.
func NewBridge(cfg Config) *Bridge {
	if cfg.Budget <= 0 {
		cfg.Budget = 4096
	}
	bus := kernelevent.NewSyncBus()
	b := &Bridge{cfg: cfg, mode: resolveRetrievalMode(cfg), events: bus}
	bus.Subscribe(b.mirrorKernel)
	b.evolution = NewEvolutionLoop(bus, evolve.New(evolve.Config{}))
	return b
}

func resolveRetrievalMode(cfg Config) RetrievalMode {
	if !cfg.Enabled {
		return RetrievalOff
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Mode)) {
	case "":
		if cfg.Inject {
			return RetrievalStrict
		}
		return RetrievalOff
	case string(RetrievalOff):
		return RetrievalOff
	case string(RetrievalShadow):
		return RetrievalShadow
	case string(RetrievalStrict):
		return RetrievalStrict
	default:
		return RetrievalOff
	}
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
	Text        string
	Targets     []string
	Diagnostics *event.RetrievalDiagnostics
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

// RetrievalMode reports the fail-closed effective L2 mode.
func (b *Bridge) RetrievalMode() RetrievalMode {
	if b == nil {
		return RetrievalOff
	}
	return b.mode
}

// InjectEnabled reports whether L2 injection is wired on (used to decide
// whether speculative prefetch warm-up is worth starting).
func (b *Bridge) InjectEnabled() bool {
	return b != nil && b.Enabled() && b.mode == RetrievalStrict
}

// Sink wraps inner so every event is also mirrored into the kernel session
// JSONL. Returns inner unchanged when the kernel is not enabled (zero-cost
// no-op on the hot path).
func (b *Bridge) Sink(inner event.Sink) event.Sink {
	if !b.Enabled() {
		return inner
	}
	b.mu.Lock()
	b.sink = inner
	b.mu.Unlock()
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
	return b.InjectDegradedDetailed(ctx, query).Text
}

// InjectDegradedDetailed is the target-preserving form used by the agent so a
// later negative-transfer fuse can attribute the reduced block to its slices.
func (b *Bridge) InjectDegradedDetailed(ctx context.Context, query string) InjectResult {
	budget := b.cfg.Budget / 2
	if budget <= 0 {
		budget = 1 // never fall back to the full DefaultBudget via the <=0 path
	}
	return b.injectResult(ctx, query, budget)
}

// inject is the shared injection path with an explicit block budget.
func (b *Bridge) inject(ctx context.Context, query string, budget int) string {
	return b.injectResult(ctx, query, budget).Text
}

func (b *Bridge) InjectDetailed(ctx context.Context, query string) InjectResult {
	return b.injectResult(ctx, query, b.cfg.Budget)
}

func (b *Bridge) injectResult(ctx context.Context, query string, budget int) InjectResult {
	if !b.Enabled() || b.mode == RetrievalOff {
		return InjectResult{}
	}
	store, idx, err := b.kernelIndex()
	if err != nil {
		b.emitKernelCache("miss", "L2", nil, 0, "slice store unavailable")
		return InjectResult{}
	}
	defer closeSliceStore(store)
	projectSlices, err := store.List(slice.Project)
	if err != nil {
		b.emitKernelCache("miss", "L2", nil, 0, "slice library unavailable")
		return InjectResult{}
	}
	retrievalQuery := buildRetrievalQuery(query)
	cleanedQuery := retrievalQuery.Text
	if cleanedQuery == "" {
		diagnostics := b.retrievalDiagnostics(query, retrievalQuery, projectSlices, nil, nil)
		diagnostics.Decision = "rejected"
		diagnostics.DecisionReason = "empty_query_after_cleaning"
		b.emitKernelCacheDetailed("miss", "L2", nil, 0, diagnostics.DecisionReason, diagnostics)
		return InjectResult{Diagnostics: diagnostics}
	}
	hits, err := idx.Search(cleanedQuery, 5, slice.Project)
	if err != nil {
		b.emitKernelCache("miss", "L2", nil, 0, err.Error())
		return InjectResult{}
	}
	z := zone.Default()
	workspaceDir := b.workspaceDir()
	inj, err := (&inject.Injector{
		Index:                idx,
		Scope:                slice.Project,
		K:                    5,
		Budget:               budget,
		AllowedTypes:         strictAllowedTypes,
		RootDir:              workspaceDir,
		CurrentCommit:        readGitHead(workspaceDir),
		LibrarySize:          len(projectSlices),
		MinLibrarySize:       strictMinLibrarySize,
		SourceSessionsByType: sourceSessionCounts(projectSlices),
		MinSourceSessions:    strictMinSourceSessions,
		MinScore:             strictMinScore,
		MinCoverage:          strictMinCoverage,
		MinTopMargin:         strictMinTopMargin,
		RequireRunnerUp:      true,
		Zones:                &z,
		AllowGrey:            b.cfg.GreyMode == "audit",
	}).BuildHits(cleanedQuery, hits)
	if err != nil {
		op := "miss"
		if budget < b.cfg.Budget {
			op = "degraded"
		}
		b.emitKernelCache(op, "L2", nil, 0, err.Error())
		return InjectResult{}
	}
	diagnostics := b.retrievalDiagnostics(query, retrievalQuery, projectSlices, hits, inj)
	if b.mode == RetrievalShadow {
		diagnostics.Decision = "withheld"
		diagnostics.DecisionReason = "shadow_mode"
		b.emitKernelCacheDetailed("shadow", "L2", diagnostics.FinalOrder, 0, "shadow_mode", diagnostics)
		return InjectResult{Targets: append([]string(nil), diagnostics.FinalOrder...), Diagnostics: diagnostics}
	}
	if inj == nil || len(inj.Slices) == 0 {
		op := "miss"
		if budget < b.cfg.Budget {
			op = "degraded"
		}
		diagnostics.Decision = "rejected"
		diagnostics.DecisionReason = "no_admitted_slices"
		b.emitKernelCacheDetailed(op, "L2", nil, 0, "no matching slices", diagnostics)
		return InjectResult{Diagnostics: diagnostics}
	}
	targets := make([]string, 0, len(inj.Slices))
	for _, sl := range inj.Slices {
		if sl != nil {
			targets = append(targets, sl.ID)
		}
	}
	sort.Strings(targets)
	b.auditInjection(inj.Text)
	b.recordInjection(targets, inj.Bytes)
	diagnostics.Injected = true
	diagnostics.Bytes = inj.Bytes
	diagnostics.MessageRole = "user"
	diagnostics.Decision = "injected"
	diagnostics.DecisionReason = "admitted"
	op := "inject"
	if budget < b.cfg.Budget {
		op = "degraded"
	}
	b.emitKernelCacheDetailed(op, "L2", targets, inj.Bytes, "", diagnostics)
	return InjectResult{Text: inj.Text, Targets: targets, Diagnostics: diagnostics}
}

func (b *Bridge) auditInjection(block string) {
	path := strings.TrimSpace(b.cfg.InjectAuditPath)
	if path == "" || block == "" {
		return
	}
	b.auditMu.Lock()
	defer b.auditMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err == nil && info.Size() > 0 {
		_, _ = f.WriteString("\n\n--- semantix injection ---\n\n")
	}
	_, _ = f.WriteString(strings.TrimRight(block, "\n") + "\n")
}

func (b *Bridge) retrievalDiagnostics(query string, retrievalQuery RetrievalQuery, library []*slice.Slice, hits []slice.Hit, inj *inject.Injection) *event.RetrievalDiagnostics {
	projectDir := b.workspaceDir()
	d := &event.RetrievalDiagnostics{
		Mode: string(b.mode), LibrarySize: len(library), Repo: filepath.Base(filepath.Clean(projectDir)),
		BaseCommit: readGitHead(projectDir), QueryBefore: summarizeQuery(query), QueryAfter: summarizeQuery(retrievalQuery.Text),
		QueryStructure: event.RetrievalQueryStructure{
			Strategy: retrievalQuery.Strategy, Intent: retrievalQuery.Intent, Repo: retrievalQuery.Repo,
			Paths: append([]string(nil), retrievalQuery.Paths...), Symbols: append([]string(nil), retrievalQuery.Symbols...),
			ErrorCodes: append([]string(nil), retrievalQuery.ErrorCodes...), TestNames: append([]string(nil), retrievalQuery.TestNames...),
			Dependencies: append([]string(nil), retrievalQuery.Dependencies...), FallbackReason: retrievalQuery.FallbackReason,
		},
	}
	if inj == nil {
		return d
	}
	d.TopMargin = inj.TopMargin
	for i, decision := range inj.Decisions {
		candidate := event.RetrievalCandidate{
			ID: decision.ID, Score: decision.Score, Coverage: decision.Coverage,
			Zone: decision.Zone, Admitted: decision.Admitted, Reason: decision.Reason, Verified: "unknown",
		}
		if i < len(hits) && hits[i].Slice != nil {
			sl := hits[i].Slice
			candidate.Type = sl.Type.String()
			candidate.SourceSession = sl.Meta.SourceSession
			candidate.Project = sl.Meta.ProjectSlug
			candidate.BaseCommit = sl.Meta.BaseCommit
			candidate.Origin = string(sl.Meta.Origin)
			if sl.Type == slice.Result {
				candidate.Verified = string(sl.Meta.EffectiveResultStatus())
			}
		}
		d.Candidates = append(d.Candidates, candidate)
	}
	for _, sl := range inj.Slices {
		if sl != nil {
			d.FinalOrder = append(d.FinalOrder, sl.ID)
		}
	}
	return d
}

func sourceSessionCounts(library []*slice.Slice) map[slice.SliceType]int {
	sets := make(map[slice.SliceType]map[string]struct{})
	for _, sl := range library {
		if sl == nil || sl.Meta.SourceSession == "" {
			continue
		}
		if sets[sl.Type] == nil {
			sets[sl.Type] = make(map[string]struct{})
		}
		sets[sl.Type][sl.Meta.SourceSession] = struct{}{}
	}
	counts := make(map[slice.SliceType]int, len(sets))
	for typ, sessions := range sets {
		counts[typ] = len(sessions)
	}
	return counts
}

func summarizeQuery(query string) event.QuerySummary {
	sum := sha256.Sum256([]byte(query))
	return event.QuerySummary{SHA256: fmt.Sprintf("%x", sum[:]), Bytes: len(query), Tokens: len(bm25.Tokenize(query))}
}

func readGitHead(root string) string {
	gitDir := filepath.Join(root, ".git")
	if raw, err := os.ReadFile(gitDir); err == nil {
		line := strings.TrimSpace(string(raw))
		if strings.HasPrefix(line, "gitdir:") {
			gitDir = strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
			if !filepath.IsAbs(gitDir) {
				gitDir = filepath.Join(root, gitDir)
			}
		}
	}
	head, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return ""
	}
	value := strings.TrimSpace(string(head))
	if !strings.HasPrefix(value, "ref:") {
		return value
	}
	ref := strings.TrimSpace(strings.TrimPrefix(value, "ref:"))
	for _, base := range []string{gitDir, filepath.Clean(filepath.Join(gitDir, "..", ".."))} {
		if raw, err := os.ReadFile(filepath.Join(base, filepath.FromSlash(ref))); err == nil {
			return strings.TrimSpace(string(raw))
		}
	}
	return ""
}

// emitKernelCache forwards an observed kernel cache operation to the live
// harness sink. The kernel bus remains the source of truth for its own JSONL
// mirror; this small projection is only for the workspace SSE/UI and is
// fail-open when no frontend sink is attached.
func (b *Bridge) emitKernelCache(op, layer string, ids []string, bytes int, reason string) {
	b.emitKernelCacheDetailed(op, layer, ids, bytes, reason, nil)
}

func (b *Bridge) emitKernelCacheDetailed(op, layer string, ids []string, bytes int, reason string, retrieval *event.RetrievalDiagnostics) {
	if b == nil || !b.Enabled() {
		return
	}
	ids = append([]string(nil), ids...)
	b.mu.Lock()
	sink := b.sink
	label := b.label
	b.mu.Unlock()
	if sink == nil {
		return
	}
	sink.Emit(event.Event{Kind: event.KernelCache, Text: op, Source: label,
		KernelCache: &event.KernelCachePayload{Op: op, Layer: layer, SliceIDs: ids, Bytes: bytes, Reason: reason, Retrieval: retrieval}})
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

// RecordInjectionReject attributes a conservative negative-transfer signal to
// every injected slice that was active when the harness loop guard fired. IDs
// are canonicalized so one fuse increments each slice exactly once.
func (b *Bridge) RecordInjectionReject(ids []string, reason string) {
	b.recordInjectionOutcome(ids, "harmful", reason, true)
}

// RecordInjectionOutcome persists an evaluator/guard observation for each
// injected slice. Unsupported outcomes are ignored rather than inventing a
// category; callers must supply useful, neutral, or harmful.
func (b *Bridge) RecordInjectionOutcome(ids []string, outcome, reason string) {
	b.recordInjectionOutcome(ids, outcome, reason, false)
}

func (b *Bridge) recordInjectionOutcome(ids []string, outcome, reason string, legacyReject bool) {
	if b == nil || !b.Enabled() || len(ids) == 0 {
		return
	}
	outcome = strings.ToLower(strings.TrimSpace(outcome))
	if outcome != "useful" && outcome != "neutral" && outcome != "harmful" {
		return
	}
	ids = append([]string(nil), ids...)
	sort.Strings(ids)
	unique := ids[:0]
	for _, id := range ids {
		if id != "" && (len(unique) == 0 || unique[len(unique)-1] != id) {
			unique = append(unique, id)
		}
	}
	if len(unique) == 0 {
		return
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "negative_transfer"
	}
	now := time.Now().UTC()
	if store, err := slice.NewFileStore(filepath.Join(b.projectDir(), ".semantix", "project.db")); err == nil {
		deltas := make(map[string]slice.SliceStats, len(unique))
		for _, id := range unique {
			delta := slice.SliceStats{LastUsed: now.Unix()}
			switch outcome {
			case "useful":
				delta.Useful = 1
			case "neutral":
				delta.Neutral = 1
			case "harmful":
				delta.Harmful = 1
				delta.Rejected = 1
			}
			deltas[id] = delta
		}
		_ = slice.ApplyStats(store, deltas)
		closeSliceStore(store)
	}
	b.mu.Lock()
	session := b.label
	b.mu.Unlock()
	for _, id := range unique {
		data, err := json.Marshal(kernelevent.SliceOutcomePayload{SliceID: id, Outcome: outcome, Reason: reason})
		if err == nil {
			b.events.Emit(kernelevent.Event{Kind: kernelevent.SliceOutcome, SessionID: session, At: now, Data: data})
		}
		if legacyReject {
			data, err = json.Marshal(kernelevent.SliceRejectPayload{SliceID: id, Reason: reason})
			if err == nil {
				b.events.Emit(kernelevent.Event{Kind: kernelevent.SliceReject, SessionID: session, At: now, Data: data})
			}
		}
	}
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
	ids := make([]string, 0, len(hits))
	sessions := make([]string, 0, len(hits))
	for _, h := range hits {
		if h.Slice != nil {
			ids = append(ids, h.Slice.ID)
			sessions = append(sessions, h.Slice.Meta.SourceSession)
		}
	}
	if len(ids) > 0 {
		sort.Strings(ids)
		data, marshalErr := json.Marshal(kernelevent.SliceHitPayload{Layer: "L2", SliceIDs: ids})
		if marshalErr == nil {
			b.mu.Lock()
			session := b.label
			b.mu.Unlock()
			b.events.Emit(kernelevent.Event{Kind: kernelevent.SliceHit, SessionID: session, At: time.Now().UTC(), Data: data})
		}
		b.emitKernelCache("hit", "L2", ids, 0, "")
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

func (b *Bridge) workspaceDir() string {
	if strings.TrimSpace(b.cfg.WorkspaceDir) != "" {
		return b.cfg.WorkspaceDir
	}
	return b.projectDir()
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
	b.sink = nil
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
