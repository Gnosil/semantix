// Package gateway implements the Semantix Gateway v1 (Issue #133): an
// OpenAI-compatible HTTP gateway that sits between New API and upstream
// LLMs, running every request through the kernel three-layer cache before
// forwarding. Zero new core logic — it wires kernel/cache, kernel/inject,
// kernel/fingerprint, kernel/slice, kernel/ingest and kernel/usage.
//
// Design: docs/specs/newapi-gateway-design.md.
package gateway

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config mirrors semantix-gateway.toml (design doc §3.9).
type Config struct {
	Server    ServerConfig     `toml:"server"`
	Store     StoreConfig      `toml:"store"`
	Retrieval RetrievalConfig  `toml:"retrieval"`
	Cache     CacheConfig      `toml:"cache"`
	Ingest    IngestConfig     `toml:"ingest"`
	Upstreams []UpstreamConfig `toml:"upstreams"`
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
}

// RetrievalConfig tunes the L2 injector.
type RetrievalConfig struct {
	Retriever string `toml:"retriever"` // bm25 | vector | hybrid (Issue #186); vector/hybrid use kernel/embed HashEmbedder
	TopK      int    `toml:"top_k"`
	Budget    int    `toml:"budget"`
	VectorDim int    `toml:"vector_dim"` // HashEmbedder dimension (<=0 -> 256)
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
}

// IngestConfig controls the session-sidecar write path.
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
	if c.Cache.JudgeAPIKey != "" && (strings.TrimSpace(c.Cache.JudgeBaseURL) == "" || strings.TrimSpace(c.Cache.JudgeModel) == "") {
		return fmt.Errorf("gateway config: [cache] judge_api_key requires judge_base_url and judge_model")
	}
	if c.Cache.JudgeProtocol != "" && c.Cache.JudgeProtocol != "openai" && c.Cache.JudgeProtocol != "anthropic" {
		return fmt.Errorf("gateway config: [cache] judge_protocol %q must be openai or anthropic", c.Cache.JudgeProtocol)
	}
	if c.Cache.TTLSeconds < 0 {
		return fmt.Errorf("gateway config: [cache] ttl_seconds must be >= 0 (0 disables the time window)")
	}
	if c.Server.HealthTimeoutSeconds < 0 {
		return fmt.Errorf("gateway config: [server] health_timeout_seconds must be >= 0 (0 disables the upstream probe)")
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

func validRetriever(s string) bool {
	switch s {
	case "bm25", "vector", "hybrid":
		return true
	}
	return false
}

// DefaultConfig returns the built-in defaults (for tests and docs).
func DefaultConfig() *Config {
	return &Config{
		Server:    ServerConfig{Addr: ":8080", GatewayKey: "dev-key", HealthTimeoutSeconds: 3},
		Store:     StoreConfig{DB: ".semantix/gateway.jsonl", Scope: "project", DepsRoot: "."},
		Retrieval: RetrievalConfig{Retriever: "bm25", TopK: 5, Budget: 4096},
		Cache:     CacheConfig{TTLSeconds: 86400},
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
