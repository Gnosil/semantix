package gateway

// Anthropic / Claude vendor adapter (Issue #185, gateway M2 upper half).
//
// The gateway's client-facing surface stays OpenAI-compatible (New API
// speaks OpenAI); this file only translates on the upstream hop for
// upstreams declared vendor="anthropic". It deliberately lives in its own
// file and touches nothing on the OpenAI passthrough path.
//
// Translation rules (design: docs/specs/newapi-gateway-design.md §0.5):
//   request   chat/completions  ->  POST {base_url}/v1/messages
//             system            ->  top-level "system" (block array when a
//                                   cache_control breakpoint or an L2 block
//                                   is present; see applyCacheBreakpoints)
//             assistant tool_calls -> content block {type:"tool_use"}
//             role=tool         ->  user content block {type:"tool_result"}
//             adjacent same-role messages merged (Anthropic alternation rule)
//             max_tokens        ->  required upstream; defaulted when absent
//   response  messages          ->  chat.completion (usage mapped, see
//                                   anthropicToOpenAIResponse)
//   stream    Anthropic SSE     ->  OpenAI SSE (see anthropicSSEConverter)

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"semantix/kernel/cache"
	"semantix/kernel/inject"
	"semantix/kernel/usage"
)

// anthropicVersion is the API version pinned for the Anthropic messages
// endpoint (design §3.8: x-api-key + anthropic-version headers).
const anthropicVersion = "2023-06-01"

// defaultAnthropicMaxTokens is the max_tokens sent when the OpenAI request
// carries none: Anthropic requires the field, OpenAI clients often omit it.
const defaultAnthropicMaxTokens = 4096

// openAITool is the OpenAI tools[] entry the adapter maps to Anthropic.
type openAITool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
}

// anthropicMessage is one messages[] entry of the Anthropic request.
type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []anthropicBlock `json:"content"`
}

// anthropicBlock is a content block: text / tool_use / tool_result / image.
type anthropicBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// thinking (GLM-P0-5 #293): a preserved reasoning sequence round-tripped
	// from the client's assistant reasoning_content. Must precede text/tool
	// blocks and must never be reordered or rewritten — GLM endpoints degrade
	// output and cache hits otherwise (spec §3.3).
	Thinking string `json:"thinking,omitempty"`
	// tool_use
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
	// tool_use.input — the parsed function arguments. Never used for
	// tool_result: the Messages API has no "input" key on that block.
	Input any `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	// tool_result.content — the tool output (string or block array). The
	// pre-P0-a adapter shipped this under "input", which the Messages API
	// schema does not define for tool_result (spec:
	// docs/specs/gateway-anthropic-cache-p0a.md §1).
	Content any `json:"content,omitempty"`
	// image source (url or base64)
	Source *anthropicImageSource `json:"source,omitempty"`
	// L1 prompt-cache breakpoint. Placement policy: docs/specs/
	// gateway-anthropic-cache-p0a.md §3 (BP1 static-system tail, BP2 last
	// cacheable block of the final message).
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicImageSource struct {
	Type      string `json:"type"` // "url" | "base64"
	URL       string `json:"url,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

type anthropicCacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"`
	// CacheControl carries the BP1 fallback when the request has no system
	// prompt: tools render before system in the cache prefix, so the last
	// tool is the latest static position (spec P0-a §3, BP1′).
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

