package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"semantix/kernel/inject"
)

// fixtureBody is the shared two-path fixture (#293 acceptance): an assistant
// turn carrying preserved reasoning, a reasoning_effort, and tool history.
const fixtureBody = `{
  "model": "glm",
  "reasoning_effort": "high",
  "messages": [
    {"role": "system", "content": "You are an agent."},
    {"role": "user", "content": "list files"},
    {"role": "assistant", "content": "checking", "reasoning_content": "the user wants a listing; ls is enough",
     "tool_calls": [{"type":"function","id":"c1","function":{"name":"ls","arguments":"{}"}}]},
    {"role": "tool", "tool_call_id": "c1", "content": "a.go b.go"},
    {"role": "user", "content": "and now?"}
  ]
}`

// TestAnthropicNeverForwardsReasoningEffort is the #293 acceptance line: the
// anthropic path re-marshals through a struct with no reasoning_effort
// field, so the measured hard-400 parameter cannot reach the upstream, and
// the effort arrives translated as thinking.budget_tokens instead.
func TestAnthropicNeverForwardsReasoningEffort(t *testing.T) {
	out, err := toAnthropicRequest([]byte(fixtureBody), UpstreamConfig{UpstreamModel: "glm-5.3", Vendor: "anthropic"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "reasoning_effort") {
		t.Fatalf("anthropic body leaked reasoning_effort:\n%s", out)
	}
	var req struct {
		Thinking anthropicThinking `json:"thinking"`
	}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatal(err)
	}
	if req.Thinking.Type != "enabled" || req.Thinking.BudgetTokens != effortBudgetTokens["high"] {
		t.Fatalf("thinking = %+v, want enabled/%d", req.Thinking, effortBudgetTokens["high"])
	}
}

// TestAnthropicPreservesReasoningSequence: the assistant's preserved
// reasoning survives conversion verbatim, positioned FIRST on the assistant
// turn, before its text and tool_use blocks — never reordered, never
// spliced by the injection block.
func TestAnthropicPreservesReasoningSequence(t *testing.T) {
	out, err := toAnthropicRequest([]byte(fixtureBody), UpstreamConfig{UpstreamModel: "m", Vendor: "anthropic"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var req struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type     string `json:"type"`
				Thinking string `json:"thinking"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &req); err != nil {
		t.Fatal(err)
	}
	var assistant *struct {
		Role    string `json:"role"`
		Content []struct {
			Type     string `json:"type"`
			Thinking string `json:"thinking"`
		} `json:"content"`
	}
	for i := range req.Messages {
		if req.Messages[i].Role == "assistant" {
			assistant = &req.Messages[i]
			break
		}
	}
	if assistant == nil || len(assistant.Content) < 3 {
		t.Fatalf("assistant turn missing or too few blocks: %+v", req.Messages)
	}
	if assistant.Content[0].Type != "thinking" ||
		assistant.Content[0].Thinking != "the user wants a listing; ls is enough" {
		t.Fatalf("first assistant block must be the verbatim thinking sequence: %+v", assistant.Content[0])
	}
	if assistant.Content[1].Type != "text" || assistant.Content[2].Type != "tool_use" {
		t.Fatalf("block order after thinking must stay text→tool_use: %+v", assistant.Content)
	}
}

// TestThinkingTranslationLadder covers the effort ladder, the explicit
// client thinking override, and the unknown-effort fail-open.
func TestThinkingTranslationLadder(t *testing.T) {
	for effort, want := range effortBudgetTokens {
		raw, _, err := translateThinking(effort, nil, defaultAnthropicMaxTokens, false)
		if err != nil || raw == nil {
			t.Fatalf("%s: translation failed", effort)
		}
		var th anthropicThinking
		if err := json.Unmarshal(raw, &th); err != nil {
			t.Fatal(err)
		}
		if th.Type != "enabled" || th.BudgetTokens != want {
			t.Fatalf("%s → %+v, want enabled/%d", effort, th, want)
		}
	}

	// Explicit client thinking object wins verbatim over the effort field.
	client := json.RawMessage(`{"type":"enabled","budget_tokens":1024}`)
	raw, _, err := translateThinking("high", client, defaultAnthropicMaxTokens, false)
	if err != nil || string(raw) != string(client) {
		t.Fatalf("client thinking should win verbatim: %s", raw)
	}

	// Unknown effort fails open: no parameter (logged).
	raw, _, err = translateThinking("ultra-brain", nil, defaultAnthropicMaxTokens, false)
	if err != nil || raw != nil {
		t.Fatalf("unknown effort must fail open, got %s err=%v", raw, err)
	}

	// No effort, no client object: nothing to send.
	raw, _, err = translateThinking("", nil, defaultAnthropicMaxTokens, false)
	if err != nil || raw != nil {
		t.Fatalf("empty effort must be a clean no-op, got %s err=%v", raw, err)
	}
}

// TestOpenAIPathKeepsReasoningFields: the OpenAI forwarding path must keep
// reasoning_effort AND the round-tripped assistant reasoning_content
// byte-identical — rewriteOutgoing operates on the decoded raw body and
// the sanitizer must not touch either field.
func TestOpenAIPathKeepsReasoningFields(t *testing.T) {
	g := &Gateway{cfg: &Config{}}
	out := g.rewriteOutgoing([]byte(fixtureBody), nil, UpstreamConfig{UpstreamModel: "glm-5.3"}, nil)
	var raw map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["reasoning_effort"] != "high" {
		t.Fatalf("reasoning_effort lost on the OpenAI path: %v", raw["reasoning_effort"])
	}
	msgs := raw["messages"].([]any)
	assistant := msgs[2].(map[string]any)
	if assistant["reasoning_content"] != "the user wants a listing; ls is enough" {
		t.Fatalf("reasoning_content mutated: %v", assistant["reasoning_content"])
	}
}

// TestInjectionNeverSplitsReasoning: on both paths the L2 block lands in the
// system prefix region — after the system prompt, before conversation — and
// never between an assistant's thinking and its subsequent blocks.
func TestInjectionNeverSplitsReasoning(t *testing.T) {
	block := "[semantix-reuse]\nreuse\n[/semantix-reuse]"

	// OpenAI path: block appends to the system message; assistant untouched.
	g := &Gateway{cfg: &Config{}}
	out := g.rewriteOutgoing([]byte(fixtureBody), nil, UpstreamConfig{UpstreamModel: "m"},
		&inject.Injection{Text: block})
	var raw map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatal(err)
	}
	msgs := raw["messages"].([]any)
	sys := msgs[0].(map[string]any)
	if !strings.Contains(sys["content"].(string), block) {
		t.Fatalf("OpenAI path: block not in system message: %v", sys["content"])
	}
	for i, m := range msgs[1:] {
		if c, _ := m.(map[string]any)["content"].(string); strings.Contains(c, "[semantix-reuse]") {
			t.Fatalf("OpenAI path: block leaked into message %d", i+1)
		}
	}

	// Anthropic path: block lives in the top-level system field only.
	aout, err := toAnthropicRequest([]byte(fixtureBody), UpstreamConfig{UpstreamModel: "m", Vendor: "anthropic"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var areq struct {
		System   json.RawMessage `json:"system"`
		Messages []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(aout, &areq); err != nil {
		t.Fatal(err)
	}
	for i, m := range areq.Messages {
		for _, b := range m.Content {
			if strings.Contains(b.Text, "[semantix-reuse]") {
				t.Fatalf("anthropic path: block leaked into message %d", i)
			}
		}
	}
}
