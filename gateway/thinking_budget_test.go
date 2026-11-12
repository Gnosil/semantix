package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicThinkingBudgets(t *testing.T) {
	for _, effort := range []string{"low", "medium", "high", "max"} {
		for _, capField := range []string{"max_tokens", "max_completion_tokens"} {
			for _, limit := range []int{0, 1, 1024, 1025, 4096, 16384, 40000} {
				t.Run(fmt.Sprintf("%s/%s/%d", effort, capField, limit), func(t *testing.T) {
					capJSON := ""
					if limit != 0 {
						capJSON = fmt.Sprintf(",%q:%d", capField, limit)
					}
					body := fmt.Sprintf(`{"messages":[{"role":"user","content":"hello"}],"reasoning_effort":%q%s}`, effort, capJSON)
					raw, err := toAnthropicRequest([]byte(body), UpstreamConfig{UpstreamModel: "claude-sonnet-4-5"}, nil)
					if limit > 0 && limit <= 1024 {
						if err == nil || !strings.Contains(err.Error(), "1024") {
							t.Fatalf("want minimum-budget error, got body=%s err=%v", raw, err)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					var req struct {
						MaxTokens int               `json:"max_tokens"`
						Thinking  anthropicThinking `json:"thinking"`
					}
					if err := json.Unmarshal(raw, &req); err != nil {
						t.Fatal(err)
					}
					if req.Thinking.Type != "enabled" || req.Thinking.BudgetTokens < 1024 || req.Thinking.BudgetTokens >= req.MaxTokens {
						t.Fatalf("invalid token budgets: %+v", req)
					}
					if limit == 0 {
						wantLimit := map[string]int{"low": 5120, "medium": 8192, "high": 20480, "max": 36864}[effort]
						if req.MaxTokens != wantLimit {
							t.Fatalf("default limit = %d, want %d", req.MaxTokens, wantLimit)
						}
					}
					if limit > 0 && req.MaxTokens != limit {
						t.Fatalf("caller limit %d changed to %d", limit, req.MaxTokens)
					}
					wantBudget := effortBudgetTokens[effort]
					if limit > 0 && wantBudget >= limit {
						wantBudget = limit - 1
					}
					if req.Thinking.BudgetTokens != wantBudget {
						t.Fatalf("budget = %d, want %d", req.Thinking.BudgetTokens, wantBudget)
					}
				})
			}
		}
	}
}

func TestAnthropicExplicitThinkingBudgets(t *testing.T) {
	for _, tc := range []struct {
		name, fields, wantThinking, wantError string
		wantLimit                             int
	}{
		{"valid override", `"reasoning_effort":"high","max_tokens":2048,"thinking":{"type":"enabled","budget_tokens":1024}`, `{"type":"enabled","budget_tokens":1024}`, "", 2048},
		{"equal cap", `"max_tokens":4096,"thinking":{"type":"enabled","budget_tokens":4096}`, "", "less than the output limit", 0},
		{"over cap", `"max_completion_tokens":2048,"thinking":{"type":"enabled","budget_tokens":4096}`, "", "less than the output limit", 0},
		{"below minimum", `"thinking":{"type":"enabled","budget_tokens":512}`, "", "at least 1024", 0},
		{"missing budget", `"thinking":{"type":"enabled"}`, "", "at least 1024", 0},
		{"omitted cap, minimum budget", `"thinking":{"type":"enabled","budget_tokens":1024}`, `{"type":"enabled","budget_tokens":1024}`, "", 5120},
		{"omitted cap, middle budget", `"thinking":{"type":"enabled","budget_tokens":2048}`, `{"type":"enabled","budget_tokens":2048}`, "", 6144},
		{"omitted cap, just below default", `"thinking":{"type":"enabled","budget_tokens":4095}`, `{"type":"enabled","budget_tokens":4095}`, "", 8191},
		{"non-object thinking", `"thinking":[]`, "", "parse thinking", 0},
		{"omitted cap", `"thinking":{"type":"enabled","budget_tokens":4096}`, `{"type":"enabled","budget_tokens":4096}`, "", 8192},
		{"disabled", `"reasoning_effort":"high","max_tokens":100,"thinking":{"type":"disabled"}`, `{"type":"disabled"}`, "", 100},
		{"adaptive", `"reasoning_effort":"high","max_tokens":100,"thinking":{"type":"adaptive"}`, `{"type":"adaptive"}`, "", 100},
		{"null", `"reasoning_effort":"low","max_tokens":2048,"thinking":null`, `{"type":"enabled","budget_tokens":1024}`, "", 2048},
		{"cap precedence", `"reasoning_effort":"high","max_tokens":2048,"max_completion_tokens":8192`, `{"type":"enabled","budget_tokens":2047}`, "", 2048},
		{"no thinking", `"max_tokens":100`, "", "", 100},
		{"unknown effort", `"reasoning_effort":"unknown","max_tokens":100`, "", "", 100},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := toAnthropicRequest([]byte(`{"messages":[{"role":"user","content":"hello"}],`+tc.fields+`}`), UpstreamConfig{UpstreamModel: "claude-sonnet-4-5"}, nil)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("want error %q, got body=%s err=%v", tc.wantError, raw, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var req anthropicRequest
			if err := json.Unmarshal(raw, &req); err != nil {
				t.Fatal(err)
			}
			if string(req.Thinking) != tc.wantThinking || req.MaxTokens != tc.wantLimit {
				t.Fatalf("thinking=%s limit=%d; want %s and %d", req.Thinking, req.MaxTokens, tc.wantThinking, tc.wantLimit)
			}
		})
	}
}

func TestAnthropicThinkingRejectsSmallLimitBeforeForwarding(t *testing.T) {
	up := &testAnthropicUpstream{plain: anthropicPlainReply}
	upServer := httptest.NewServer(up.handler())
	defer upServer.Close()
	g := newAnthropicGateway(t, upServer.URL)
	server := httptest.NewServer(g)
	defer server.Close()
	response, body := postChat(t, server, "test-key", `{"model":"claude-sonnet","messages":[{"role":"user","content":"hello"}],"reasoning_effort":"low","max_tokens":1024}`)
	if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "output limit must exceed 1024") {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	if calls, _, _, _ := up.snapshot(); calls != 0 {
		t.Fatalf("invalid request reached upstream: %d calls", calls)
	}
}
