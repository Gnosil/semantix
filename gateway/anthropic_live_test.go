package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestLiveAnthropicPromptCache is the real-upstream acceptance check for
// P0-a (docs/specs/gateway-anthropic-cache-p0a.md §验证). It is skipped
// unless SEMANTIX_LIVE_ANTHROPIC_API_KEY is set, because it spends real
// tokens (about 3 × 6k input tokens at cache prices) against an
// Anthropic-vendor upstream.
//
//	SEMANTIX_LIVE_ANTHROPIC_API_KEY=...            required
//	SEMANTIX_LIVE_ANTHROPIC_BASE_URL=...           default https://api.anthropic.com/v1
//	SEMANTIX_LIVE_ANTHROPIC_MODEL=...              default claude-sonnet-4-6
//
// The scenario is a three-request tool loop through the gateway: request 1
// writes the cache, requests 2 and 3 must read it (cache_read_input_tokens
// > 0 surfaced as prompt_tokens_details.cached_tokens), and the model must
// be able to see the tool result body (it is asked to echo a nonce that only
// appears inside the tool output).
func TestLiveAnthropicPromptCache(t *testing.T) {
	key := os.Getenv("SEMANTIX_LIVE_ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("SEMANTIX_LIVE_ANTHROPIC_API_KEY not set; live upstream check skipped")
	}
	baseURL := os.Getenv("SEMANTIX_LIVE_ANTHROPIC_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com/v1"
	}
	model := os.Getenv("SEMANTIX_LIVE_ANTHROPIC_MODEL")
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	g := newAnthropicGateway(t, baseURL)
	g.cfg.Upstreams[0].APIKey = key
	g.cfg.Upstreams[0].UpstreamModel = model
	srv := httptest.NewServer(g)
	defer srv.Close()

	// The static system prompt must exceed the model's minimum cacheable
	// prefix (512–4096 tokens depending on the model); ~6k tokens of
	// deterministic filler keeps every current model above it.
	var filler strings.Builder
	filler.WriteString("You are a terse assistant used in an automated cache test. Follow tool results exactly.\n")
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&filler, "Reference line %03d: the quick brown fox jumps over the lazy dog near the river bank.\n", i)
	}
	system := filler.String()
	const nonce = "NONCE-7f3a9c"

	turn := func(msgs []map[string]any) (usage map[string]any, content string, toolCalls []any) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"model":      "claude-sonnet",
			"stream":     false,
			"max_tokens": 64,
			"tools": []map[string]any{{
				"type": "function",
				"function": map[string]any{
					"name":        "read_note",
					"description": "Read the note and return its text.",
					"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
				},
			}},
			"messages": msgs,
		})
		resp, out := postChat(t, srv, "test-key", string(body))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, body %s", resp.StatusCode, out)
		}
		var r struct {
			Choices []struct {
				Message struct {
					Content   string `json:"content"`
					ToolCalls []any  `json:"tool_calls"`
				} `json:"message"`
			} `json:"choices"`
			Usage map[string]any `json:"usage"`
		}
		if err := json.Unmarshal(out, &r); err != nil || len(r.Choices) == 0 {
			t.Fatalf("bad completion: %v %s", err, out)
		}
		return r.Usage, r.Choices[0].Message.Content, r.Choices[0].Message.ToolCalls
	}
	cached := func(u map[string]any) float64 {
		d, _ := u["prompt_tokens_details"].(map[string]any)
		c, _ := d["cached_tokens"].(float64)
		return c
	}

	msgs := []map[string]any{
		{"role": "system", "content": system},
		{"role": "user", "content": "Call read_note, then reply with only the nonce it contains."},
	}
	u1, _, tc1 := turn(msgs)
	t.Logf("request 1 usage: %v", u1)
	if len(tc1) == 0 {
		t.Fatalf("request 1: model did not call the tool; cannot exercise the tool loop")
	}
	call, _ := tc1[0].(map[string]any)
	id, _ := call["id"].(string)
	msgs = append(msgs,
		map[string]any{"role": "assistant", "content": "", "tool_calls": tc1},
		map[string]any{"role": "tool", "tool_call_id": id, "content": "note: the nonce is " + nonce},
	)
	u2, content2, _ := turn(msgs)
	t.Logf("request 2 usage: %v content: %q", u2, content2)
	if !strings.Contains(content2, nonce) {
		t.Errorf("request 2: model did not see the tool_result body (content %q); tool_result.content mapping broken", content2)
	}
	if cached(u2) == 0 {
		t.Errorf("request 2: cached_tokens = 0; the static prefix written by request 1 was not read back")
	}
	msgs = append(msgs,
		map[string]any{"role": "assistant", "content": content2},
		map[string]any{"role": "user", "content": "Repeat the nonce once more."},
	)
	u3, content3, _ := turn(msgs)
	t.Logf("request 3 usage: %v content: %q", u3, content3)
	if cached(u3) <= cached(u2) {
		t.Errorf("request 3: cached_tokens %v should exceed request 2's %v (conversation tail breakpoint)", cached(u3), cached(u2))
	}
}
