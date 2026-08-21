package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gateway.toml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const validConfig = `
[server]
addr = ":8080"
gateway_key = "${GW_KEY}"

[store]
db = "~/.semantix/gw.jsonl"
scope = "project"
deps_root = "."

[retrieval]
retriever = "bm25"
top_k = 5
budget = 4096

[cache]
ttl_seconds = 86400

[ingest]
sessions_dir = "~/.semantix/sessions"
usage_log = "~/.semantix/gw-usage.jsonl"

[[upstreams]]
name = "deepseek"
base_url = "https://api.deepseek.com/v1"
api_key = "${DS_KEY}"
model_alias = ["deepseek-chat", "ds-chat"]
upstream_model = "deepseek-chat"
vendor = "deepseek"
`

func TestLoadExpandsEnvAndHome(t *testing.T) {
	t.Setenv("GW_KEY", "k1")
	t.Setenv("DS_KEY", "k2")
	cfg, err := Load(writeConfig(t, validConfig))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.GatewayKey != "k1" || cfg.Upstreams[0].APIKey != "k2" {
		t.Errorf("env expansion failed: key=%q upkey=%q", cfg.Server.GatewayKey, cfg.Upstreams[0].APIKey)
	}
	home, _ := os.UserHomeDir()
	wantDB := filepath.Join(home, ".semantix", "gw.jsonl")
	if cfg.Store.DB != wantDB {
		t.Errorf("db = %q, want %q", cfg.Store.DB, wantDB)
	}
	if cfg.Upstreams[0].UpstreamModel != "deepseek-chat" || cfg.Upstreams[0].Vendor != "deepseek" {
		t.Errorf("upstream fields = %#v", cfg.Upstreams[0])
	}
}

func TestLoadFailsOnUnresolvedEnv(t *testing.T) {
	os.Unsetenv("GW_KEY")
	os.Unsetenv("DS_KEY")
	_, err := Load(writeConfig(t, validConfig))
	if err == nil || !strings.Contains(err.Error(), "GW_KEY") {
		t.Fatalf("want unresolved-env error naming GW_KEY, got %v", err)
	}
}

func TestLoadValidation(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
		msg  string
	}{
		{"missing addr", func(c *Config) { c.Server.Addr = "" }, "addr"},
		{"missing key", func(c *Config) { c.Server.GatewayKey = "" }, "gateway_key"},
		{"missing db", func(c *Config) { c.Store.DB = "" }, "db"},
		{"no upstreams", func(c *Config) { c.Upstreams = nil }, "upstreams"},
		{"bad scope", func(c *Config) { c.Store.Scope = "all" }, "scope"},
		{"unsupported vendor", func(c *Config) { c.Upstreams[0].Vendor = "bogus" }, "vendor"},
		{"dup alias", func(c *Config) {
			c.Upstreams = append(c.Upstreams, c.Upstreams[0])
			c.Upstreams[1].Name = "second"
		}, "alias"},
		{"missing api_key", func(c *Config) { c.Upstreams[0].APIKey = "" }, "api_key"},
		{"bad retriever", func(c *Config) { c.Retrieval.Retriever = "rrf" }, "retriever"},
		{"judge key missing base_url", func(c *Config) {
			c.Cache.JudgeAPIKey = "k"
			c.Cache.JudgeBaseURL = ""
		}, "judge_base_url"},
		{"judge key missing model", func(c *Config) {
			c.Cache.JudgeAPIKey = "k"
			c.Cache.JudgeBaseURL = "https://j/v1"
			c.Cache.JudgeModel = ""
		}, "judge_model"},
		{"bad judge protocol", func(c *Config) {
			c.Cache.JudgeAPIKey = "k"
			c.Cache.JudgeBaseURL = "https://j/v1"
			c.Cache.JudgeModel = "j"
			c.Cache.JudgeProtocol = "gemini"
		}, "judge_protocol"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := DefaultConfig()
			c.Server.GatewayKey = "k"
			c.Store.DB = "x.jsonl"
			c.Upstreams = []UpstreamConfig{{
				Name: "ds", BaseURL: "https://u/v1", APIKey: "k",
				ModelAlias: []string{"m"}, UpstreamModel: "m", Vendor: "deepseek",
			}}
			tc.mut(c)
			err := c.validate()
			if err == nil || !strings.Contains(err.Error(), tc.msg) {
				t.Fatalf("want error containing %q, got %v", tc.msg, err)
			}
		})
	}
}

func TestModelListAndUpstreamFor(t *testing.T) {
	c := DefaultConfig()
	c.Upstreams = []UpstreamConfig{
		{Name: "a", ModelAlias: []string{"b-model", "a-model"}},
		{Name: "b", ModelAlias: []string{"c-model"}},
	}
	got := c.ModelList()
	want := []string{"a-model", "b-model", "c-model"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ModelList = %v, want %v", got, want)
	}
	if u, ok := c.UpstreamFor("b-model"); !ok || u.Name != "a" {
		t.Errorf("UpstreamFor(b-model) = %#v, %v", u, ok)
	}
	if _, ok := c.UpstreamFor("nope"); ok {
		t.Error("UpstreamFor(nope) must be false")
	}
}

