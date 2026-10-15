package gateway

import "testing"

// Fixtures follow the measured wire shapes in docs/reports/glm-spike-week.md
// §2/§3A.2/§3B (GLM-P0-3 / Issue #291).
func TestNormalizeUpstreamUsageShapes(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want normUsage
		ok   bool
	}{
		{
			// Tencent TokenHub OpenAI path: full prompt + details.cached_tokens
			// (measured: prompt 2620, cached 2560).
			name: "openai-style full prompt",
			raw:  `{"prompt_tokens":2620,"completion_tokens":15,"total_tokens":2635,"prompt_tokens_details":{"cached_tokens":2560}}`,
			want: normUsage{Prompt: 2620, Completion: 15, CacheHit: 2560},
			ok:   true,
		},
		{
			// Tencent TokenHub Anthropic path: incremental input_tokens, the
			// cache_read field appears only on a hit; full input reconciles as
			// input + cache_read (measured §2).
			name: "anthropic-style incremental hit",
			raw:  `{"input_tokens":64,"output_tokens":15,"cache_read_input_tokens":2556}`,
			want: normUsage{Prompt: 2620, Completion: 15, CacheHit: 2556},
			ok:   true,
		},
		{
			// AtomClub Anthropic path: the cache field family is always
			// present, zero-valued on a miss.
			name: "anthropic-style constant fields miss",
			raw:  `{"input_tokens":2620,"output_tokens":20,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}`,
			want: normUsage{Prompt: 2620, Completion: 20, CacheHit: 0},
			ok:   true,
		},
		{
			name: "deepseek top-level counters",
			raw:  `{"prompt_tokens":100,"completion_tokens":9,"prompt_cache_hit_tokens":64,"prompt_cache_miss_tokens":36}`,
			want: normUsage{Prompt: 100, Completion: 9, CacheHit: 64},
			ok:   true,
		},
		{
			// A semantix gateway's own bytes/4 synthesis must never be
			// re-ingested as exact accounting (cascaded gateways).
			name: "estimator marked synthesis rejected",
			raw:  `{"prompt_tokens":40,"completion_tokens":10,"estimator":"bytes/4"}`,
			ok:   false,
		},
		{
			name: "empty object rejected",
			raw:  `{}`,
			ok:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := parseUsageRaw([]byte(c.raw))
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if ok && got != c.want {
				t.Fatalf("normUsage = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestParseUsageBody(t *testing.T) {
	body := `{"id":"x","choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":2620,"completion_tokens":64,"prompt_tokens_details":{"cached_tokens":2560}}}`
	nu, ok := parseUsageBody([]byte(body))
	if !ok || nu.Prompt != 2620 || nu.CacheHit != 2560 || nu.Completion != 64 {
		t.Fatalf("parseUsageBody = %+v ok=%v", nu, ok)
	}
	if _, ok := parseUsageBody([]byte(`{"choices":[]}`)); ok {
		t.Fatal("body without usage must not parse")
	}
}

// TestNormalizeUpstreamUsageCacheWrite: cache writes survive normalization
// from both the raw Anthropic shape and the OpenAI-shape extension the
// gateway emits for translated Anthropic responses.
func TestNormalizeUpstreamUsageCacheWrite(t *testing.T) {
	nu, ok := parseUsageRaw([]byte(`{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":300,"cache_read_input_tokens":700}`))
	if !ok || nu.Prompt != 1010 || nu.CacheHit != 700 || nu.CacheWrite != 300 {
		t.Errorf("anthropic shape: %+v ok=%v", nu, ok)
	}
	nu, ok = parseUsageRaw([]byte(`{"prompt_tokens":1010,"completion_tokens":5,"prompt_tokens_details":{"cached_tokens":700,"cache_creation_tokens":300}}`))
	if !ok || nu.CacheHit != 700 || nu.CacheWrite != 300 {
		t.Errorf("openai-shape extension: %+v ok=%v", nu, ok)
	}
	nu, ok = parseUsageRaw([]byte(`{"prompt_tokens":100,"completion_tokens":5,"prompt_cache_hit_tokens":60,"prompt_cache_miss_tokens":40}`))
	if !ok || nu.CacheWrite != 0 {
		t.Errorf("deepseek shape reports no writes: %+v ok=%v", nu, ok)
	}
}

// TestAnthropicResponseCarriesCacheWrite: the translated non-streaming
// response exposes cache writes, and the gateway's own parser reads them back.
func TestAnthropicResponseCarriesCacheWrite(t *testing.T) {
	body := []byte(`{"id":"m","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn",
		"usage":{"input_tokens":10,"output_tokens":3,"cache_creation_input_tokens":400,"cache_read_input_tokens":0}}`)
	out, err := anthropicToOpenAIResponse(body, "m")
	if err != nil {
		t.Fatal(err)
	}
	nu, ok := parseUsageBody(out)
	if !ok || nu.Prompt != 410 || nu.CacheWrite != 400 || nu.CacheHit != 0 {
		t.Errorf("round trip: %+v ok=%v body=%s", nu, ok, out)
	}
}