// anthropicRequest mirrors the /v1/messages request body the adapter emits.
type anthropicRequest struct {
	Model         string             `json:"model"`
	MaxTokens     int                `json:"max_tokens"`
	Messages      []anthropicMessage `json:"messages"`
	System        json.RawMessage    `json:"system,omitempty"` // string or block array
	Stream        bool               `json:"stream,omitempty"`
	Temperature   *float64           `json:"temperature,omitempty"`
	TopP          *float64           `json:"top_p,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
	Tools         []anthropicTool    `json:"tools,omitempty"`
	ToolChoice    any                `json:"tool_choice,omitempty"`
	// Thinking carries the translated effort parameter (#293). This struct
	// deliberately has NO reasoning_effort field: re-marshaling through it is
	// what guarantees the OpenAI-only parameter can never reach an
	// anthropic-vendor upstream.
	Thinking json.RawMessage `json:"thinking,omitempty"`
}

// openAIChatBody is the full OpenAI chat request body (a superset of
// chatRequest: it also keeps the fields the Anthropic adapter maps).
type openAIChatBody struct {
	chatRequest
	MaxTokens        int          `json:"max_tokens"`
	MaxCompletionTok int          `json:"max_completion_tokens"`
	TopP             *float64     `json:"top_p"`
	Stop             any          `json:"stop"`
	Tools            []openAITool `json:"tools"`
	ToolChoice       any          `json:"tool_choice"`
	// Thinking-effort fields (#293): translated per path, never forwarded
	// verbatim to an anthropic-vendor upstream (measured hard 400).
	ReasoningEffort string          `json:"reasoning_effort"`
	Thinking        json.RawMessage `json:"thinking"`
}

// toAnthropicRequest converts an OpenAI chat/completions request body into
// an Anthropic /v1/messages request body for the given upstream, rendering
// the L2 injection block (when one was built) as its own system block and
// placing prompt-cache breakpoints per the upstream's cache_breakpoints
// policy. The body is expected to have passed sanitizeBody already.
// Returns the marshaled Anthropic body (nil on conversion error).
func toAnthropicRequest(body []byte, up UpstreamConfig, inj *inject.Injection) ([]byte, error) {
	var openAI openAIChatBody
	if err := json.Unmarshal(body, &openAI); err != nil {
		return nil, fmt.Errorf("parse OpenAI body: %w", err)
	}
	if len(openAI.Messages) == 0 {
		return nil, fmt.Errorf("messages must not be empty")
	}

	system, msgs := splitSystem(openAI.Messages)
	// Only a real injection (retrieved slices, not the empty marker block)
	// is rendered; an empty marker is meaningless.
	hasBlock := inj != nil && len(inj.Slices) > 0

	out := anthropicRequest{
		Model:     up.UpstreamModel,
		MaxTokens: defaultAnthropicMaxTokens,
		Messages:  msgs,
		Stream:    openAI.Stream,
	}
	if openAI.MaxTokens > 0 {
		out.MaxTokens = openAI.MaxTokens
	} else if openAI.MaxCompletionTok > 0 {
		out.MaxTokens = openAI.MaxCompletionTok
	}
	out.Temperature = openAI.Temperature
	// Anthropic rejects requests that set both temperature and top_p; OpenAI
	// clients routinely send both, so drop top_p whenever temperature is set.
	if openAI.Temperature == nil {
		out.TopP = openAI.TopP
	}
	if seqs, ok := toStopSequences(openAI.Stop); ok {
		out.StopSequences = seqs
	}
	out.Tools = mapTools(openAI.Tools)
	out.ToolChoice = mapToolChoice(openAI.ToolChoice, len(openAI.Tools))
	// Per-path thinking translation (#293): reasoning_effort maps onto
	// thinking budgets; an explicit client thinking object wins; an unknown
	// effort fails open to the endpoint default (no parameter at all).
	if thinking, _ := translateThinking(openAI.ReasoningEffort, openAI.Thinking); thinking != nil {
		out.Thinking = thinking
	}

	applyCacheBreakpoints(&out, system, inj, hasBlock, up.cacheBreakpointMode())

	raw, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}
	return raw, nil
}

// splitSystem lifts every system message out of the messages array into the
// top-level system string (Anthropic has no system role) and converts the
// remaining messages to Anthropic shape, merging adjacent same-role messages
// (Anthropic requires user/assistant alternation).
func splitSystem(messages []chatMessage) (system string, out []anthropicMessage) {
	var sys []string
	for _, m := range messages {
		if m.Role == "system" {
			if t := textParts(m.Content); t != "" {
				sys = append(sys, t)
			}
			continue
		}
		var blocks []anthropicBlock
		switch m.Role {
		case "user":
			blocks = userBlocks(m.Content)
		case "assistant":
			blocks = assistantBlocks(m.Content, m.ToolCalls)
			// Preserved thinking rides FIRST on the assistant turn, verbatim
			// (#293): GLM Anthropic-path endpoints require the reasoning
			// sequence complete, unmodified and unreordered — dropping it
			// here (the old behavior) degraded output and cache hits.
			if m.ReasoningContent != "" {
				blocks = append([]anthropicBlock{{Type: "thinking", Thinking: m.ReasoningContent}}, blocks...)
			}
		case "tool":
			tr := anthropicBlock{Type: "tool_result", ToolUseID: m.ToolCallID, Content: textParts(m.Content)}
			// tool results must ride on a user message that follows the
			// tool_use; merge consecutive results into one user message.
			if len(out) > 0 && out[len(out)-1].Role == "user" {
				out[len(out)-1].Content = append(out[len(out)-1].Content, tr)
				continue
			}
			out = append(out, anthropicMessage{Role: "user", Content: []anthropicBlock{tr}})
			continue
		default:
			// unknown role: keep as a text block rather than drop it
			blocks = []anthropicBlock{{Type: "text", Text: textParts(m.Content)}}
		}
		if len(blocks) == 0 {
			continue
		}
		if len(out) > 0 && out[len(out)-1].Role == m.Role {
			out[len(out)-1].Content = append(out[len(out)-1].Content, blocks...)
			continue
		}
		out = append(out, anthropicMessage{Role: m.Role, Content: blocks})
	}
	return strings.Join(sys, "\n\n"), out
}

// userBlocks converts a user message content (string or part array) into
// Anthropic text/image blocks.
func userBlocks(content any) []anthropicBlock {
	switch v := content.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []anthropicBlock{{Type: "text", Text: v}}
	case []any:
		var blocks []anthropicBlock
		for _, part := range v {
			m, ok := part.(map[string]any)
			if !ok {
				continue
			}
			switch m["type"] {
			case "text":
				if s, _ := m["text"].(string); s != "" {
					blocks = append(blocks, anthropicBlock{Type: "text", Text: s})
				}
			case "image_url":
				if src := imageSource(m["image_url"]); src != nil {
					blocks = append(blocks, anthropicBlock{Type: "image", Source: src})
				}
			}
		}
		return blocks
	default:
		return nil
	}
}

// imageSource maps an OpenAI image_url part to an Anthropic image source:
// data: URIs become base64 sources, everything else a URL source.
func imageSource(v any) *anthropicImageSource {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	url, _ := m["url"].(string)
	if url == "" {
		return nil
	}
	if strings.HasPrefix(url, "data:") {
		// data:[<mediatype>][;base64],<data>
		rest := url[len("data:"):]
		comma := strings.Index(rest, ",")
		if comma < 0 {
			return nil
		}
		meta, data := rest[:comma], rest[comma+1:]
		mediaType, b64 := meta, false
		if i := strings.Index(meta, ";base64"); i >= 0 {
			mediaType, b64 = meta[:i], true
		}
		if !b64 {
			return nil
		}
		return &anthropicImageSource{Type: "base64", MediaType: mediaType, Data: data}
	}
	return &anthropicImageSource{Type: "url", URL: url}
}

// assistantBlocks converts an assistant message into text + tool_use blocks.
func assistantBlocks(content any, toolCalls any) []anthropicBlock {
	blocks := userBlocks(content)
	tcs, ok := toolCalls.([]any)
	if !ok {
		return blocks
	}
	for _, tc := range tcs {
		m, ok := tc.(map[string]any)
		if !ok || m["type"] != "function" {
			continue
		}
		id, _ := m["id"].(string)
		fn, _ := m["function"].(map[string]any)
		name, _ := fn["name"].(string)
		var input any = map[string]any{}
		if args, _ := fn["arguments"].(string); args != "" {
			if err := json.Unmarshal([]byte(args), &input); err != nil {
				input = map[string]any{}
			}
		}
		blocks = append(blocks, anthropicBlock{Type: "tool_use", ID: id, Name: name, Input: input})
	}
	return blocks
}

// mapTools converts OpenAI tools[] to Anthropic tools[] (input_schema).
func mapTools(tools []openAITool) []anthropicTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]anthropicTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, anthropicTool{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			InputSchema: t.Function.Parameters,
		})
	}
	return out
}

// mapToolChoice converts the OpenAI tool_choice value to Anthropic's:
// auto/auto, none/none, required/any, {function:{name}} -> {type:"tool"}.
func mapToolChoice(tc any, nTools int) any {
	if tc == nil {
		if nTools == 0 {
			return nil
		}
		return "auto"
	}
	switch v := tc.(type) {
	case string:
		switch v {
		case "auto", "none":
			return v
		case "required":
			return "any"
		}
	case map[string]any:
		if fn, ok := v["function"].(map[string]any); ok {
			if name, _ := fn["name"].(string); name != "" {
				return map[string]any{"type": "tool", "name": name}
			}
		}
	}
	return nil
}

// toStopSequences normalizes the OpenAI stop field (string or array).
func toStopSequences(v any) ([]string, bool) {
	switch s := v.(type) {
	case string:
		if s == "" {
			return nil, false
		}
		return []string{s}, true
	case []any:
		var out []string
		for _, item := range s {
			if str, ok := item.(string); ok && str != "" {
				out = append(out, str)
			}
		}
		return out, len(out) > 0
	}
	return nil, false
}

// cacheBreakpointMode is the per-upstream prompt-cache breakpoint policy
// (spec docs/specs/gateway-anthropic-cache-p0a.md §3).
type cacheBreakpointMode int

const (
	// breakpointsAlways: BP1 (static prefix) + BP2 (conversation tail) on
	// every request, whether or not an L2 block was injected. Default.
	breakpointsAlways cacheBreakpointMode = iota
	// breakpointsL2Only: breakpoints only on requests that carry an L2
	// block (the pre-P0-a trigger, with the new placement).
	breakpointsL2Only
	// breakpointsOff: never emit cache_control. Implied by
	// strip_cache_control = true.
	breakpointsOff
)

// cacheBreakpointMode resolves the upstream's cache_breakpoints setting.
// The value is validated at config load; anything unexpected falls back to
// the default rather than silently disabling caching.
func (up UpstreamConfig) cacheBreakpointMode() cacheBreakpointMode {
	if up.StripCacheControl {
		return breakpointsOff
	}
	switch up.CacheBreakpoints {
	case "l2_only":
		return breakpointsL2Only
	case "off":
		return breakpointsOff
	default:
		return breakpointsAlways
	}
}

// applyCacheBreakpoints renders the top-level system field and places the
// L1 prompt-cache breakpoints (spec P0-a §3):
//
//	BP1  last text block of the STATIC system prompt. The L2 block, when
//	     present, is a separate system block placed AFTER BP1, so a block
//	     that differs from the previous request only invalidates itself,
//	     never tools + static system.
//	BP1′ when there is no static system prompt: the last tool definition
//	     (tools render before system, so it is the latest static position).
//	     The L2 block never carries BP1 — it is the volatile part.
//	BP2  last cacheable block of the final message, any type the API
//	     accepts a marker on (text / image / tool_use / tool_result). Tool
//	     loops end in tool_result, so a text-only rule left them without a
//	     conversation breakpoint.
//
// At most two markers are emitted, well under the API limit of four; all
// use the default 5-minute TTL.
func applyCacheBreakpoints(out *anthropicRequest, system string, inj *inject.Injection, hasBlock bool, mode cacheBreakpointMode) {
	want := mode == breakpointsAlways || (mode == breakpointsL2Only && hasBlock)
	bp := func() *anthropicCacheControl {
		if !want {
			return nil
		}
		return &anthropicCacheControl{Type: "ephemeral"}
	}

	var blocks []anthropicBlock
	if system != "" {
		blocks = append(blocks, anthropicBlock{Type: "text", Text: system, CacheControl: bp()})
	} else if want && len(out.Tools) > 0 {
		out.Tools[len(out.Tools)-1].CacheControl = bp()
	}
	if hasBlock {
		blocks = append(blocks, anthropicBlock{Type: "text", Text: inj.Text})
	}
	switch {
	case len(blocks) == 0:
		// no system field at all
	case len(blocks) == 1 && blocks[0].CacheControl == nil:
		// plain string form: byte-identical to the pre-P0-a no-block request
		out.System = mustJSON(blocks[0].Text)
	default:
		out.System = mustJSON(blocks)
	}

	if want && len(out.Messages) > 0 {
		markLastCacheableBlock(&out.Messages[len(out.Messages)-1])
	}
}

// markLastCacheableBlock attaches a cache_control breakpoint to the last
// block of a message that the Messages API accepts a marker on. Every
// content block type the adapter emits qualifies except thinking, which
// is skipped so a request ending in a preserved reasoning sequence still
// gets its conversation breakpoint on the block before it.
func markLastCacheableBlock(m *anthropicMessage) {
	for i := len(m.Content) - 1; i >= 0; i-- {
		switch m.Content[i].Type {
		case "text", "image", "tool_use", "tool_result":
			m.Content[i].CacheControl = &anthropicCacheControl{Type: "ephemeral"}
			return
		}
	}
}

func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

// ---------------------------------------------------------------------------
// non-streaming response translation

// anthropicResponse is the subset of a /v1/messages response the gateway
// maps back into OpenAI chat.completion shape.
type anthropicResponse struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Role       string `json:"role"`
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Content    []struct {
		Type  string `json:"type"`
		Text  string `json:"text"`
		ID    string `json:"id"`
		Name  string `json:"name"`
		Input any    `json:"input"`
	} `json:"content"`
	Usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

// anthropicToOpenAIResponse converts a non-streaming /v1/messages response
// into OpenAI chat.completion JSON (usage: input_tokens -> prompt_tokens,
// output_tokens -> completion_tokens, cache_read -> prompt_tokens_details.
// cached_tokens; tool_use blocks -> tool_calls; stop_reason -> finish_reason).
func anthropicToOpenAIResponse(body []byte, model string) ([]byte, error) {
	var a anthropicResponse
	if err := json.Unmarshal(body, &a); err != nil {
		return nil, fmt.Errorf("parse anthropic response: %w", err)
	}
	var text strings.Builder
	var toolCalls []map[string]any
	for _, c := range a.Content {
		switch c.Type {
		case "text":
			text.WriteString(c.Text)
		case "tool_use":
			args, _ := json.Marshal(c.Input)
			toolCalls = append(toolCalls, map[string]any{
				"id": c.ID, "type": "function",
				"function": map[string]any{"name": c.Name, "arguments": string(args)},
			})
		}
	}
	msg := map[string]any{"role": "assistant", "content": text.String()}
	if len(toolCalls) > 0 {
		msg["tool_calls"] = toolCalls
	}
	// Anthropic-style input_tokens is incremental — it excludes cache reads
	// and writes (measured: cache_read + input = full input, glm-spike-week.md
	// §2). OpenAI prompt_tokens is the full input, so reconcile here or the
	// translated usage under-reports every cache hit by its cached portion.
	prompt := a.Usage.InputTokens + a.Usage.CacheCreationInputTokens + a.Usage.CacheReadInputTokens
	usage := map[string]any{
		"prompt_tokens":     prompt,
		"completion_tokens": a.Usage.OutputTokens,
		"total_tokens":      prompt + a.Usage.OutputTokens,
	}
	if a.Usage.CacheReadInputTokens > 0 {
		usage["prompt_tokens_details"] = map[string]any{"cached_tokens": a.Usage.CacheReadInputTokens}
	}
	out := map[string]any{
		"id":      a.ID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       msg,
			"finish_reason": anthropicStopReason(a.StopReason),
		}},
		"usage": usage,
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("marshal openai response: %w", err)
	}
	return raw, nil
}

// anthropicStopReason maps Anthropic stop_reason to OpenAI finish_reason
// (design §0.5 response table).
func anthropicStopReason(s string) string {
	switch s {
	case "", "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	default:
		return "stop"
	}
}

// anthropicError extracts the message from an Anthropic error body
// {"error":{"type":...,"message":...}} for a readable envelope.
func anthropicError(body []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err != nil || e.Error.Message == "" {
		return strings.TrimSpace(string(body))
	}
	return e.Error.Message
}

// ---------------------------------------------------------------------------
// streaming translation (Anthropic SSE -> OpenAI SSE)

// anthropicSSEConverter is a stateful line filter that turns Anthropic
// /v1/messages stream events into OpenAI chat.completion.chunk events.
//
//	message_start          -> delta {role:"assistant"} (first chunk); captures
//	                          message.id/model for every subsequent chunk
//	content_block_start    -> tool_use: delta {tool_calls:[{index,id,name,arguments:""}]}
//	content_block_delta    -> text_delta -> delta {content}; input_json_delta
//	                          -> delta {tool_calls:[{index,function.arguments}]}
//	message_delta          -> delta {}, finish_reason (mapped)
//	message_stop           -> data: [DONE]
//	error                  -> OpenAI error envelope + [DONE] (upstream failure
//	                          must never surface as a clean completion)
//
// Thinking blocks and non-data lines (event:, ping) are dropped: the client
// only ever sees OpenAI-shaped chunks.
type anthropicSSEConverter struct {
	w          io.Writer
	flusher    http.Flusher
	toolIdx    int  // next tool_calls index
	done       bool // stream has ended (message_stop / error / [DONE])
	id         string
	model      string
	created    int64
	finishSent bool // a real finish_reason already went out
	// Usage counters harvested from message_start/message_delta frames.
	// Counters are cumulative; max-merge tolerates gateways that repeat
	// partial usage across both frames (GLM-P0-3 / #291). usageSent guards
	// the single translated usage chunk emitted before [DONE].
	inTok, outTok, cacheCreate, cacheRead int
	sawUsage                              bool
	usageSent                             bool
}

func newAnthropicSSEConverter(w io.Writer, flusher http.Flusher) *anthropicSSEConverter {
	return &anthropicSSEConverter{w: w, flusher: flusher, created: time.Now().Unix()}
}

// feed consumes one SSE line. It returns true once the stream has ended
// (message_stop, error, or an explicit data: [DONE]).
func (c *anthropicSSEConverter) feed(line []byte) bool {
	trimmed := bytes.TrimSpace(line)
	if !bytes.HasPrefix(trimmed, []byte("data:")) {
		return false
	}
	payload := bytes.TrimSpace(bytes.TrimPrefix(trimmed, []byte("data:")))
	if bytes.Equal(payload, []byte("[DONE]")) {
		c.done = true
		return true
	}
	var ev struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return false
	}
	switch ev.Type {
	case "message_start":
		c.messageStart(payload)
		c.writeChunk(map[string]any{"role": "assistant"}, nil)
	case "content_block_start":
		c.blockStart(payload)
	case "content_block_delta":
		c.blockDelta(payload)
	case "message_delta":
		c.messageDelta(payload)
	case "error":
		c.writeError(payload)
		c.done = true
		return true
	case "message_stop":
		c.writeUsageChunk()
		c.writeDone()
		c.done = true
		return true
	}
	return false
}

// finish terminates a stream that ended abnormally (no message_stop): emit
// the final chunk + [DONE] so the client never hangs (same guarantee as the
// OpenAI passthrough path, design §3.4). If a real finish_reason already
// went out via message_delta, it is not overwritten.
func (c *anthropicSSEConverter) finish() {
	if c.done {
		return
	}
	c.done = true
	if !c.finishSent {
		c.writeChunk(map[string]any{}, "stop")
	}
	c.writeUsageChunk()
	c.writeDone()
}

// messageStart captures the message id/model so every OpenAI chunk carries
// the stable identity the OpenAI SDKs validate on, plus the input/cache
// usage counters the native stream reports here (message_start carries
// input-side accounting; message_delta carries output_tokens).
func (c *anthropicSSEConverter) messageStart(payload []byte) {
	var ev struct {
		Message struct {
			ID    string        `json:"id"`
			Model string        `json:"model"`
			Usage *wireSSEUsage `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	if ev.Message.ID != "" {
		c.id = ev.Message.ID
	}
	if ev.Message.Model != "" {
		c.model = ev.Message.Model
	}
	c.mergeUsage(ev.Message.Usage)
}

