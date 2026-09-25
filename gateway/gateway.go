package gateway

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"semantix/kernel/adapt"
	"semantix/kernel/cache"
	kernelevent "semantix/kernel/event"
	"semantix/kernel/ingest"
	"semantix/kernel/inject"
	"semantix/kernel/judge"
	"semantix/kernel/promote"
	"semantix/kernel/slice"
	"semantix/kernel/usage"
	"semantix/kernel/zone"
)

// Gateway is the assembled Semantix Gateway v1. All semantic behavior is
// delegated to kernel packages (cache / inject / slice / ingest / usage);
// this type only wires them together and owns the HTTP transport.
type Gateway struct {
	cfg      *Config
	store    slice.Store
	index    slice.Index
	decider  *cache.L3Decider
	injector *inject.Injector
	usageLog *usage.Recorder
	quota    *quotaEngine // nil unless [billing] enabled (gateway/quota.go)
	client   *http.Client

	// healthProbe checks upstream reachability for /healthz (nil or a
	// nil-config timeout disables probing). Injectable so tests can stub
	// or slow it down without a real upstream.
	healthProbe func(ctx context.Context) error

	sessionsMu sync.Mutex // serializes sidecar JSONL appends
	ingestMu   sync.Mutex // serializes the async ingest writes
	ingestWG   sync.WaitGroup
	shutdownMu sync.Mutex // guards closing flag vs recordSession's Add
	closing    bool
	disabled   bool // SEMANTIX_GATEWAY_DISABLE ablation switch

	// Suspected-false-hit tracking (Issue #262 §3.3): the most recent L3
	// reuse per session, bounded, so a same-session retry of a served
	// query bypasses L3 and is recorded as L3FalseHit.
	reuseMu  sync.Mutex
	l3Reuses map[string]l3ReuseEntry

	// Per-entry adaptive thresholds (Issue #259 阶段 3): the engine the
	// decider consults and the feedback hooks feed; nil when adaptation
	// is disabled by config. zones is the effective classifier snapshot
	// (explicit config > evolve > defaults) used as the cold-start prior.
	adapt *adapt.Engine
	zones zone.Zones

	now func() time.Time

	// lexicalBlocks counts zone-Hit candidates downgraded by the lexical
	// support gate (Issue #260), for hit-rate-loss accounting.
	lexicalBlocks atomic.Int64

	// retrievalEvents appends the opt-in L2 retrieval trace ([retrieval]
	// events_log). Zero value is ready; the file opens lazily on first use.
	retrievalEvents retrievalEventLog
}

// l3ReuseEntry records one L3-served request per session (Issue #262).
type l3ReuseEntry struct {
	Query   string // the served query, for retry similarity
	SliceID string
	At      time.Time // LRU eviction timestamp
}

// maxL3ReuseEntries bounds the per-session reuse map (LRU eviction).
const maxL3ReuseEntries = 1024

