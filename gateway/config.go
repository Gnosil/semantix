// Package gateway implements the Semantix Gateway v1 (Issue #133): an
// OpenAI-compatible HTTP gateway that sits between New API and upstream
// LLMs, running every request through the kernel three-layer cache before
// forwarding. Zero new core logic — it wires kernel/cache, kernel/inject,
// kernel/fingerprint, kernel/slice, kernel/ingest and kernel/usage.
//
// Design: docs/specs/newapi-gateway-design.md.
package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"semantix/kernel/evolve"
	"semantix/kernel/fuse"
	"semantix/kernel/slice"
	"semantix/kernel/zone"

	"github.com/BurntSushi/toml"
)

// Config mirrors semantix-gateway.toml (design doc §3.9).
type Config struct {
	Server    ServerConfig     `toml:"server"`
	Store     StoreConfig      `toml:"store"`
	Retrieval RetrievalConfig  `toml:"retrieval"`
	Cache     CacheConfig      `toml:"cache"`
	Ingest    IngestConfig     `toml:"ingest"`
	Slice     SliceConfig      `toml:"slice"`
	Sanitize  SanitizeConfig   `toml:"sanitize"`
	Billing   BillingConfig    `toml:"billing"`
	Upstreams []UpstreamConfig `toml:"upstreams"`
}

// SanitizeConfig is the prefix-hygiene middleware (GLM-P0-2, Issue #290).
// Third-party GLM-style caches are byte-exact over the whole prefix
// (glm-spike-week.md §3), and unlike first-party endpoints they do not strip
// harness billing markers server-side — a per-request marker in the system
// head breaks the cache from the first token (133× hit-rate difference
// reported for Claude Code's attribution header, spec §3.3/§4.1-1).
type SanitizeConfig struct {
	// StripAttribution removes attribution/billing marker lines from the head
	// of the first system message before the body reaches the upstream.
	// Default ON (nil = true); set strip_attribution = false to forward the
	// client request untouched (documented cost: cache spend up to 4-5×).
	StripAttribution *bool `toml:"strip_attribution"`
	// AttributionMarkers are the line prefixes recognized as attribution
	// segments. Merged with the built-in defaults (x-anthropic-billing-header).
	AttributionMarkers []string `toml:"attribution_markers"`
	// SortTools canonicalizes the request tools array by tool name so client
	// enumeration order cannot break byte-exact prefix caches. Default ON
	// (nil = true). Semantically neutral: tool choice is name-addressed.
	SortTools *bool `toml:"sort_tools"`
}

// StripAttributionEnabled reports the effective default-on switch.
func (s SanitizeConfig) StripAttributionEnabled() bool {
	return s.StripAttribution == nil || *s.StripAttribution
}

// SortToolsEnabled reports the effective default-on switch.
func (s SanitizeConfig) SortToolsEnabled() bool {
	return s.SortTools == nil || *s.SortTools
}

// builtinAttributionMarkers match the known harness billing markers that
// third-party endpoints do not strip server-side.
var builtinAttributionMarkers = []string{"x-anthropic-billing-header"}

// EffectiveAttributionMarkers merges configured markers with the built-ins.
func (s SanitizeConfig) EffectiveAttributionMarkers() []string {
	out := append([]string(nil), builtinAttributionMarkers...)
	for _, m := range s.AttributionMarkers {
		m = strings.TrimSpace(m)
		if m != "" {
			out = append(out, m)
		}
	}
	return out
}

// ServerConfig is the listener and gateway-key settings.
type ServerConfig struct {
	Addr       string `toml:"addr"`
	GatewayKey string `toml:"gateway_key"`
	// HealthTimeoutSeconds bounds the /healthz upstream probe across all
	// configured upstreams. 0 disables the probe (local readiness only).
	HealthTimeoutSeconds int `toml:"health_timeout_seconds"`
}

// StoreConfig selects the slice store (which also holds L3 cache entries,
// per decision D4: one JSONL store).
type StoreConfig struct {
	DB    string `toml:"db"`
	Scope string `toml:"scope"`
	// DepsRoot is the project root that L3 dep fingerprints are verified
	// against (design §3.5: "deps root provided by config"). Missing files
	// fail closed → the cached entry is treated as stale.
	DepsRoot string `toml:"deps_root"`
	// MaxSlices caps the library; the worst-scored slices are archived down
	// to this count at startup (spec slice-value-eviction §4). Pointer so an
	// absent key gets the default while an explicit 0 disables the cap.
	MaxSlices *int `toml:"max_slices"`
}

// defaultMaxSlices matches the CLI config default (store.max_slices).
const defaultMaxSlices = 5000

// EffectiveMaxSlices resolves the cap: absent → default, explicit 0 → off.
func (s StoreConfig) EffectiveMaxSlices() int {
	if s.MaxSlices == nil {
		return defaultMaxSlices
	}
	return *s.MaxSlices
}