// wireSSEUsage is the usage fragment carried on Anthropic stream frames.
type wireSSEUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// mergeUsage folds one frame's counters in. Counters are cumulative and
// non-negative, so keeping the max also tolerates gateways that repeat
// partial usage on both message_start and message_delta.
func (c *anthropicSSEConverter) mergeUsage(u *wireSSEUsage) {
	if u == nil {
		return
	}
	c.sawUsage = true
	c.inTok = max(c.inTok, u.InputTokens)
	c.outTok = max(c.outTok, u.OutputTokens)
	c.cacheCreate = max(c.cacheCreate, u.CacheCreationInputTokens)
	c.cacheRead = max(c.cacheRead, u.CacheReadInputTokens)
}

// usage returns the harvested provider accounting normalized to the full
// prompt convention (input + cache_creation + cache_read; see
// anthropicToOpenAIResponse). ok is false when no frame carried usage.
func (c *anthropicSSEConverter) usage() (normUsage, bool) {
	if !c.sawUsage {
		return normUsage{}, false
	}
	return normalizeUpstreamUsage(&upstreamUsage{
		InputTokens:              c.inTok,
		OutputTokens:             c.outTok,
		CacheCreationInputTokens: c.cacheCreate,
		CacheReadInputTokens:     c.cacheRead,
	})
}

