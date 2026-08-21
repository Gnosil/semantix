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
	"time"

	"semantix/kernel/cache"
	"semantix/kernel/ingest"
	"semantix/kernel/inject"
	"semantix/kernel/judge"
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

	now func() time.Time
}

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
	// Startup is a process boundary: fold any journal into the base before
	// serving (freeze-window semantics — the library never shifts mid-flight).
	// Best-effort: a failed fold still leaves a consistent store.
	if c, ok := store.(interface{ Compact() error }); ok {
		if err := c.Compact(); err != nil {
			log.Printf("gateway: store compact: %v", err)
		}
	}
	idx := newRetriever(cfg.Retrieval.Retriever, cfg.Retrieval.VectorDim)
	if err := loadIndex(store, idx); err != nil {
		_ = closeStore(store)
		return nil, fmt.Errorf("gateway: rebuild index: %w", err)
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
		})
		if err != nil {
			_ = closeStore(store)
			return nil, fmt.Errorf("gateway: judge: %w", err)
		}
		llmJudge = j
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
	z := zone.Default()

	var rec *usage.Recorder
	if cfg.Ingest.UsageLog != "" {
		rec, err = usage.NewRecorder(cfg.Ingest.UsageLog)
		if err != nil {
			_ = closeStore(store)
			return nil, fmt.Errorf("gateway: usage recorder: %w", err)
		}
	}

	g := &Gateway{
		cfg:      cfg,
		store:    store,
		index:    idx,
		decider:  &cache.L3Decider{Index: idx, Store: store, Root: root, K: topK, Judge: llmJudge},
		injector: &inject.Injector{Index: idx, Store: store, Scope: scope, K: topK, Budget: budget, Zones: &z},
		usageLog: rec,
		client:   &http.Client{Timeout: 120 * time.Second},
		disabled: disableEnv(),
		now:      time.Now,
	}
	// Grey-zone judge decisions become durable here (Issue #242 gap 1):
	// one structured log line plus a field on the turn's usage event, so a
	// non-hit can be explained and the judge's own model call can be costed.
	g.decider.OnJudge = g.observeJudge
	g.healthProbe = g.probeUpstreams
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
	go func() {
		defer g.ingestWG.Done()
		g.ingestSession(path, ctxHash, model)
	}()
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
