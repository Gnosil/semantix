package gateway

import (
	"encoding/json"
	"strings"
	"testing"

	"semantix/kernel/inject"
	"semantix/kernel/slice"
)

// ---------------------------------------------------------------------------
// request translation (OpenAI chat/completions -> Anthropic /v1/messages)

func anthropicUp() UpstreamConfig {
	return UpstreamConfig{
		Name: "claude", BaseURL: "https://api.anthropic.com/v1",
		APIKey: "up-key", ModelAlias: []string{"claude-sonnet"},
		UpstreamModel: "claude-sonnet-4", Vendor: "anthropic",
	}
}

func TestToAnthropicRequestSystemLift(t *testing.T) {
	body := `{"model":"claude-sonnet","stream":false,
		"messages":[
			{"role":"system","content":"you are helpful"},
			{"role":"user","content":"hello"},
			{"role":"assistant","content":"hi there"},
			{"role":"user","content":"tell me about widgets"}
		]}`
	raw, err := toAnthropicRequest([]byte(body), anthropicUp(), nil)
	if err != nil {
		t.Fatalf("toAnthropicRequest: %v", err)
	}
	var req anthropicRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.Model != "claude-sonnet-4" {
		t.Errorf("model = %q, want upstream model claude-sonnet-4", req.Model)
	}
	if req.MaxTokens != defaultAnthropicMaxTokens {
		t.Errorf("max_tokens = %d, want default %d (Anthropic requires it)", req.MaxTokens, defaultAnthropicMaxTokens)
	}
	// P0-a: the lifted system prompt carries the static-prefix breakpoint
	// (BP1), so it is rendered as a one-element block array.
	var sys []anthropicBlock
	if err := json.Unmarshal(req.System, &sys); err != nil || len(sys) != 1 || sys[0].Text != "you are helpful" {
		t.Errorf("system = %s, want one lifted text block (err %v)", req.System, err)
	} else if sys[0].CacheControl == nil {
		t.Errorf("lifted system block missing BP1 cache_control: %s", req.System)
	}
	if len(req.Messages) != 3 {
		t.Fatalf("messages = %d, want 3 (system lifted out)", len(req.Messages))
	}
	if req.Messages[0].Role != "user" || req.Messages[0].Content[0].Text != "hello" {
		t.Errorf("messages[0] = %#v", req.Messages[0])
	}
	if req.Messages[1].Role != "assistant" || req.Messages[1].Content[0].Text != "hi there" {
		t.Errorf("messages[1] = %#v", req.Messages[1])
	}
}

func TestToAnthropicRequestToolCalls(t *testing.T) {
	body := `{"model":"claude-sonnet","stream":false,
		"tools":[{"type":"function","function":{"name":"read_file","description":"read","parameters":{"type":"object","properties":{"path":{"type":"string"}}}}}],
		"tool_choice":"auto",
		"messages":[
			{"role":"user","content":"read the file"},
			{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"/a\"}"}}]},
			{"role":"tool","tool_call_id":"call_1","name":"read_file","content":"file contents"}
		]}`
	raw, err := toAnthropicRequest([]byte(body), anthropicUp(), nil)
	if err != nil {
		t.Fatalf("toAnthropicRequest: %v", err)
	}
	var req anthropicRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(req.Tools) != 1 || req.Tools[0].Name != "read_file" {
		t.Fatalf("tools = %#v", req.Tools)
	}
	if req.ToolChoice != "auto" {
		t.Errorf("tool_choice = %v, want auto", req.ToolChoice)
	}
	// user, assistant(tool_use), user(tool_result) — tool result rides on a
	// user message, adjacent user messages merged
	if len(req.Messages) != 3 {
		t.Fatalf("messages = %d, want 3, got %#v", len(req.Messages), req.Messages)
	}
	assistant := req.Messages[1]
	if assistant.Role != "assistant" || len(assistant.Content) != 1 || assistant.Content[0].Type != "tool_use" {
		t.Fatalf("assistant message = %#v, want one tool_use block", assistant)
	}
	tu := assistant.Content[0]
	if tu.ID != "call_1" || tu.Name != "read_file" {
		t.Errorf("tool_use = %#v", tu)
	}
	input, _ := json.Marshal(tu.Input)
	if string(input) != `{"path":"/a"}` {
		t.Errorf("tool_use.input = %s, want parsed arguments object", input)
	}
	toolResult := req.Messages[2]
	if toolResult.Role != "user" || len(toolResult.Content) != 1 {
		t.Fatalf("tool result message = %#v", toolResult)
	}
	tr := toolResult.Content[0]
	if tr.Type != "tool_result" || tr.ToolUseID != "call_1" || tr.Content != "file contents" || tr.Input != nil {
		t.Errorf("tool_result = %#v", tr)
	}
	// Wire-level check (P0-a §1): the Messages API tool_result block has
	// tool_use_id / content, never "input" — the pre-P0-a adapter shipped
	// the output under "input", which the struct-only assertion above
	// could not catch.
	var wire struct {
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal wire: %v", err)
	}
	wtr := wire.Messages[2].Content[0]
	if wtr["content"] != "file contents" {
		t.Errorf("wire tool_result.content = %#v, want the tool output", wtr["content"])
	}
	if _, has := wtr["input"]; has {
		t.Errorf("wire tool_result must not carry an input key: %#v", wtr)
	}
	wtu := wire.Messages[1].Content[0]
	if _, has := wtu["input"]; !has {
		t.Errorf("wire tool_use must keep its input key: %#v", wtu)
	}
	if _, has := wtu["content"]; has {
		t.Errorf("wire tool_use must not carry a content key: %#v", wtu)
	}
}