// writeUsageChunk emits one OpenAI stream-usage event (empty choices +
// usage) carrying the translated provider accounting, before [DONE], so
// OpenAI-surface clients receive real usage instead of nothing.
func (c *anthropicSSEConverter) writeUsageChunk() {
	nu, ok := c.usage()
	if !ok || c.usageSent {
		return
	}
	c.usageSent = true
	usage := map[string]any{
		"prompt_tokens":     nu.Prompt,
		"completion_tokens": nu.Completion,
		"total_tokens":      nu.Prompt + nu.Completion,
	}
	if nu.CacheHit > 0 {
		usage["prompt_tokens_details"] = map[string]any{"cached_tokens": nu.CacheHit}
	}
	evt := map[string]any{
		"id":      c.id,
		"object":  "chat.completion.chunk",
		"created": c.created,
		"model":   c.model,
		"choices": []any{},
		"usage":   usage,
	}
	raw, _ := json.Marshal(evt)
	_, _ = fmt.Fprintf(c.w, "data: %s\n\n", raw)
	if c.flusher != nil {
		c.flusher.Flush()
	}
}

// writeError maps an Anthropic mid-stream error event to an OpenAI error
// envelope + [DONE] so the client sees a failure, never a clean completion.
func (c *anthropicSSEConverter) writeError(payload []byte) {
	var ev struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		_, _ = fmt.Fprintf(c.w, "data: %s\n\n", `{"error":{"message":"upstream stream error"}}`)
	} else {
		msg := ev.Error.Message
		if msg == "" {
			msg = ev.Error.Type
		}
		if msg == "" {
			msg = "upstream stream error"
		}
		raw, _ := json.Marshal(map[string]any{"error": map[string]any{"message": msg}})
		_, _ = fmt.Fprintf(c.w, "data: %s\n\n", raw)
	}
	c.writeDone()
	if c.flusher != nil {
		c.flusher.Flush()
	}
}