// RetrievalConfig tunes the L2 injector.
type RetrievalConfig struct {
	Retriever string `toml:"retriever"` // bm25 | vector | hybrid (Issue #186); vector/hybrid use kernel/embed HashEmbedder
	TopK      int    `toml:"top_k"`
	Budget    int    `toml:"budget"`
	VectorDim int    `toml:"vector_dim"` // HashEmbedder dimension (<=0 -> 256)
	// Fusion selects the hybrid fusion strategy (Issue #274): ""/weighted
	// (default, historical score average) | rrf (reciprocal rank fusion).
	Fusion string `toml:"fusion"`
	// RrfK is the RRF constant; 0 → fuse.DefaultRrfK (60). Only read in
	// rrf mode.
	RrfK int `toml:"rrf_k"`
	// BM25Weight is the weighted-mode BM25 route share in [0,1]; nil →
	// fuse.DefaultBM25Weight (0.5), explicit 0 = pure vector route. Only
	// read in weighted mode.
	BM25Weight *float64 `toml:"bm25_weight"`
	// EmbedBackend selects the vector-route embedding backend: "" / "hash"
	// (deterministic, offline) or "model" (remote OpenAI-compatible
	// embeddings API + HNSW ANN index; key from SEMANTIX_EMBED_API_KEY,
	// fail-open to hash). W2 of the efficiency research plan.
	EmbedBackend string `toml:"embed_backend"`
	EmbedBaseURL string `toml:"embed_base_url"`
	EmbedModel   string `toml:"embed_model"`
	// GreyMode: "drop" (default, fail-closed) or "audit" (grey slices enter
	// the injection block under a separate unverified marker, so grey-zone
	// hit-rate loss is measurable and recoverable instead of silent — GW4
	// measured 8 of 10 repeated tasks landing in grey). W3.
	GreyMode string `toml:"grey_mode"`
	// Grey-zone thresholds (Issue #259 阶段 1). An explicit tau_*/abs_*
	// key overrides zone.Default(); unspecified keys fall back to the
	// tuned defaults. Validation: 0 < tau <= 1, abs >= 0, tau_high >
	// tau_low. EvolveDB is an optional evolve state dir (params.json,
	// written by `usage --evolve-db`): its tuned TauL2 drives TauLow when
	// no explicit tau_low is configured (clamped to the evolve tuning
	// band), mirroring the CLI --evolve-db behavior.
	TauHigh  *float64 `toml:"tau_high"`
	TauLow   *float64 `toml:"tau_low"`
	AbsHigh  *float64 `toml:"abs_high"`
	AbsLow   *float64 `toml:"abs_low"`
	EvolveDB string   `toml:"evolve_db"`
	// Adaptive enables per-entry online TauLow learning (Issue #259 阶段
	// 3, vCache route): each high-frequency reused slice learns its own
	// grey floor from positive/negative evidence, with the global
	// thresholds as cold-start prior. nil → enabled; explicit false
	// disables (all entries use the global/per-type thresholds).
	// ErrorBound is the operator-specified false-hit rate ceiling
	// (0 → 0.05; -1 also disables adaptation). AdaptDB is the state file
	// (empty → <store dir>/l3-adapt.json).
	Adaptive   *bool   `toml:"adaptive"`
	ErrorBound float64 `toml:"error_bound"`
	AdaptDB    string  `toml:"adapt_db"`
	// ByType holds per-slice-type threshold overrides (Issue #259 阶段
	// 2), keyed by the stable wire name (prompt|context|tool_pattern|
	// result|memory). Each entry is partial: only the keys present are
	// set, everything else inherits the effective global thresholds when
	// assembled into zone.Zones.ByType. Unknown type names fail startup.
	ByType map[string]zoneOverride `toml:"by_type"`
	// EventsLog, when non-empty, appends one JSONL event per L2 retrieval
	// carrying the query and the full per-candidate admission trace
	// (inject.Injection.Decisions) — the offline training corpus for the
	// local retrieval model (docs/specs/local-retrieval-model.md §3 G1).
	// Strictly opt-in: the empty default writes nothing. The log carries
	// query plaintext, so production configs must leave it off; it exists
	// for the retrieval-lab arms.
	EventsLog string `toml:"events_log"`
	// RerankBaseURL, when non-empty, decorates the retriever with the local
	// reranker (spec §3 B): candidates are over-fetched to RerankTopN and
	// POSTed to {base}/rerank; the reply's order and bounded [0,1] scores
	// replace the route scores. Fail-soft: any error returns the inner
	// results untouched. The protocol is unauthenticated plaintext HTTP, so
	// validation restricts the host to loopback (spec §8).
	RerankBaseURL string `toml:"rerank_base_url"`
	// RerankTopN is the over-fetch depth handed to the reranker (0 → 20).
	RerankTopN int `toml:"rerank_top_n"`
	// RerankTimeoutMs bounds one rerank call (0 → 300).
	RerankTimeoutMs int `toml:"rerank_timeout_ms"`
}