func TestToAnthropicRequestCacheControlBreakpoints(t *testing.T) {
	body := `{"model":"claude-sonnet","stream":false,
		"messages":[
			{"role":"system","content":"base sys"},
			{"role":"user","content":"query about widgets"}
		]}`
	inj := &inject.Injection{
		Text:   "[semantix-reuse]\nprior knowledge\n[/semantix-reuse]",
		Slices: []*slice.Slice{{ID: "s1", Type: slice.Prompt, Scope: slice.Project, Content: []byte("prior knowledge")}},
	}
	raw, err := toAnthropicRequest([]byte(body), anthropicUp(), inj)
	if err != nil {
		t.Fatalf("toAnthropicRequest: %v", err)
	}
	var req anthropicRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// P0-a §3: system is a two-block array — the static prompt with BP1,
	// then the L2 block WITHOUT a marker, so a block that differs from the
	// previous request invalidates only itself, never tools + static system.
	var sys []anthropicBlock
	if err := json.Unmarshal(req.System, &sys); err != nil || len(sys) != 2 {
		t.Fatalf("system = %s, want [static, l2] block array (unmarshal err %v)", req.System, err)
	}
	if sys[0].Text != "base sys" || sys[0].CacheControl == nil || sys[0].CacheControl.Type != "ephemeral" {
		t.Errorf("static system block must carry BP1: %#v", sys[0])
	}
	if !strings.Contains(sys[1].Text, "[semantix-reuse]") || sys[1].CacheControl != nil {
		t.Errorf("L2 block must follow BP1 without a marker: %#v", sys[1])
	}
	// final message tail gets BP2 (≤2 total)
	if len(req.Messages) == 0 {
		t.Fatal("no messages")
	}
	last := req.Messages[len(req.Messages)-1]
	found := false
	for _, b := range last.Content {
		if b.Type == "text" && b.CacheControl != nil {
			found = true
		}
	}
	if !found {
		t.Errorf("final message tail missing cache_control breakpoint: %#v", last)
	}
	if n := strings.Count(string(raw), `"cache_control"`); n != 2 {
		t.Errorf("cache_control markers = %d, want exactly 2 (BP1 + BP2)", n)
	}
}

// TestToAnthropicRequestNoBlockDefaultStillCaches: P0-a §3 — a request
// without an L2 block still gets its conversation-tail breakpoint under the
// default "always" policy (pre-P0-a such requests carried no cache_control
// at all and were never cached).
func TestToAnthropicRequestNoBlockDefaultStillCaches(t *testing.T) {
	body := `{"model":"claude-sonnet","messages":[{"role":"user","content":"hi"}]}`
	raw, err := toAnthropicRequest([]byte(body), anthropicUp(), nil)
	if err != nil {
		t.Fatalf("toAnthropicRequest: %v", err)
	}
	if n := strings.Count(string(raw), `"cache_control"`); n != 1 {
		t.Errorf("cache_control markers = %d, want 1 (BP2 only: no system, no tools): %s", n, raw)
	}
	if strings.Contains(string(raw), `"system"`) {
		t.Errorf("no system field expected without a system prompt or block: %s", raw)
	}
}