// TestModelAliasEnvSubstitution: ${VAR} references inside model_alias must
// expand (regression: the old code took the address of a loop copy, making
// the substitution a silent no-op).
func TestModelAliasEnvSubstitution(t *testing.T) {
	t.Setenv("GW_KEY", "k1")
	t.Setenv("DS_KEY", "k2")
	t.Setenv("ALIAS_EXTRA", "ds-chat")
	body := strings.Replace(validConfig, `model_alias = ["deepseek-chat", "ds-chat"]`,
		`model_alias = ["deepseek-chat", "${ALIAS_EXTRA}"]`, 1)
	cfg, err := Load(writeConfig(t, body))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	aliases := cfg.Upstreams[0].ModelAlias
	if len(aliases) != 2 || aliases[1] != "ds-chat" {
		t.Fatalf("model_alias env substitution failed: %v", aliases)
	}
}

// TestHealthTimeoutValidation: health_timeout_seconds must be >= 0
// (0 disables the upstream probe, negatives are rejected).
func TestHealthTimeoutValidation(t *testing.T) {
	body := strings.Replace(validConfig, "gateway_key = \"${GW_KEY}\"",
		"gateway_key = \"${GW_KEY}\"\nhealth_timeout_seconds = -1", 1)
	t.Setenv("GW_KEY", "k1")
	if _, err := Load(writeConfig(t, body)); err == nil {
		t.Error("Load accepted health_timeout_seconds = -1, want error")
	}
}

// TestDeployConfigExampleLoads (issue #183 acceptance): the shipped
// deploy/semantix-gateway.toml.example must stay parseable by the loader —
// every ${VAR} it references resolves, and the decoded values match the
// documented contract. Guards the example against config drift.
func TestDeployConfigExampleLoads(t *testing.T) {
	path := filepath.Join("..", "deploy", "semantix-gateway.toml.example")
	t.Setenv("SEMANTIX_GATEWAY_KEY", "test-channel-key")
	t.Setenv("DEEPSEEK_API_KEY", "test-upstream-key")
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load(%s): %v", path, err)
	}
	if cfg.Server.GatewayKey != "test-channel-key" {
		t.Errorf("gateway_key = %q, want env-resolved value", cfg.Server.GatewayKey)
	}
	if cfg.Server.HealthTimeoutSeconds != 3 {
		t.Errorf("health_timeout_seconds = %d, want 3", cfg.Server.HealthTimeoutSeconds)
	}
	if len(cfg.Upstreams) != 1 {
		t.Fatalf("upstreams = %d, want 1", len(cfg.Upstreams))
	}
	up := cfg.Upstreams[0]
	if up.Name != "deepseek" || up.APIKey != "test-upstream-key" || up.Vendor != "deepseek" {
		t.Errorf("upstream = %#v, want deepseek with env-resolved key", up)
	}
	if cfg.Store.DB != "/data/gateway.jsonl" || cfg.Ingest.SessionsDir != "/data/sessions" {
		t.Errorf("store/ingest paths not container-friendly: db=%q sessions=%q", cfg.Store.DB, cfg.Ingest.SessionsDir)
	}
}

// TestAnthropicVendorAccepted: vendor="anthropic" must pass validation
// (Issue #185 — the conversion layer makes it a first-class vendor).
func TestAnthropicVendorAccepted(t *testing.T) {
	c := DefaultConfig()
	c.Server.GatewayKey = "k"
	c.Store.DB = "x.jsonl"
	c.Upstreams = []UpstreamConfig{{
		Name: "claude", BaseURL: "https://api.anthropic.com/v1", APIKey: "k",
		ModelAlias: []string{"claude-sonnet"}, UpstreamModel: "claude-sonnet-4",
		Vendor: "anthropic",
	}}
	if err := c.validate(); err != nil {
		t.Fatalf("anthropic vendor must be accepted, got %v", err)
	}
}

// TestTTLForVendorDifferentiation: L3 freshness TTL is vendor-aware
// (design §3.5: DeepSeek 24h / Anthropic 5m), config vendor_ttl wins,
// generic ttl_seconds is the fallback.
func TestTTLForVendorDifferentiation(t *testing.T) {
	c := DefaultConfig() // ttl_seconds = 86400, no vendor_ttl
	if got := c.TTLFor("anthropic"); got != 5*60 {
		t.Errorf("anthropic TTL = %d, want %d (5m)", got, 5*60)
	}
	if got := c.TTLFor("deepseek"); got != 24*60*60 {
		t.Errorf("deepseek TTL = %d, want %d (24h)", got, 24*60*60)
	}
	if got := c.TTLFor("openai"); got != 86400 {
		t.Errorf("openai TTL = %d, want config default 86400", got)
	}
	// explicit vendor_ttl overrides the built-in default
	c.Cache.VendorTTL = map[string]int64{"anthropic": 60}
	if got := c.TTLFor("anthropic"); got != 60 {
		t.Errorf("anthropic TTL with vendor_ttl = %d, want 60", got)
	}
	// ttl_seconds <= 0 disables the window for vendors without a default
	c.Cache.TTLSeconds = 0
	if got := c.TTLFor("openai"); got != 0 {
		t.Errorf("openai TTL with ttl_seconds=0 = %d, want 0 (disabled)", got)
	}
}