// zoneOverride is the partial per-type override syntax for [retrieval]
// by_type.<type>: absent fields inherit the global thresholds.
type zoneOverride struct {
	TauHigh *float64 `toml:"tau_high"`
	TauLow  *float64 `toml:"tau_low"`
	AbsHigh *float64 `toml:"abs_high"`
	AbsLow  *float64 `toml:"abs_low"`
}

// CacheConfig holds L3 policy. TTL is resolved by the gateway and passed to
// the kernel's age-aware L3 gate: candidates in the second half of the window
// require a judge, while expired or unstamped candidates fail closed. The
// dependency-fingerprint chain remains the content-change authority.
// TTLSeconds is the generic window; VendorTTL overrides it per vendor (design
// §3.5: DeepSeek 24h / Anthropic 5m) and wins over the built-in defaults.
type CacheConfig struct {
	TTLSeconds    int64            `toml:"ttl_seconds"`
	VendorTTL     map[string]int64 `toml:"vendor_ttl"`
	JudgeAPIKey   string           `toml:"judge_api_key"`
	JudgeBaseURL  string           `toml:"judge_base_url"`
	JudgeModel    string           `toml:"judge_model"`
	JudgeProtocol string           `toml:"judge_protocol"`
	// JudgeTimeoutMs bounds one judge HTTP call. The kernel default is 30s,
	// which on the synchronous L3 path directly delays TTFT; a shorter
	// window (recommended: 2000-5000) fails closed fast, and the verdict
	// cache warms the answer in the background for the next occurrence.
	JudgeTimeoutMs int `toml:"judge_timeout_ms"`
	// LexicalFloor is the L3 lexical-support floor (Issue #260): a zone-Hit
	// with less term overlap is downgraded to Grey (judge-gated). nil keeps
	// the kernel default (0.05); explicit 0 disables the gate.
	LexicalFloor *float64 `toml:"lexical_floor"`
	// FalseHitSim is the suspected-false-hit retry similarity threshold
	// (normalized edit ratio in [0,1], Issue #262 §3.3). 0/unspecified →
	// DefaultFalseHitSim (0.6); -1 disables the retry detection.
	FalseHitSim float64 `toml:"false_hit_sim"`
	// Promote wiring (Issue #280): a consensus-approved (query, slice,
	// version) skips the judge on later grey hits. PromoteDB is the
	// promotion entry file (empty → promote disabled); PromoteTTLSeconds
	// bounds promotion validity (0 → DefaultPromoteTTL 7d); PromoteConsensus
	// selects the promotion gate (2 = dual-rubric consensus, 1 = single-
	// judgement baseline); RejectLimit blacklists a (query, slice) after
	// that many in-window rejections (0 → DefaultRejectLimit 2; -1 disables
	// the blacklist).
	PromoteDB         string `toml:"promote_db"`
	PromoteTTLSeconds int64  `toml:"promote_ttl_seconds"`
	PromoteConsensus  int    `toml:"promote_consensus"`
	RejectLimit       int    `toml:"reject_limit"`
}

// DefaultFalseHitSim is the built-in retry-similarity threshold when
// [cache] false_hit_sim is unspecified.
const DefaultFalseHitSim = 0.6

// Default promote tuning (Issue #280): promotion entries stay valid for 7
// days (Security §4.2.4: "verified" never exempts from TTL), the promotion
// gate is the dual-rubric consensus, and a (query, slice) is blacklisted
// after 2 in-window rejections.
const (
	DefaultPromoteTTLSeconds = 7 * 24 * 60 * 60
	DefaultPromoteConsensus  = 2
	DefaultRejectLimit       = 2
)

// BillingConfig is the customer free-tier gate (see gateway/quota.go):
// the first FreeTokens upstream tokens are served for free; after that the
// gateway answers 402 with RechargeURL until the customer's platform wallet
// reports an active balance.
type BillingConfig struct {
	Enabled bool `toml:"enabled"`
	// FreeTokens is the free-tier size in tokens (prompt + completion of
	// forwarded requests; L3 cache hits are free). 0 defaults to 10M.
	FreeTokens int64 `toml:"free_tokens"`
	// RechargeURL is the platform top-up page shown in the 402 message and
	// the x-semantix-quota-recharge-url header. Required when enabled.
	RechargeURL string `toml:"recharge_url"`
	// StateFile persists the token counter across restarts. Defaults to
	// quota-state.json next to the slice store db.
	StateFile string `toml:"state_file"`
	// BalanceURL + BalanceKey (both or neither) probe the platform wallet
	// (DeepSeek GET /user/balance schema) once the tier is exhausted: an
	// available balance unlocks paid mode automatically after a top-up.
	BalanceURL string `toml:"balance_url"`
	BalanceKey string `toml:"balance_key"`
	// BalanceCacheSeconds bounds probe frequency (0 defaults to 300).
	BalanceCacheSeconds int `toml:"balance_cache_seconds"`
}

// defaultFreeTokens is the free tier granted to every customer install:
// 10,000,000 tokens (产品口径: 前 1000 万 token 免费).
const defaultFreeTokens int64 = 10_000_000