// TestToAnthropicRequestL2OnlyPolicy: cache_breakpoints = "l2_only" restores
// the pre-P0-a trigger (markers only when a block was injected) while
// keeping the new placement.
func TestToAnthropicRequestL2OnlyPolicy(t *testing.T) {
	up := anthropicUp()
	up.CacheBreakpoints = "l2_only"
	body := `{"model":"claude-sonnet","messages":[{"role":"system","content":"base sys"},{"role":"user","content":"hi"}]}`

	raw, err := toAnthropicRequest([]byte(body), up, nil)
	if err != nil {
		t.Fatalf("toAnthropicRequest: %v", err)
	}
	if strings.Contains(string(raw), "cache_control") {
		t.Errorf("l2_only without a block must emit no markers: %s", raw)
	}
	var req anthropicRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	if string(req.System) != `"base sys"` {
		t.Errorf("system without markers or block must stay a plain string (byte-identical to pre-P0-a): %s", req.System)
	}

	inj := &inject.Injection{
		Text:   "[semantix-reuse]\nprior knowledge\n[/semantix-reuse]",
		Slices: []*slice.Slice{{ID: "s1", Type: slice.Prompt, Scope: slice.Project, Content: []byte("prior knowledge")}},
	}
	raw, err = toAnthropicRequest([]byte(body), up, inj)
	if err != nil {
		t.Fatalf("toAnthropicRequest: %v", err)
	}
	if n := strings.Count(string(raw), `"cache_control"`); n != 2 {
		t.Errorf("l2_only with a block: markers = %d, want 2: %s", n, raw)
	}
}

func TestToAnthropicRequestImageAndAdjacentMerge(t *testing.T) {
	body := `{"model":"claude-sonnet",
		"messages":[
			{"role":"user","content":"first turn"},
			{"role":"user","content":[{"type":"text","text":"second turn"},{"type":"image_url","image_url":{"url":"https://x/y.png"}}]}
		]}`
	raw, err := toAnthropicRequest([]byte(body), anthropicUp(), nil)
	if err != nil {
		t.Fatalf("toAnthropicRequest: %v", err)
	}
	var req anthropicRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// both user turns merge into one message (Anthropic alternation rule)
	if len(req.Messages) != 1 {
		t.Fatalf("messages = %d, want 1 (adjacent users merged): %#v", len(req.Messages), req.Messages)
	}
	blocks := req.Messages[0].Content
	if len(blocks) != 3 {
		t.Fatalf("blocks = %d, want 3 (two text + one image): %#v", len(blocks), blocks)
	}
	if blocks[0].Text != "first turn" || blocks[1].Text != "second turn" {
		t.Errorf("text blocks = %q %q", blocks[0].Text, blocks[1].Text)
	}
	if blocks[2].Type != "image" || blocks[2].Source == nil || blocks[2].Source.Type != "url" {
		t.Errorf("image block = %#v", blocks[2])
	}
}

// TestToAnthropicRequestTemperatureTopPExclusive: Anthropic rejects requests
// that set both temperature and top_p; the adapter must drop top_p when
// temperature is present (OpenAI clients routinely send both).
func TestToAnthropicRequestTemperatureTopPExclusive(t *testing.T) {
	body := `{"model":"claude-sonnet","temperature":0.7,"top_p":0.9,
		"messages":[{"role":"user","content":"hi"}]}`
	raw, err := toAnthropicRequest([]byte(body), anthropicUp(), nil)
	if err != nil {
		t.Fatalf("toAnthropicRequest: %v", err)
	}
	var req anthropicRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.Temperature == nil || *req.Temperature != 0.7 {
		t.Errorf("temperature = %v, want 0.7", req.Temperature)
	}
	if req.TopP != nil {
		t.Errorf("top_p = %v, want nil (must be dropped alongside temperature)", req.TopP)
	}
	// without temperature, top_p passes through
	body = `{"model":"claude-sonnet","top_p":0.9,"messages":[{"role":"user","content":"hi"}]}`
	raw, _ = toAnthropicRequest([]byte(body), anthropicUp(), nil)
	req = anthropicRequest{}
	_ = json.Unmarshal(raw, &req)
	if req.TopP == nil || *req.TopP != 0.9 {
		t.Errorf("top_p alone = %v, want 0.9", req.TopP)
	}
}

// ---------------------------------------------------------------------------
// response translation (Anthropic /v1/messages -> OpenAI chat.completion)