// disableEnv reports the ablation switch SEMANTIX_GATEWAY_DISABLE. Only
// truthy values disable ("1", "true", "yes", "on") — "0"/"false" must keep
// the cache enabled, otherwise the escape hatch silently breaks caching.
func disableEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SEMANTIX_GATEWAY_DISABLE"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// New assembles the gateway from config: opens the slice store, rebuilds the
// in-memory retrieval index (bm25 | vector | hybrid, Issue #186) from it,
// wires the grey-zone LLM judge (spec §3.5, optional), and wires the L2
// injector + L3 decider. It never starts network listeners — the caller
// (cmd/semantix-gateway) does.
func New(cfg *Config) (*Gateway, error) {
	root := cfg.Store.DepsRoot
	if root == "" {
		root = "."
	}
	store, err := slice.NewFileStore(cfg.Store.DB)
	if err != nil {
		return nil, fmt.Errorf("gateway: open store %s: %w", cfg.Store.DB, err)
	}
	// Startup is a process boundary: run the scoring + eviction pass and
	// fold the journal before serving (freeze-window semantics — the
	// library never shifts mid-flight; over-cap growth during a run is
	// tolerated and converges at the next boot/gc). Best-effort: a failed
	// pass still leaves a consistent store.
	gcRes, gcErr := slice.GC(store, slice.GCOptions{
		Rescore:     true,
		MaxSlices:   cfg.Store.EffectiveMaxSlices(),
		Params:      slice.DefaultScoreParams(),
		ArchivePath: cfg.Store.DB + ".archive.jsonl",
	})
	if gcErr != nil {
		log.Printf("gateway: store maintenance: %v", gcErr)
	} else if gcRes.Removed > 0 || gcRes.RescoredWeights > 0 {
		log.Printf("gateway: store maintenance: rescored=%d evicted=%d archived=%d capacity=%d",
			gcRes.RescoredWeights, gcRes.Removed, gcRes.Archived, gcRes.Capacity)
	}
	var idx slice.Index = newRetriever(cfg.Retrieval.Retriever, cfg.Retrieval.VectorDim, cfg.fusionConfig(), EmbedSettings{
		Backend: cfg.Retrieval.EmbedBackend,
		BaseURL: cfg.Retrieval.EmbedBaseURL,
		Model:   cfg.Retrieval.EmbedModel,
	})
	if err := loadIndex(store, idx); err != nil {
		_ = closeStore(store)
		return nil, fmt.Errorf("gateway: rebuild index: %w", err)
	}
	if cfg.Retrieval.RerankBaseURL != "" {
		// Local reranker decorator (spec §3 B): wraps after the index is
		// loaded so InsertBatch during the rebuild keeps its batch path.
		idx = newRerankIndex(idx, rerankSettings{
			BaseURL:   cfg.Retrieval.RerankBaseURL,
			TopN:      cfg.Retrieval.RerankTopN,
			TimeoutMs: cfg.Retrieval.RerankTimeoutMs,
		})
	}

	// Grey-zone LLM judge (Issue #186 / GW6, spec §3.5): when a judge key is
	// configured, ambiguous L3 candidates are confirmed by kernel/judge
	// before reuse; without it the decider conservatively rejects grey.
	var llmJudge judge.Judge
	if cfg.Cache.JudgeAPIKey != "" {
		protocol := cfg.Cache.JudgeProtocol
		if protocol == "" {
			protocol = "openai"
		}
		j, err := judge.NewLLMJudge(judge.LLMConfig{
			Protocol: protocol,
			BaseURL:  cfg.Cache.JudgeBaseURL,
			Model:    cfg.Cache.JudgeModel,
			APIKey:   cfg.Cache.JudgeAPIKey,
			Timeout:  time.Duration(cfg.Cache.JudgeTimeoutMs) * time.Millisecond,
		})
		if err != nil {
			_ = closeStore(store)
			return nil, fmt.Errorf("gateway: judge: %w", err)
		}
		// Verdict cache + background warm (W3): the judge sits on the
		// synchronous L3 path, so every grey candidate would otherwise pay
		// a full model round-trip per request. The cache serves repeat
		// verdicts instantly; a timed-out call is re-run in the background
		// so the NEXT occurrence of the same candidate is instant instead
		// of the request failing closed every time.
		llmJudge = &judge.CachedJudge{Inner: j, Warm: true}
	}

	scope, err := parseScope(cfg.Store.Scope)
	if err != nil {
		_ = closeStore(store)
		return nil, err
	}
	topK := cfg.Retrieval.TopK
	if topK <= 0 {
		topK = 5
	}
	budget := cfg.Retrieval.Budget
	if budget <= 0 {
		budget = inject.DefaultBudget
	}
	// Grey-zone thresholds (Issue #259 阶段 1): explicit [retrieval]
	// tau_*/abs_* keys, else the evolve-tuned TauL2 (evolve_db), else the
	// tuned defaults. zoneConfig is validated by Load, so a non-nil error
	// here means the evolve state file itself is unreadable/corrupt.
	z, err := cfg.zoneConfig()
	if err != nil {
		_ = closeStore(store)
		return nil, err
	}

	var rec *usage.Recorder
	if cfg.Ingest.UsageLog != "" {
		rec, err = usage.NewRecorder(cfg.Ingest.UsageLog)
		if err != nil {
			_ = closeStore(store)
			return nil, fmt.Errorf("gateway: usage recorder: %w", err)
		}
	}

	decider := &cache.L3Decider{Index: idx, Store: store, Root: root, K: topK, Judge: llmJudge, MinOrigin: cfg.minInjectOrigin()}
	// Issue #260: lexical support floor for the L3 Hit path (0 disables the
	// gate; nil keeps the kernel default).
	if cfg.Cache.LexicalFloor != nil {
		decider.LexicalFloor = cfg.Cache.LexicalFloor
	}
	g := &Gateway{
		cfg:     cfg,
		store:   store,
		index:   idx,
		decider: decider,
		injector: &inject.Injector{Index: idx, Store: store, Scope: scope, K: topK, Budget: budget, Zones: &z,
			MinOrigin: cfg.minInjectOrigin(),
			// W3 grey audit mode: grey slices enter the block under an
			// unverified marker instead of being silently dropped (GW4:
			// 8/10 repeated tasks landed in grey under the default drop
			// policy).
			AllowGrey: cfg.Retrieval.GreyMode == "audit",
		},
		usageLog: rec,
		client:   &http.Client{Timeout: 120 * time.Second},
		disabled: disableEnv(),
		l3Reuses: make(map[string]l3ReuseEntry),
		now:      time.Now,
		zones:    z,
	}
	// Per-entry adaptive TauLow (Issue #259 阶段 3, vCache route): each
	// high-frequency reused slice learns its own grey floor from the
	// gateway's positive/negative evidence. The engine is nil when
	// disabled by config (adaptive = false or error_bound = -1); the
	// decider's nil-safe Adapt hook then keeps every entry on the
	// global/per-type thresholds (cold start). Adaptive state is derived
	// and rebuildable — a corrupt file only logs, never blocks startup.
	if cfg.Retrieval.Adaptive == nil || *cfg.Retrieval.Adaptive {
		adaptCfg := adapt.Config{}
		if cfg.Retrieval.ErrorBound != 0 {
			adaptCfg.ErrorBound = cfg.Retrieval.ErrorBound
		}
		adaptPath := cfg.Retrieval.AdaptDB
		if adaptPath == "" {
			adaptPath = adapt.DefaultAdaptDB(cfg.Store.DB)
		}
		g.adapt = adapt.New(adaptCfg, adaptPath)
		decider.Adapt = g.adapt
	}

	// Promotion wiring (Issue #280): when promote_db is configured, the
	// grey path gets the consensus-gated promotion decision — approved
	// (query, slice, version) pairs skip the judge inside the TTL window,
	// repeated rejections blacklist the pair. The rejection lessons live
	// in an independent file next to the promotion entries (never
	// injected/indexed). Consensus=2 wraps the LLM judge in the
	// dual-rubric gate; consensus=1 keeps the single-judgement baseline.
	if cfg.Cache.PromoteDB != "" {
		ttl := cfg.Cache.PromoteTTLSeconds
		if ttl == 0 {
			ttl = DefaultPromoteTTLSeconds
		}
		limit := int64(cfg.Cache.RejectLimit)
		if limit == 0 {
			limit = DefaultRejectLimit
		}
		entries, err := promote.NewFileStore(cfg.Cache.PromoteDB)
		if err != nil {
			_ = closeStore(store)
			return nil, fmt.Errorf("gateway: promote store: %w", err)
		}
		rejections, err := promote.NewRejectionFileStore(filepath.Join(filepath.Dir(cfg.Cache.PromoteDB), "rejections.jsonl"))
		if err != nil {
			_ = closeStore(store)
			return nil, fmt.Errorf("gateway: rejection store: %w", err)
		}
		decider.Promote = promote.NewDecision(entries, rejections, ttl, limit)
		consensus := cfg.Cache.PromoteConsensus
		if consensus == 0 {
			consensus = DefaultPromoteConsensus
		}
		if consensus == 2 {
			// The consensus second perspective comes from the judge
			// itself (LLMJudge is a VariantJudge): the primary judge
			// approves first, then the rephrased rubric confirms —
			// 2 calls per promotion, never more (Issue #280).
			decider.Consensus = llmJudge
		}
	}
	// Grey-zone judge decisions become durable here (Issue #242 gap 1):
	// one structured log line plus a field on the turn's usage event, so a
	// non-hit can be explained and the judge's own model call can be costed.
	g.decider.OnJudge = g.observeJudge
	g.decider.OnLexicalGate = g.observeLexicalGate
	g.healthProbe = g.probeUpstreams
	if gcErr == nil && gcRes.Removed > 0 {
		// Type-aware eviction observation (Issue #277): a library-level
		// Compact event makes the startup eviction visible to kernel/event
		// consumers. "maintenance" is a fixed library-scope session id, not
		// a real conversation. Best-effort — a failed write only drops the
		// observation.
		data, merr := json.Marshal(kernelevent.CompactPayload{
			Trigger:       "evict",
			Before:        gcRes.Checked,
			After:         gcRes.Checked - gcRes.Removed,
			EvictedByType: gcRes.EvictedByType,
		})
		if merr == nil {
			g.recordKernelEvent("maintenance", "", "", kernelevent.Event{
				Kind: kernelevent.Compact,
				At:   time.Now(),
				Data: data,
			})
		}
	}

	// Customer free-tier gate (gateway/quota.go). The persisted counter
	// lives next to the slice store unless [billing] state_file overrides.
	if cfg.Billing.Enabled {
		statePath := cfg.Billing.StateFile
		if statePath == "" {
			statePath = filepath.Join(filepath.Dir(cfg.Store.DB), "quota-state.json")
		}
		qe, qerr := newQuotaEngine(cfg.Billing, statePath, g.client, g.now)
		if qerr != nil {
			_ = closeStore(store)
			return nil, fmt.Errorf("gateway: billing: %w", qerr)
		}
		g.quota = qe
	}
	return g, nil
}