// IngestConfig controls the session-sidecar write path.
// SliceConfig holds slice provenance/trust policy (Issue #279).
type SliceConfig struct {
	// MinInjectOrigin is the lowest provenance level admitted to L2
	// injection and L3 Hit pass-through: ""/import admit everything except
	// nothing (level 1 floor), "session-auto" (default) excludes
	// import/legacy, "user-curated" admits only curated slices. See
	// kernel/slice Origin.Level.
	MinInjectOrigin string `toml:"min_inject_origin"`
}

// minInjectOrigin resolves the provenance floor to a kernel/slice.Origin.
// The zero value (unset) keeps the kernel default — no filtering — so an
// unconfigured gateway behaves exactly like before; the explicit default
// for production configs is session-auto (fail-closed for import/legacy).
func (c *Config) minInjectOrigin() slice.Origin {
	switch c.Slice.MinInjectOrigin {
	case "import", "session-auto", "prefetch", "user-curated":
		return slice.Origin(c.Slice.MinInjectOrigin)
	case "":
		return slice.Origin("") // kernel default: level-1 floor, no filtering
	default:
		return slice.Origin("") // validate() rejects unknown values
	}
}

type IngestConfig struct {
	SessionsDir string `toml:"sessions_dir"`
	// UsageLog is the kernel/usage event log (design §4.3: gateway usage
	// accounting, reconciled against New API billing).
	UsageLog      string `toml:"usage_log"`
	L3SafeDefault bool   `toml:"l3_safe_default"`
}

// UpstreamConfig is one model channel (New API channel = one upstream).
type UpstreamConfig struct {
	Name          string   `toml:"name"`
	BaseURL       string   `toml:"base_url"`
	APIKey        string   `toml:"api_key"`
	ModelAlias    []string `toml:"model_alias"`
	UpstreamModel string   `toml:"upstream_model"`
	Vendor        string   `toml:"vendor"`
	// StripCacheControl removes cache_control fields from the outgoing body
	// for this upstream. Default OFF: both measured GLM-hosting stacks accept
	// cache_control without error (glm-spike-week.md §2/§3A.2), so the gateway
	// forwards it untouched unless a provider is known to reject it.
	StripCacheControl bool `toml:"strip_cache_control"`
}

// vendor names accepted by the v1 gateway. anthropic needs message-format
// conversion + cache_control breakpoints (design §0.5), handled by
// gateway/anthropic.go on the upstream hop; the other three speak OpenAI
// protocol natively.
var supportedVendors = map[string]bool{
	"deepseek":  true,
	"openai":    true,
	"moonshot":  true,
	"anthropic": true,
}

// defaultVendorTTLSeconds are the vendor-aware L3 TTL windows from design
// §3.5 (DeepSeek 24h / Anthropic 5m). An explicit [cache] vendor_ttl entry
// overrides these; anything else falls back to [cache] ttl_seconds.
var defaultVendorTTLSeconds = map[string]int64{
	"deepseek":  24 * 60 * 60,
	"anthropic": 5 * 60,
}

// TTLFor resolves the cache freshness window for an upstream vendor:
// explicit vendor_ttl config wins, then the built-in vendor default, then
// the generic ttl_seconds (<=0 disables the time window entirely).
func (c *Config) TTLFor(vendor string) int64 {
	if v, ok := c.Cache.VendorTTL[vendor]; ok {
		return v
	}
	if v, ok := defaultVendorTTLSeconds[vendor]; ok {
		return v
	}
	return c.Cache.TTLSeconds
}