func TestAnthropicToOpenAIResponse(t *testing.T) {
	body := `{"id":"msg_01","type":"message","role":"assistant","model":"claude-sonnet-4",
		"content":[{"type":"text","text":"the answer"}],
		"stop_reason":"end_turn",
		"usage":{"input_tokens":100,"output_tokens":25,"cache_read_input_tokens":60}}`
	raw, err := anthropicToOpenAIResponse([]byte(body), "claude-sonnet")
	if err != nil {
		t.Fatalf("anthropicToOpenAIResponse: %v", err)
	}
	var out struct {
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
			PromptDetails    struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Object != "chat.completion" || out.Model != "claude-sonnet" {
		t.Errorf("object/model = %q/%q", out.Object, out.Model)
	}
	if out.Choices[0].Message.Content != "the answer" || out.Choices[0].FinishReason != "stop" {
		t.Errorf("choice = %#v (end_turn must map to stop)", out.Choices[0])
	}
	// Anthropic input_tokens is incremental (excludes cache reads/writes):
	// OpenAI prompt_tokens must carry the reconciled full input, or every
	// cache hit under-reports by its cached portion (#291; measured
	// cache_read + input = full input, glm-spike-week.md §2).
	if out.Usage.PromptTokens != 160 || out.Usage.CompletionTokens != 25 || out.Usage.TotalTokens != 185 {
		t.Errorf("usage = %#v (prompt must be input+cache_read reconciled)", out.Usage)
	}
	if out.Usage.PromptDetails.CachedTokens != 60 {
		t.Errorf("cached_tokens = %d, want 60 (cache_read_input_tokens)", out.Usage.PromptDetails.CachedTokens)
	}
}

func TestAnthropicToOpenAIResponseToolUse(t *testing.T) {
	body := `{"id":"msg_02","type":"message","role":"assistant","model":"m",
		"content":[{"type":"tool_use","id":"toolu_1","name":"read_file","input":{"path":"/a"}}],
		"stop_reason":"tool_use",
		"usage":{"input_tokens":10,"output_tokens":5}}`
	raw, err := anthropicToOpenAIResponse([]byte(body), "m")
	if err != nil {
		t.Fatalf("anthropicToOpenAIResponse: %v", err)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("finish_reason = %q, want tool_calls", out.Choices[0].FinishReason)
	}
	tc := out.Choices[0].Message.ToolCalls
	if len(tc) != 1 || tc[0].ID != "toolu_1" || tc[0].Function.Name != "read_file" {
		t.Fatalf("tool_calls = %#v", tc)
	}
	if tc[0].Function.Arguments != `{"path":"/a"}` {
		t.Errorf("arguments = %q, want marshaled input", tc[0].Function.Arguments)
	}
}

func TestAnthropicStopReasonMapping(t *testing.T) {
	cases := map[string]string{
		"":              "stop",
		"end_turn":      "stop",
		"stop_sequence": "stop",
		"max_tokens":    "length",
		"tool_use":      "tool_calls",
		"weird":         "stop",
	}
	for in, want := range cases {
		if got := anthropicStopReason(in); got != want {
			t.Errorf("anthropicStopReason(%q) = %q, want %q", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// streaming translation (Anthropic SSE -> OpenAI SSE)

func TestAnthropicSSEConverter(t *testing.T) {
	var out strings.Builder
	conv := newAnthropicSSEConverter(&out, nil)
	events := []string{
		`event: message_start
data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"m"}}`,
		`event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}`,
		`event: content_block_stop
data: {"type":"content_block_stop","index":0}`,
		`event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"read_file","input":{}}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
		`event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"/a\"}"}}`,
		`event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"output_tokens":5}}`,
		`event: message_stop
data: {"type":"message_stop"}`,
	}
	done := false
	for _, e := range events {
		lines := strings.Split(e, "\n")
		for _, l := range lines {
			if conv.feed([]byte(l)) {
				done = true
			}
		}
	}
	if !done {
		t.Error("converter did not finish on message_stop")
	}
	got := out.String()
	for _, want := range []string{
		`"id":"msg_1"`,                 // stable id captured from message_start
		`"model":"m"`,                  // model echoed on every chunk
		`"role":"assistant"`,           // first chunk carries the role
		`"content":"Hel"`,              // text deltas mapped verbatim
		`"content":"lo"`,               // text delta mapped verbatim (lowercase)
		`"id":"toolu_1"`,               // tool_use start -> tool_calls chunk
		`"arguments":"{\"path\":`,      // input_json deltas streamed
		`"finish_reason":"tool_calls"`, // stop_reason mapped
		"data: [DONE]",                 // message_stop terminator
	} {
		if !strings.Contains(got, want) {
			t.Errorf("stream missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "message_start") || strings.Contains(got, "ping") {
		t.Errorf("raw anthropic events leaked through:\n%s", got)
	}
}

// TestAnthropicSSEConverterUsage covers the #291 harvest: message_start
// carries input/cache counters (Tencent-style: cache_read only on a hit,
// input_tokens incremental), message_delta carries output_tokens; the
// converter reconciles to the full-prompt convention and emits one
// translated usage chunk before [DONE].
func TestAnthropicSSEConverterUsage(t *testing.T) {
	var out strings.Builder
	conv := newAnthropicSSEConverter(&out, nil)
	frames := []string{
		`data: {"type":"message_start","message":{"id":"msg_u","model":"glm-5.3","usage":{"input_tokens":64,"cache_read_input_tokens":2556}}}`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":15}}`,
		`data: {"type":"message_stop"}`,
	}
	for _, f := range frames {
		conv.feed([]byte(f))
	}
	nu, ok := conv.usage()
	if !ok {
		t.Fatal("converter harvested no usage")
	}
	if nu.Prompt != 2620 || nu.CacheHit != 2556 || nu.Completion != 15 {
		t.Fatalf("usage = %+v (want reconciled prompt 2620, hit 2556, out 15)", nu)
	}
	got := out.String()
	usageIdx := strings.Index(got, `"prompt_tokens":2620`)
	doneIdx := strings.LastIndex(got, "data: [DONE]")
	if usageIdx < 0 {
		t.Fatalf("no translated usage chunk in stream:\n%s", got)
	}
	if !strings.Contains(got, `"cached_tokens":2556`) {
		t.Errorf("usage chunk missing cached_tokens:\n%s", got)
	}
	if doneIdx >= 0 && usageIdx > doneIdx {
		t.Errorf("usage chunk must precede [DONE]:\n%s", got)
	}
}

// TestAnthropicSSEConverterMidStreamError: an Anthropic error event must
// surface as an OpenAI error envelope — never as a clean completion.
func TestAnthropicSSEConverterMidStreamError(t *testing.T) {
	var out strings.Builder
	conv := newAnthropicSSEConverter(&out, nil)
	conv.feed([]byte(`data: {"type":"message_start","message":{"id":"m1"}}`))
	conv.feed([]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`))
	done := conv.feed([]byte(`data: {"type":"error","error":{"type":"overloaded_error","message":"upstream blew up"}}`))
	if !done {
		t.Error("error event must terminate the stream")
	}
	conv.finish() // must be a no-op after error
	got := out.String()
	if !strings.Contains(got, `"error"`) || !strings.Contains(got, "upstream blew up") {
		t.Errorf("error not surfaced as OpenAI envelope: %s", got)
	}
	if !strings.Contains(got, "data: [DONE]") {
		t.Errorf("error path missing [DONE]: %s", got)
	}
	if strings.Contains(got, `"finish_reason":"`) {
		t.Errorf("error path must not emit a real finish_reason (clean completion): %s", got)
	}
}

// TestAnthropicSSEConverterFinishKeepsRealReason: when message_delta already
// emitted the real finish_reason, an abnormal end must not clobber it.
func TestAnthropicSSEConverterFinishKeepsRealReason(t *testing.T) {
	var out strings.Builder
	conv := newAnthropicSSEConverter(&out, nil)
	conv.feed([]byte(`data: {"type":"message_delta","delta":{"stop_reason":"max_tokens"}}`))
	conv.finish()
	got := out.String()
	if n := strings.Count(got, "finish_reason"); n != 1 {
		t.Errorf("finish_reason emitted %d times, want 1: %s", n, got)
	}
	if !strings.Contains(got, `"finish_reason":"length"`) {
		t.Errorf("real finish_reason lost: %s", got)
	}
	if !strings.Contains(got, "data: [DONE]") {
		t.Errorf("missing [DONE]: %s", got)
	}
}

func TestAnthropicSSEConverterAbnormalEnd(t *testing.T) {
	var out strings.Builder
	conv := newAnthropicSSEConverter(&out, nil)
	conv.feed([]byte(`data: {"type":"message_start","message":{}}`))
	conv.feed([]byte(`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}`))
	conv.finish() // upstream disconnected without message_stop
	got := out.String()
	if !strings.Contains(got, "partial") || !strings.Contains(got, "data: [DONE]") {
		t.Errorf("abnormal end must flush content + [DONE], got: %s", got)
	}
}