// probeUpstreams is the default /healthz probe: every configured upstream
// must answer 2xx to GET {base_url}/models within the caller-provided
// timeout. Any non-2xx, network error or timeout marks the gateway
// unhealthy (fail-closed), so New API disables the channel rather than
// routing into a dead upstream.
func (g *Gateway) probeUpstreams(ctx context.Context) error {
	for _, up := range g.cfg.Upstreams {
		endpoint := strings.TrimRight(up.BaseURL, "/") + "/models"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return fmt.Errorf("upstream %s: %w", up.Name, err)
		}
		req.Header.Set("Authorization", "Bearer "+up.APIKey)
		resp, err := g.client.Do(req)
		if err != nil {
			// Do not wrap the raw *url.Error: its text embeds the full
			// base URL, which must never leak into the healthz response.
			reason := err.Error()
			var uerr *url.Error
			if errors.As(err, &uerr) {
				reason = uerr.Err.Error()
			}
			return fmt.Errorf("upstream %s unreachable: %s", up.Name, reason)
		}
		_ = resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("upstream %s unhealthy: status %d", up.Name, resp.StatusCode)
		}
	}
	return nil
}

// Close stops accepting new sidecar writes, waits for in-flight ingestion
// (write memory is best-effort but must not be silently dropped on
// shutdown), then releases the store.
func (g *Gateway) Close() error {
	g.shutdownMu.Lock()
	g.closing = true
	g.shutdownMu.Unlock()
	g.ingestWG.Wait()
	if err := g.retrievalEvents.close(); err != nil {
		log.Printf("gateway: retrieval events close: %v", err)
	}
	return closeStore(g.store)
}