// and ~ paths, then validates. Any unresolved ${...} fails startup so a
// literal placeholder can never be used as a credential.
func Load(path string) (*Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return nil, fmt.Errorf("gateway config %s: %w", path, err)
	}
	if err := c.expand(); err != nil {
		return nil, err
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// expand resolves ${VAR} references in every string field (fail-closed) and
// expands a leading ~ in filesystem paths.
func (c *Config) expand() error {
	fields := []*string{
		&c.Server.Addr, &c.Server.GatewayKey,
		&c.Store.DB, &c.Store.Scope, &c.Store.DepsRoot,
		&c.Retrieval.Retriever,
		&c.Cache.JudgeAPIKey,
		&c.Cache.JudgeBaseURL,
		&c.Cache.JudgeModel,
		&c.Cache.JudgeProtocol,
		&c.Ingest.SessionsDir, &c.Ingest.UsageLog,
		&c.Billing.RechargeURL, &c.Billing.StateFile,
		&c.Billing.BalanceURL, &c.Billing.BalanceKey,
	}
	for i := range c.Upstreams {
		u := &c.Upstreams[i]
		fields = append(fields, &u.Name, &u.BaseURL, &u.APIKey, &u.UpstreamModel, &u.Vendor)
		for j := range u.ModelAlias {
			// Take the address of the slice element itself — a copy would
			// make the ${VAR} substitution below a no-op.
			fields = append(fields, &u.ModelAlias[j])
		}
	}
	for _, f := range fields {
		if err := expandField(f); err != nil {
			return err
		}
	}
	if c.Store.DB != "" {
		if home, err := expandHome(c.Store.DB); err != nil {
			return err
		} else {
			c.Store.DB = home
		}
	}
	if c.Store.DepsRoot != "" {
		if home, err := expandHome(c.Store.DepsRoot); err != nil {
			return err
		} else {
			c.Store.DepsRoot = home
		}
	}
	if c.Ingest.SessionsDir != "" {
		if home, err := expandHome(c.Ingest.SessionsDir); err != nil {
			return err
		} else {
			c.Ingest.SessionsDir = home
		}
	}
	if c.Ingest.UsageLog != "" {
		if home, err := expandHome(c.Ingest.UsageLog); err != nil {
			return err
		} else {
			c.Ingest.UsageLog = home
		}
	}
	if c.Billing.StateFile != "" {
		if home, err := expandHome(c.Billing.StateFile); err != nil {
			return err
		} else {
			c.Billing.StateFile = home
		}
	}
	return nil
}

// expandField replaces every ${VAR} with the environment value, failing on
// unknown variables (never leaves a placeholder in place).
func expandField(v *string) error {
	if *v == "" {
		return nil
	}
	var b strings.Builder
	rest := *v
	for {
		start := strings.Index(rest, "${")
		if start < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:start])
		end := strings.Index(rest[start:], "}")
		if end < 0 {
			return fmt.Errorf("gateway config: unterminated ${ in %q", *v)
		}
		name := rest[start+2 : start+end]
		val, ok := os.LookupEnv(name)
		if !ok {
			return fmt.Errorf("gateway config: environment variable %s is not set (referenced in %q)", name, *v)
		}
		b.WriteString(val)
		rest = rest[start+end+1:]
	}
	*v = b.String()
	return nil
}

// expandHome expands a leading ~ (or ~/) to the user home directory.
func expandHome(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("gateway config: resolve home for %q: %w", p, err)
		}
		if p == "~" {
			return home, nil
		}
		return filepath.Join(home, p[2:]), nil
	}
	return p, nil
}