func (c *anthropicSSEConverter) blockStart(payload []byte) {
	var ev struct {
		Index        int `json:"index"`
		ContentBlock struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"content_block"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	if ev.ContentBlock.Type != "tool_use" {
		return
	}
	c.writeChunk(map[string]any{"tool_calls": []any{map[string]any{
		"index":    c.toolIdx,
		"id":       ev.ContentBlock.ID,
		"type":     "function",
		"function": map[string]any{"name": ev.ContentBlock.Name, "arguments": ""},
	}}}, nil)
	c.toolIdx++
}

func (c *anthropicSSEConverter) blockDelta(payload []byte) {
	var ev struct {
		Index int `json:"index"`
		Delta struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
		} `json:"delta"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	switch ev.Delta.Type {
	case "text_delta":
		c.writeChunk(map[string]any{"content": ev.Delta.Text}, nil)
	case "input_json_delta":
		c.writeChunk(map[string]any{"tool_calls": []any{map[string]any{
			"index":    c.toolIdx - 1,
			"function": map[string]any{"arguments": ev.Delta.PartialJSON},
		}}}, nil)
	}
}

func (c *anthropicSSEConverter) messageDelta(payload []byte) {
	var ev struct {
		Delta struct {
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
		Usage *wireSSEUsage `json:"usage"`
	}
	if err := json.Unmarshal(payload, &ev); err != nil {
		return
	}
	c.mergeUsage(ev.Usage)
	if ev.Delta.StopReason == "" {
		return
	}
	c.finishSent = true
	c.writeChunk(map[string]any{}, anthropicStopReason(ev.Delta.StopReason))
}

func (c *anthropicSSEConverter) writeChunk(delta map[string]any, finish any) {
	evt := map[string]any{
		"id":      c.id,
		"object":  "chat.completion.chunk",
		"created": c.created,
		"model":   c.model,
		"choices": []any{map[string]any{
			"index": 0, "delta": delta, "finish_reason": finish,
		}},
	}
	raw, _ := json.Marshal(evt)
	_, _ = fmt.Fprintf(c.w, "data: %s\n\n", raw)
	if c.flusher != nil {
		c.flusher.Flush()
	}
}

func (c *anthropicSSEConverter) writeDone() {
	_, _ = io.WriteString(c.w, "data: [DONE]\n\n")
	if c.flusher != nil {
		c.flusher.Flush()
	}
}

// streamThroughAnthropic relays a streaming Anthropic upstream response as
// OpenAI SSE (the events must be translated — Anthropic frames differ from
// OpenAI's). Sidecar/usage accounting matches the OpenAI streaming path:
// request turns only, assistant content extraction is deferred debt.
func (g *Gateway) streamThroughAnthropic(w http.ResponseWriter, resp *http.Response, sessionID string, req *chatRequest, ctxHash string, query string, injectedTokens int64, sliceHits int, up UpstreamConfig, jc *judgeCollector, l3Obs cache.Obs, l3FalseHit bool) {
	if resp.StatusCode >= 400 {
		out, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		writeAPIError(w, resp.StatusCode, "upstream_error",
			fmt.Sprintf("upstream returned %d: %s", resp.StatusCode, anthropicError(out)))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("x-semantix-cache", "miss")
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)

	conv := newAnthropicSSEConverter(w, flusher)
	br := bufio.NewReader(resp.Body)
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 && conv.feed(line) {
			break
		}
		if err != nil {
			break
		}
	}
	conv.finish() // no-op when message_stop already seen
	g.recordSession(sessionID, ctxHash, req.Model, turns(req, ""))
	ev := g.withL3Obs(usage.Event{
		SessionID:      sessionID,
		Provider:       up.Name,
		Vendor:         up.Vendor,
		Model:          req.Model,
		TokensIn:       int64(len(query)/4) + injectedTokens,
		InjectedTokens: injectedTokens,
		SliceHits:      sliceHits,
		JudgeDecisions: jc.drain(),
		At:             g.now().Unix(),
	}, l3Obs, l3FalseHit)
	// message_start/message_delta carried real provider accounting: prefer
	// it over the bytes/4 estimate (GLM-P0-3 / #291).
	if nu, ok := conv.usage(); ok {
		ev.TokensIn = nu.Prompt
		ev.TokensOut = nu.Completion
		ev.CacheHitToken = nu.CacheHit
		ev.Exact = true
	}
	g.recordUsage(ev)
}