func closeStore(store slice.Store) error {
	if closer, ok := store.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

// loadIndex rebuilds the in-memory index from the persisted store so L2/L3
// retrieval sees previously extracted slices (same pattern as the CLI's
// lookup/search command assembly).
func loadIndex(store slice.Store, idx slice.Index) error {
	items, err := store.ListAll()
	if err != nil {
		return err
	}
	// Batch path first: under the model embed backend one embeddings call
	// per slice would dominate gateway startup (see BatchInserter).
	if bi, ok := idx.(BatchInserter); ok {
		return bi.InsertBatch(items)
	}
	for _, s := range items {
		if err := idx.Insert(s); err != nil {
			return err
		}
	}
	return nil
}

// parseScope maps the configured scope name to the kernel scope enum.
func parseScope(s string) (slice.Scope, error) {
	switch s {
	case "", "project":
		return slice.Project, nil
	case "session":
		return slice.Session, nil
	case "user":
		return slice.User, nil
	default:
		return 0, fmt.Errorf("gateway config: [store] scope %q must be session, project, or user", s)
	}
}

// resolveScope picks the request scope: an optional x-semantix-scope header
// overrides the configured default (design §4.4 方案 B hook; the header is
// injected by the New API channel config, clients never touch it).
func (g *Gateway) resolveScope(r *http.Request) (slice.Scope, error) {
	v := r.Header.Get("x-semantix-scope")
	if v == "" {
		return g.injector.Scope, nil
	}
	return parseScope(v)
}

// cacheFresh is the gateway's final defensive TTL check after the kernel's
// age-aware candidate gate. An active window treats unknown, future and
// expired timestamps as stale; ttl<=0 explicitly disables the time policy.
func (g *Gateway) cacheFresh(s *slice.Slice, vendor string) bool {
	ttl := g.cfg.TTLFor(vendor)
	if ttl <= 0 {
		return true
	}
	now := g.now().Unix()
	if s.CreatedAt <= 0 || s.CreatedAt > now {
		return false
	}
	return now-s.CreatedAt <= ttl
}

// randomID returns a hex id for sidecar session files.
func randomID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------------------
// session sidecar (write memory, design §3.7)

// sessionIDPattern bounds the client-supplied session id so it can only
// name a sidecar file inside the sessions dir (no path traversal).
var sessionIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// recordSession appends this request/response pair to a session JSONL file
// (0600, ingest.JSONLSource-compatible lines) and asynchronously extracts it
// into the slice library. Best-effort: failures are logged, never returned
// to the client. ctxHash/model are stamped onto extracted Result slices so
// the L3 gate can isolate outcomes per context and model.
func (g *Gateway) recordSession(sessionID string, ctxHash, model string, turns []map[string]any) {
	g.shutdownMu.Lock()
	if g.closing {
		g.shutdownMu.Unlock()
		return
	}
	g.ingestWG.Add(1)
	g.shutdownMu.Unlock()

	if sessionID == "" || !sessionIDPattern.MatchString(sessionID) {
		sessionID = "gw-" + randomID()
	}
	dir := g.cfg.Ingest.SessionsDir
	if dir == "" {
		g.ingestWG.Done()
		return
	}
	path := filepath.Join(dir, sessionID+".jsonl")

	g.sessionsMu.Lock()
	err := appendJSONLLines(path, turns)
	g.sessionsMu.Unlock()
	if err != nil {
		g.ingestWG.Done()
		log.Printf("gateway: write session %s: %v", path, err)
		return
	}
	if g.disabled {
		// Ablation switch: keep the sidecar transcript (arms stay auditable
		// offline) but never grow the slice library on a disabled gateway.
		g.ingestWG.Done()
		return
	}
	go func() {
		defer g.ingestWG.Done()
		g.ingestSession(path, ctxHash, model)
	}()
}

// recordKernelEvent writes one kernel event into the same session JSONL used
// by the transcript ingest path. Keeping the original wire object makes the
// observation available both to event consumers and to searchable projections.
func (g *Gateway) recordKernelEvent(sessionID, ctxHash, model string, e kernelevent.Event) {
	raw, err := kernelevent.ToJSON(e)
	if err != nil {
		return
	}
	var line map[string]any
	if err := json.Unmarshal(raw, &line); err != nil {
		return
	}
	g.recordSession(sessionID, ctxHash, model, []map[string]any{line})
}

// ingestSession drains one sidecar file into the slice library via the
// kernel ingest pipeline. Result slices are stamped with the producing
// request's context hash and model (through metaStore) so the L3 gate can
// isolate outcomes per context and model; the L3-safe default adapter keeps
// gateway entries out of L3 unless explicitly configured.
func (g *Gateway) ingestSession(path, ctxHash, model string) {
	g.ingestMu.Lock()
	defer g.ingestMu.Unlock()
	src, err := ingest.NewJSONLSource(path)
	if err != nil {
		log.Printf("gateway: ingest %s: %v", path, err)
		return
	}
	p := ingest.Pipeline{
		Extractor: l3SafeExtractor{inner: slice.NewExtractor(), l3Safe: g.cfg.Ingest.L3SafeDefault},
		Store:     metaStore{inner: g.store, ctxHash: ctxHash, model: model},
		Index:     g.index,
		Scope:     g.injector.Scope,
		Project:   filepath.Base(path),
	}
	if _, err := p.Run(src); err != nil {
		log.Printf("gateway: ingest %s: %v", path, err)
	}
}

// metaStore stamps the producing request's context/model onto Result slices
// as they are persisted (the only type the L3 gate ever serves). Non-Result
// slices pass through untouched.
type metaStore struct {
	inner   slice.Store
	ctxHash string
	model   string
}

func (m metaStore) Put(s *slice.Slice) error {
	if s.Type == slice.Result {
		s.Meta.ContextHash = m.ctxHash
		s.Meta.Model = m.model
		if s.Meta.Origin == "" {
			// Issue #279: gateway ingestion is automatic extraction; a
			// missing tag (legacy extractor) must not silently read as a
			// higher trust level downstream.
			s.Meta.Origin = slice.OriginSessionAuto
		}
	}
	return m.inner.Put(s)
}

func (m metaStore) Get(id string) (*slice.Slice, error)            { return m.inner.Get(id) }
func (m metaStore) List(scope slice.Scope) ([]*slice.Slice, error) { return m.inner.List(scope) }
func (m metaStore) UpdateStats(id string, delta slice.SliceStats) error {
	return m.inner.UpdateStats(id, delta)
}

// UpdateStatsBatch forwards the batch capability so wrapping the store does
// not silently degrade stats write-back to per-ID rewrites.
func (m metaStore) UpdateStatsBatch(deltas map[string]slice.SliceStats) error {
	return slice.ApplyStats(m.inner, deltas)
}
func (m metaStore) ListAll() ([]*slice.Slice, error) { return m.inner.ListAll() }
func (m metaStore) Delete(id string) error           { return m.inner.Delete(id) }

// l3SafeExtractor delegates to the kernel extractor, then applies the
// configured L3-safe default to dependency-free Result slices (the only
// case where L3Safe is consulted — slices with captured Deps are inherently
// safe, kernel-side).
type l3SafeExtractor struct {
	inner  slice.Extractor
	l3Safe bool
}

func (e l3SafeExtractor) Extract(tr []byte, meta slice.SliceMeta) ([]*slice.Slice, error) {
	items, err := e.inner.Extract(tr, meta)
	if err != nil {
		return nil, err
	}
	for _, s := range items {
		if s.Type == slice.Result && len(s.Meta.Deps) == 0 {
			s.Meta.L3Safe = e.l3Safe
		}
	}
	return items, nil
}

// appendJSONLLines appends JSON objects as lines, creating the file with
// 0600 and the parent directory with 0700 (kernel permission convention).
func appendJSONLLines(path string, lines []map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, line := range lines {
		raw, err := json.Marshal(line)
		if err != nil {
			return err
		}
		if _, err := f.Write(append(raw, '\n')); err != nil {
			return err
		}
	}
	return nil
}