// validate enforces the invariants the server depends on.
func (c *Config) validate() error {
	if strings.TrimSpace(c.Server.Addr) == "" {
		return fmt.Errorf("gateway config: [server] addr is required")
	}
	if c.Server.GatewayKey == "" {
		return fmt.Errorf("gateway config: [server] gateway_key is required (New API channel key)")
	}
	if strings.TrimSpace(c.Store.DB) == "" {
		return fmt.Errorf("gateway config: [store] db is required")
	}
	if c.Store.Scope != "" && !validScope(c.Store.Scope) {
		return fmt.Errorf("gateway config: [store] scope %q must be session, project, or user", c.Store.Scope)
	}
	if c.Retrieval.Retriever != "" && !validRetriever(c.Retrieval.Retriever) {
		return fmt.Errorf("gateway config: [retrieval] retriever %q is not supported (supported: bm25, vector, hybrid)", c.Retrieval.Retriever)
	}
	if c.Slice.MinInjectOrigin != "" && c.Slice.MinInjectOrigin != "import" &&
		c.Slice.MinInjectOrigin != "session-auto" && c.Slice.MinInjectOrigin != "prefetch" &&
		c.Slice.MinInjectOrigin != "user-curated" {
		return fmt.Errorf("gateway config: [slice] min_inject_origin %q must be import, session-auto, prefetch, or user-curated", c.Slice.MinInjectOrigin)
	}
	if c.Retrieval.Fusion != "" && c.Retrieval.Fusion != "weighted" && c.Retrieval.Fusion != "rrf" {
		return fmt.Errorf("gateway config: [retrieval] fusion %q must be weighted or rrf", c.Retrieval.Fusion)
	}
	if c.Retrieval.RerankBaseURL != "" {
		if err := validateLoopbackURL(c.Retrieval.RerankBaseURL); err != nil {
			// The rerank protocol is unauthenticated plaintext HTTP
			// (spec §8): anything beyond loopback would ship query text and
			// slice content across the network in the clear.
			return fmt.Errorf("gateway config: [retrieval] rerank_base_url: %w", err)
		}
	}
	if c.Retrieval.RerankTopN < 0 {
		return fmt.Errorf("gateway config: [retrieval] rerank_top_n must be >= 0 (0 = default)")
	}
	if c.Retrieval.RerankTimeoutMs < 0 {
		return fmt.Errorf("gateway config: [retrieval] rerank_timeout_ms must be >= 0 (0 = default)")
	}
	if c.Retrieval.RrfK < 0 {
		return fmt.Errorf("gateway config: [retrieval] rrf_k must be >= 0 (0 = fuse default)")
	}
	if c.Retrieval.BM25Weight != nil {
		w := *c.Retrieval.BM25Weight
		if math.IsNaN(w) || math.IsInf(w, 0) || w < 0 || w > 1 {
			return fmt.Errorf("gateway config: [retrieval] bm25_weight must be in [0,1], got %v", w)
		}
	}
	// Grey-zone thresholds (Issue #259 阶段 1): taus are relative
	// confidences in (0,1], abs floors are non-negative; NaN/Inf must be
	// rejected because comparisons are false for NaN and would silently
	// distort classification.
	for _, th := range []struct {
		key string
		v   *float64
	}{
		{"tau_high", c.Retrieval.TauHigh},
		{"tau_low", c.Retrieval.TauLow},
		{"abs_high", c.Retrieval.AbsHigh},
		{"abs_low", c.Retrieval.AbsLow},
	} {
		if th.v == nil {
			continue
		}
		v := *th.v
		isTau := th.key == "tau_high" || th.key == "tau_low"
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("gateway config: [retrieval] %s must be a finite number, got %v", th.key, v)
		}
		if isTau && (v <= 0 || v > 1) {
			return fmt.Errorf("gateway config: [retrieval] %s must be in (0,1], got %v", th.key, v)
		}
		if !isTau && v < 0 {
			return fmt.Errorf("gateway config: [retrieval] %s must be >= 0, got %v", th.key, v)
		}
	}
	if c.Retrieval.TauHigh != nil && c.Retrieval.TauLow != nil && *c.Retrieval.TauHigh <= *c.Retrieval.TauLow {
		return fmt.Errorf("gateway config: [retrieval] tau_high (%v) must be > tau_low (%v)", *c.Retrieval.TauHigh, *c.Retrieval.TauLow)
	}
	// Adaptive error bound (Issue #259 阶段 3): -1 disables per-entry
	// adaptation; any other value must be a rate in [0,1].
	if c.Retrieval.ErrorBound != 0 && c.Retrieval.ErrorBound != -1 {
		if math.IsNaN(c.Retrieval.ErrorBound) || math.IsInf(c.Retrieval.ErrorBound, 0) ||
			c.Retrieval.ErrorBound < 0 || c.Retrieval.ErrorBound > 1 {
			return fmt.Errorf("gateway config: [retrieval] error_bound must be -1 (disabled) or in [0,1], got %v", c.Retrieval.ErrorBound)
		}
	}
	// Per-type overrides (Issue #259 阶段 2): every by_type key must be a
	// known slice type (fail closed on typos) and each override obeys the
	// same value-domain rules as the global thresholds.
	for name, o := range c.Retrieval.ByType {
		if _, ok := slice.TypeFromString(name); !ok {
			return fmt.Errorf("gateway config: [retrieval] by_type key %q is not a slice type (want prompt|context|tool_pattern|result|memory)", name)
		}
		for _, th := range []struct {
			key string
			v   *float64
		}{
			{"tau_high", o.TauHigh},
			{"tau_low", o.TauLow},
			{"abs_high", o.AbsHigh},
			{"abs_low", o.AbsLow},
		} {
			if th.v == nil {
				continue
			}
			v := *th.v
			isTau := th.key == "tau_high" || th.key == "tau_low"
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return fmt.Errorf("gateway config: [retrieval] by_type.%s.%s must be a finite number, got %v", name, th.key, v)
			}
			if isTau && (v <= 0 || v > 1) {
				return fmt.Errorf("gateway config: [retrieval] by_type.%s.%s must be in (0,1], got %v", name, th.key, v)
			}
			if !isTau && v < 0 {
				return fmt.Errorf("gateway config: [retrieval] by_type.%s.%s must be >= 0, got %v", name, th.key, v)
			}
		}
		if o.TauHigh != nil && o.TauLow != nil && *o.TauHigh <= *o.TauLow {
			return fmt.Errorf("gateway config: [retrieval] by_type.%s tau_high (%v) must be > tau_low (%v)", name, *o.TauHigh, *o.TauLow)
		}
	}
	if c.Cache.JudgeAPIKey != "" && (strings.TrimSpace(c.Cache.JudgeBaseURL) == "" || strings.TrimSpace(c.Cache.JudgeModel) == "") {
		return fmt.Errorf("gateway config: [cache] judge_api_key requires judge_base_url and judge_model")
	}
	if c.Cache.JudgeProtocol != "" && c.Cache.JudgeProtocol != "openai" && c.Cache.JudgeProtocol != "anthropic" {
		return fmt.Errorf("gateway config: [cache] judge_protocol %q must be openai or anthropic", c.Cache.JudgeProtocol)
	}
	if c.Cache.TTLSeconds < 0 {
		return fmt.Errorf("gateway config: [cache] ttl_seconds must be >= 0 (0 disables the time window)")
	}
	if c.Cache.FalseHitSim != 0 {
		// -1 disables the retry detection; otherwise a similarity in [0,1].
		if c.Cache.FalseHitSim != -1 && (math.IsNaN(c.Cache.FalseHitSim) || math.IsInf(c.Cache.FalseHitSim, 0) ||
			c.Cache.FalseHitSim < 0 || c.Cache.FalseHitSim > 1) {
			return fmt.Errorf("gateway config: [cache] false_hit_sim must be -1 (disabled) or in [0,1], got %v", c.Cache.FalseHitSim)
		}
	}
	// Promotion wiring (Issue #280): consensus ∈ {1,2}; TTL >= 0; reject
	// limit >= -1 (-1 disables the blacklist).
	if c.Cache.PromoteConsensus != 0 && c.Cache.PromoteConsensus != 1 && c.Cache.PromoteConsensus != 2 {
		return fmt.Errorf("gateway config: [cache] promote_consensus must be 1 (single judgement) or 2 (dual-rubric consensus), got %d", c.Cache.PromoteConsensus)
	}
	if c.Cache.PromoteTTLSeconds < 0 {
		return fmt.Errorf("gateway config: [cache] promote_ttl_seconds must be >= 0 (0 uses the default %d)", DefaultPromoteTTLSeconds)
	}
	if c.Cache.RejectLimit < -1 {
		return fmt.Errorf("gateway config: [cache] reject_limit must be >= -1 (-1 disables the blacklist), got %d", c.Cache.RejectLimit)
	}
	if c.Server.HealthTimeoutSeconds < 0 {
		return fmt.Errorf("gateway config: [server] health_timeout_seconds must be >= 0 (0 disables the upstream probe)")
	}
	if c.Store.MaxSlices != nil && *c.Store.MaxSlices < 0 {
		return fmt.Errorf("gateway config: [store] max_slices must be >= 0 (0 disables the cap)")
	}
	if c.Billing.Enabled {
		if c.Billing.FreeTokens < 0 {
			return fmt.Errorf("gateway config: [billing] free_tokens must be >= 0 (0 uses the default %d)", defaultFreeTokens)
		}
		if c.Billing.FreeTokens == 0 {
			c.Billing.FreeTokens = defaultFreeTokens
		}
		if strings.TrimSpace(c.Billing.RechargeURL) == "" {
			return fmt.Errorf("gateway config: [billing] recharge_url is required when billing is enabled (the top-up page customers are sent to)")
		}
		if (c.Billing.BalanceURL == "") != (c.Billing.BalanceKey == "") {
			return fmt.Errorf("gateway config: [billing] balance_url and balance_key must be set together")
		}
		if c.Billing.BalanceCacheSeconds < 0 {
			return fmt.Errorf("gateway config: [billing] balance_cache_seconds must be >= 0 (0 uses the default 300)")
		}
		if c.Billing.BalanceCacheSeconds == 0 {
			c.Billing.BalanceCacheSeconds = 300
		}
	}
	if len(c.Upstreams) == 0 {
		return fmt.Errorf("gateway config: at least one [[upstreams]] entry is required")
	}
	seenModel := map[string]string{}
	for i := range c.Upstreams {
		u := &c.Upstreams[i]
		if strings.TrimSpace(u.Name) == "" {
			return fmt.Errorf("gateway config: upstreams[%d]: name is required", i)
		}
		if strings.TrimSpace(u.BaseURL) == "" || strings.TrimSpace(u.APIKey) == "" {
			return fmt.Errorf("gateway config: upstreams[%d] (%s): base_url and api_key are required", i, u.Name)
		}
		if strings.TrimSpace(u.UpstreamModel) == "" {
			return fmt.Errorf("gateway config: upstreams[%d] (%s): upstream_model is required", i, u.Name)
		}
		if len(u.ModelAlias) == 0 {
			return fmt.Errorf("gateway config: upstreams[%d] (%s): model_alias must list at least one alias", i, u.Name)
		}
		if !supportedVendors[u.Vendor] {
			return fmt.Errorf("gateway config: upstreams[%d] (%s): vendor %q is not supported by gateway v1 (supported: deepseek, openai, moonshot, anthropic)", i, u.Name, u.Vendor)
		}
		for _, alias := range u.ModelAlias {
			if prev, dup := seenModel[alias]; dup {
				return fmt.Errorf("gateway config: model alias %q is declared by both %q and %q", alias, prev, u.Name)
			}
			seenModel[alias] = u.Name
		}
	}
	return nil
}