func TestValidateAcceptsRetrieverKindsAndJudge(t *testing.T) {
	for _, kind := range []string{"bm25", "vector", "hybrid"} {
		c := DefaultConfig()
		c.Server.GatewayKey = "k"
		c.Store.DB = "x.jsonl"
		c.Retrieval.Retriever = kind
		c.Upstreams = []UpstreamConfig{{
			Name: "ds", BaseURL: "https://u/v1", APIKey: "k",
			ModelAlias: []string{"m"}, UpstreamModel: "m", Vendor: "deepseek",
		}}
		if err := c.validate(); err != nil {
			t.Fatalf("retriever %q rejected: %v", kind, err)
		}
	}
	c := DefaultConfig()
	c.Server.GatewayKey = "k"
	c.Store.DB = "x.jsonl"
	c.Cache.JudgeAPIKey = "jk"
	c.Cache.JudgeBaseURL = "https://judge/v1"
	c.Cache.JudgeModel = "judge-model"
	c.Upstreams = []UpstreamConfig{{
		Name: "ds", BaseURL: "https://u/v1", APIKey: "k",
		ModelAlias: []string{"m"}, UpstreamModel: "m", Vendor: "deepseek",
	}}
	if err := c.validate(); err != nil {
		t.Fatalf("wired judge rejected: %v", err)
	}
}
// TestLexicalFloorConfigParse: lexical_floor maps to Cache.LexicalFloor
// (Issue #260). Missing = nil (kernel default), 0 = gate disabled,
// any other value = the configured floor.
func TestLexicalFloorConfigParse(t *testing.T) {
	base := `[server]
	addr = "127.0.0.1:0"
	gateway_key = "k"
	[store]
	db = "x.jsonl"
	[retrieval]
	[ingest]
	[[upstreams]]
	name = "d"
	base_url = "https://api.deepseek.com"
	api_key = "k"
	model_alias = ["x"]
	upstream_model = "y"
	vendor = "deepseek"
	`
	load := func(floor string) *float64 {
		body := base + "[cache]" + string([]byte{10}) + "ttl_seconds = 86400" + string([]byte{10})
		if floor != "" {
			body += "lexical_floor = " + floor + string([]byte{10})
		}
		p := filepath.Join(t.TempDir(), "gw.toml")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(p)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		return cfg.Cache.LexicalFloor
	}

	if got := load(""); got != nil {
		t.Fatalf("missing lexical_floor = %v, want nil", got)
	}
	if got := load("0"); got == nil || *got != 0 {
		t.Fatalf("lexical_floor=0 = %v, want 0 (gate disabled)", got)
	}
	if got := load("0.2"); got == nil || *got != 0.2 {
		t.Fatalf("lexical_floor=0.2 = %v, want 0.2", got)
	}
}

// TestValidateEmbedderSection rejects an incomplete or invalid
// [retrieval.embedder] and accepts hash/model (Issue #275).
func TestValidateEmbedderSection(t *testing.T) {
	base := func() *Config {
		c := DefaultConfig()
		c.Server.GatewayKey = "k"
		c.Store.DB = "x.jsonl"
		c.Upstreams = []UpstreamConfig{{
			Name: "ds", BaseURL: "https://u/v1", APIKey: "k",
			ModelAlias: []string{"m"}, UpstreamModel: "m", Vendor: "deepseek",
		}}
		return c
	}

	// model without base_url/model must be rejected.
	c := base()
	c.Retrieval.Embedder = EmbedderConfig{Kind: "model", BaseURL: "https://emb/v1"}
	if err := c.validate(); err == nil {
		t.Fatal("kind=model without base_url+model must be rejected")
	}

	// unknown kind must be rejected.
	c = base()
	c.Retrieval.Embedder = EmbedderConfig{Kind: "nope"}
	if err := c.validate(); err == nil {
		t.Fatal("unknown embedder kind must be rejected")
	}

	// empty / hash accepted (default, zero-dep).
	if c := base(); c.validate() != nil {
		t.Fatalf("empty embedder rejected: %v", c.validate())
	}
	c = base()
	c.Retrieval.Embedder = EmbedderConfig{Kind: "hash"}
	if err := c.validate(); err != nil {
		t.Fatalf("kind=hash rejected: %v", err)
	}
	// model with everything present accepted.
	c = base()
	c.Retrieval.Embedder = EmbedderConfig{Kind: "model", BaseURL: "https://emb/v1", Model: "text-emb"}
	if err := c.validate(); err != nil {
		t.Fatalf("full model embedder rejected: %v", err)
	}
}