func validScope(s string) bool {
	switch s {
	case "session", "project", "user":
		return true
	}
	return false
}

// validateLoopbackURL accepts only http URLs whose host resolves textually
// to loopback (127.0.0.1, ::1, or localhost). Used for the rerank sidecar,
// whose wire protocol has no authentication.
func validateLoopbackURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	if u.Scheme != "http" {
		return fmt.Errorf("%q must use plain http on loopback (got scheme %q)", raw, u.Scheme)
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "::1" && host != "localhost" {
		return fmt.Errorf("host %q must be loopback (127.0.0.1, ::1, or localhost)", host)
	}
	return nil
}

func validRetriever(s string) bool {
	switch s {
	case "bm25", "vector", "hybrid":
		return true
	}
	return false
}

// fusionConfig maps the [retrieval] fusion keys onto kernel/fuse.Config
// (Issue #274). Zero/unset values fall back to the fuse package defaults.
func (c *Config) fusionConfig() fuse.Config {
	cfg := fuse.Config{RrfK: c.Retrieval.RrfK, BM25Weight: c.Retrieval.BM25Weight}
	if c.Retrieval.Fusion == "rrf" {
		cfg.Strategy = fuse.RRF
	}
	return cfg
}

// zoneConfig resolves the effective grey-zone thresholds (Issue #259):
// explicit tau_*/abs_* keys win over zone.Default(); when tau_low is not
// configured and evolve_db points at an evolve state dir, the engine's
// tuned TauL2 drives TauLow (clamped to the evolve tuning band). The
// result is the exact classifier the decider and injector will use.
func (c *Config) zoneConfig() (zone.Zones, error) {
	z := zone.Default()
	if c.Retrieval.TauHigh != nil {
		z.TauHigh = *c.Retrieval.TauHigh
	}
	if c.Retrieval.TauLow != nil {
		z.TauLow = *c.Retrieval.TauLow
	}
	if c.Retrieval.AbsHigh != nil {
		z.AbsHigh = *c.Retrieval.AbsHigh
	}
	if c.Retrieval.AbsLow != nil {
		z.AbsLow = *c.Retrieval.AbsLow
	}
	if c.Retrieval.TauLow == nil && c.Retrieval.EvolveDB != "" {
		tau, err := loadEvolveTauL2(c.Retrieval.EvolveDB)
		if err != nil {
			return zone.Zones{}, err
		}
		if tau > 0 {
			z.TauLow = tau
		}
	}
	// Per-type overrides (Issue #259 阶段 2): each partial entry inherits
	// the effective global thresholds (z, after evolve) for the fields it
	// does not set. A configured type therefore never silently falls back
	// mid-flight; the assembled map is a complete snapshot per type.
	if len(c.Retrieval.ByType) > 0 {
		z.ByType = make(map[string]zone.Zones, len(c.Retrieval.ByType))
		for name, o := range c.Retrieval.ByType {
			oz := z
			if o.TauHigh != nil {
				oz.TauHigh = *o.TauHigh
			}
			if o.TauLow != nil {
				oz.TauLow = *o.TauLow
			}
			if o.AbsHigh != nil {
				oz.AbsHigh = *o.AbsHigh
			}
			if o.AbsLow != nil {
				oz.AbsLow = *o.AbsLow
			}
			oz.ByType = nil // leaf snapshot: overrides never nest
			z.ByType[name] = oz
		}
	}
	return z, nil
}

// loadEvolveTauL2 reads an evolve state dir's params.json (written by
// `usage --evolve-db`) and returns its tuned TauL2 clamped to the evolve
// tuning band [DefaultMinTau, DefaultMaxTau]. A missing file or an
// unset/non-finite value reports 0 (caller falls back to defaults); a
// corrupt file is a startup error the operator should see — same
// degradation contract as the CLI applyEvolveParams path.
func loadEvolveTauL2(dir string) (float64, error) {
	p := filepath.Join(dir, "params.json")
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("gateway: read evolve state: %w", err)
	}
	var st struct {
		Params evolve.Params `json:"params"`
	}
	if err := json.Unmarshal(b, &st); err != nil {
		return 0, fmt.Errorf("gateway: parse evolve state %s: %w", p, err)
	}
	tau := st.Params.TauL2
	if math.IsNaN(tau) || math.IsInf(tau, 0) || tau <= 0 {
		return 0, nil
	}
	if tau < evolve.DefaultMinTau {
		tau = evolve.DefaultMinTau
	}
	if tau > evolve.DefaultMaxTau {
		tau = evolve.DefaultMaxTau
	}
	return tau, nil
}

// DefaultConfig returns the built-in defaults (for tests and docs).
func DefaultConfig() *Config {
	return &Config{
		Server:    ServerConfig{Addr: ":8080", GatewayKey: "dev-key", HealthTimeoutSeconds: 3},
		Store:     StoreConfig{DB: ".semantix/gateway.jsonl", Scope: "project", DepsRoot: "."},
		Retrieval: RetrievalConfig{Retriever: "bm25", TopK: 5, Budget: 4096},
		Cache:     CacheConfig{TTLSeconds: 86400, FalseHitSim: DefaultFalseHitSim},
		Slice:     SliceConfig{MinInjectOrigin: "session-auto"},
		Ingest:    IngestConfig{SessionsDir: ".semantix/sessions", UsageLog: ".semantix/gateway-usage.jsonl"},
	}
}

// ModelList returns every model alias the gateway can route, sorted.
func (c *Config) ModelList() []string {
	set := map[string]bool{}
	for _, u := range c.Upstreams {
		for _, a := range u.ModelAlias {
			set[a] = true
		}
	}
	out := make([]string, 0, len(set))
	for m := range set {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// UpstreamFor resolves a client-visible model alias to its channel.
func (c *Config) UpstreamFor(model string) (UpstreamConfig, bool) {
	for _, u := range c.Upstreams {
		for _, a := range u.ModelAlias {
			if a == model {
				return u, true
			}
		}
	}
	return UpstreamConfig{}, false
}
