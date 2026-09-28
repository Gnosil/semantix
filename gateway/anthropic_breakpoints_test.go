package gateway

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"semantix/kernel/inject"
	"semantix/kernel/slice"
)

// Tests for the P0-a breakpoint policy and Anthropic-path sanitation
// (docs/specs/gateway-anthropic-cache-p0a.md). All assertions are made on
// the serialized wire body, not on Go structs.

func p0aInjection() *inject.Injection {
	return &inject.Injection{
		Text:   "[semantix-reuse]\nprior knowledge\n[/semantix-reuse]",
		Slices: []*slice.Slice{{ID: "s1", Type: slice.Prompt, Scope: slice.Project, Content: []byte("prior knowledge")}},
	}
}

// wireMarkers returns where every cache_control marker landed, as
// "system[i]" / "tools[i]" / "messages[i].content[j]:<type>".
func wireMarkers(t *testing.T, raw []byte) []string {
	t.Helper()
	var w struct {
		System   json.RawMessage  `json:"system"`
		Tools    []map[string]any `json:"tools"`
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatalf("unmarshal wire: %v\n%s", err, raw)
	}
	var out []string
	if len(w.System) > 0 && w.System[0] == '[' {
		var blocks []map[string]any
		if err := json.Unmarshal(w.System, &blocks); err != nil {
			t.Fatal(err)
		}
		for i, b := range blocks {
			if b["cache_control"] != nil {
				out = append(out, "system["+strconv.Itoa(i)+"]")
			}
		}
	}
	for i, tl := range w.Tools {
		if tl["cache_control"] != nil {
			out = append(out, "tools["+strconv.Itoa(i)+"]")
		}
	}
	for i, m := range w.Messages {
		for j, b := range m.Content {
			if b["cache_control"] != nil {
				typ, _ := b["type"].(string)
				out = append(out, "messages["+strconv.Itoa(i)+"].content["+strconv.Itoa(j)+"]:"+typ)
			}
		}
	}
	return out
}

const p0aTools = `"tools":[{"type":"function","function":{"name":"read_file","description":"read","parameters":{"type":"object"}}},{"type":"function","function":{"name":"grep","description":"grep","parameters":{"type":"object"}}}]`

func TestP0ABreakpointPlacementTable(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		inj    *inject.Injection
		up     func() UpstreamConfig
		want   []string // marker positions
		system string   // "" = absent, "string", "blocks:N"
	}{
		{
			name:   "system, no block",
			body:   `{"model":"c","messages":[{"role":"system","content":"sys"},{"role":"user","content":"hi"}]}`,
			up:     anthropicUp,
			want:   []string{"system[0]", "messages[0].content[0]:text"},
			system: "blocks:1",
		},
		{
			name:   "system + block: BP1 on static, block after it unmarked",
			body:   `{"model":"c","messages":[{"role":"system","content":"sys"},{"role":"user","content":"hi"}]}`,
			inj:    p0aInjection(),
			up:     anthropicUp,
			want:   []string{"system[0]", "messages[0].content[0]:text"},
			system: "blocks:2",
		},
		{
			name:   "no system, block, tools: BP1' on last tool",
			body:   `{"model":"c",` + p0aTools + `,"messages":[{"role":"user","content":"hi"}]}`,
			inj:    p0aInjection(),
			up:     anthropicUp,
			want:   []string{"tools[1]", "messages[0].content[0]:text"},
			system: "string",
		},
		{
			name:   "no system, no tools, no block: BP2 only",
			body:   `{"model":"c","messages":[{"role":"user","content":"hi"}]}`,
			up:     anthropicUp,
			want:   []string{"messages[0].content[0]:text"},
			system: "",
		},
		{
			name: "tool loop: BP2 lands on tool_result",
			body: `{"model":"c","messages":[
				{"role":"system","content":"sys"},
				{"role":"user","content":"read it"},
				{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"read_file","arguments":"{}"}}]},
				{"role":"tool","tool_call_id":"c1","content":"out"}]}`,
			up:     anthropicUp,
			want:   []string{"system[0]", "messages[2].content[0]:tool_result"},
			system: "blocks:1",
		},
		{
			name: "final message is image-only: BP2 on the image",
			body: `{"model":"c","messages":[
				{"role":"system","content":"sys"},
				{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://x/y.png"}}]}]}`,
			up:     anthropicUp,
			want:   []string{"system[0]", "messages[0].content[0]:image"},
			system: "blocks:1",
		},
		{
			name: "final assistant ends with thinking first: BP2 skips thinking",
			body: `{"model":"c","messages":[
				{"role":"system","content":"sys"},
				{"role":"user","content":"q"},
				{"role":"assistant","content":"a","reasoning_content":"thinking..."}]}`,
			up: anthropicUp,
			// thinking rides FIRST on the assistant turn; text is last → BP2
			// on the text block (index 1), never on the thinking block.
			want:   []string{"system[0]", "messages[1].content[1]:text"},
			system: "blocks:1",
		},
		{
			name:   "whitespace-only system is dropped, never marked",
			body:   `{"model":"c","messages":[{"role":"system","content":"  \n "},{"role":"user","content":"hi"}]}`,
			up:     anthropicUp,
			want:   []string{"messages[0].content[0]:text"},
			system: "",
		},
		{
			name: "strip_cache_control: zero markers, plain string system",
			body: `{"model":"c","messages":[{"role":"system","content":"sys"},{"role":"user","content":"hi"}]}`,
			inj:  p0aInjection(),
			up: func() UpstreamConfig {
				u := anthropicUp()
				u.StripCacheControl = true
				return u
			},
			want:   nil,
			system: "blocks:2", // static + block still need the array form
		},
		{
			name: "cache_breakpoints=off, no block: byte-identical pre-P0-a string system",
			body: `{"model":"c","messages":[{"role":"system","content":"sys"},{"role":"user","content":"hi"}]}`,
			up: func() UpstreamConfig {
				u := anthropicUp()
				u.CacheBreakpoints = "off"
				return u
			},
			want:   nil,
			system: "string",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := toAnthropicRequest([]byte(tc.body), tc.up(), tc.inj)
			if err != nil {
				t.Fatalf("toAnthropicRequest: %v", err)
			}
			got := wireMarkers(t, raw)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("markers = %v, want %v\n%s", got, tc.want, raw)
			}
			if len(got) > 4 {
				t.Errorf("%d markers exceeds the API limit of 4", len(got))
			}
			var w struct {
				System json.RawMessage `json:"system"`
			}
			_ = json.Unmarshal(raw, &w)
			switch {
			case tc.system == "" && len(w.System) != 0:
				t.Errorf("system should be absent, got %s", w.System)
			case tc.system == "string" && (len(w.System) == 0 || w.System[0] != '"'):
				t.Errorf("system should be a plain string, got %s", w.System)
			case strings.HasPrefix(tc.system, "blocks:"):
				var blocks []map[string]any
				if err := json.Unmarshal(w.System, &blocks); err != nil || strconv.Itoa(len(blocks)) != strings.TrimPrefix(tc.system, "blocks:") {
					t.Errorf("system should be %s, got %s", tc.system, w.System)
				}
			}
		})
	}
}

// TestP0AStaticPrefixStableAcrossBlocks: the reason for splitting the system
// field — two requests in one session whose L2 blocks differ must share the
// bytes up to and including BP1, so tools + static system still hit.
func TestP0AStaticPrefixStableAcrossBlocks(t *testing.T) {
	body := `{"model":"c",` + p0aTools + `,"messages":[{"role":"system","content":"a long static system prompt"},{"role":"user","content":"turn one"}]}`
	injA := p0aInjection()
	injB := p0aInjection()
	injB.Text = "[semantix-reuse]\ndifferent knowledge\n[/semantix-reuse]"

	rawA, err := toAnthropicRequest([]byte(body), anthropicUp(), injA)
	if err != nil {
		t.Fatal(err)
	}
	rawB, err := toAnthropicRequest([]byte(body), anthropicUp(), injB)
	if err != nil {
		t.Fatal(err)
	}
	// The provider renders tools -> system -> messages, so the cache-relevant
	// static prefix is tools + system[0]; Go's marshal order (messages
	// first) is irrelevant. Compare those fields directly.
	type wire struct {
		Tools  json.RawMessage   `json:"tools"`
		System []json.RawMessage `json:"system"`
	}
	var wa, wb wire
	if err := json.Unmarshal(rawA, &wa); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(rawB, &wb); err != nil {
		t.Fatal(err)
	}
	if len(wa.System) != 2 || len(wb.System) != 2 {
		t.Fatalf("system must be [static, l2]: %s\n%s", rawA, rawB)
	}
	if !bytes.Equal(wa.Tools, wb.Tools) {
		t.Errorf("tools differ between requests with different L2 blocks")
	}
	if !bytes.Equal(wa.System[0], wb.System[0]) || !bytes.Contains(wa.System[0], []byte(`"cache_control"`)) {
		t.Errorf("system[0] (BP1) must be byte-identical and marked:\n%s\n%s", wa.System[0], wb.System[0])
	}
	if bytes.Equal(wa.System[1], wb.System[1]) {
		t.Fatal("test is vacuous: the two L2 blocks are identical")
	}
	if bytes.Contains(wa.System[1], []byte(`"cache_control"`)) {
		t.Errorf("L2 block must not carry a marker: %s", wa.System[1])
	}
}

// TestP0AAnthropicPathSanitized: the Anthropic hop runs the same prefix
// hygiene as the OpenAI hop — attribution markers stripped from the system
// head, tools sorted by name (tools render first in the Anthropic prefix).
func TestP0AAnthropicPathSanitized(t *testing.T) {
	g := sanitizeGateway(t, SanitizeConfig{})
	up := anthropicUp()
	body := `{"model":"c",
		"tools":[{"type":"function","function":{"name":"zeta","parameters":{"type":"object"}}},{"type":"function","function":{"name":"alpha","parameters":{"type":"object"}}}],
		"messages":[{"role":"system","content":"x-anthropic-billing-header: cch=abc\n\nYou are helpful."},{"role":"user","content":"hi"}]}`

	raw, err := toAnthropicRequest(g.sanitizeBody([]byte(body), up), up, nil)
	if err != nil {
		t.Fatal(err)
	}
	var req anthropicRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	if len(req.Tools) != 2 || req.Tools[0].Name != "alpha" || req.Tools[1].Name != "zeta" {
		t.Errorf("tools not sorted on the Anthropic path: %#v", req.Tools)
	}
	if strings.Contains(string(req.System), "x-anthropic-billing-header") {
		t.Errorf("attribution marker survived on the Anthropic path: %s", req.System)
	}
	if !strings.Contains(string(req.System), "You are helpful.") {
		t.Errorf("system prompt body lost: %s", req.System)
	}

	// The hop in handleChat uses exactly this composition; guard the
	// unsanitized order so the test cannot pass by accident.
	rawUnsanitized, _ := toAnthropicRequest([]byte(body), up, nil)
	var unsan anthropicRequest
	_ = json.Unmarshal(rawUnsanitized, &unsan)
	if unsan.Tools[0].Name != "zeta" {
		t.Fatal("fixture no longer exercises tool ordering")
	}
}

func TestP0ASanitizeBodyFailOpen(t *testing.T) {
	g := sanitizeGateway(t, SanitizeConfig{})
	in := []byte(`{not json`)
	if out := g.sanitizeBody(in, anthropicUp()); !bytes.Equal(out, in) {
		t.Errorf("undecodable body must pass through untouched, got %s", out)
	}
}

func TestP0ACacheBreakpointsConfigValidation(t *testing.T) {
	for _, v := range []string{"", "always", "l2_only", "off"} {
		c := DefaultConfig()
		c.Server.GatewayKey = "k"
		c.Store.DB = "x.jsonl"
		c.Upstreams = []UpstreamConfig{{
			Name: "claude", BaseURL: "https://api.anthropic.com/v1", APIKey: "k",
			ModelAlias: []string{"claude-sonnet"}, UpstreamModel: "claude-sonnet-4",
			Vendor: "anthropic", CacheBreakpoints: v,
		}}
		if err := c.validate(); err != nil {
			t.Errorf("cache_breakpoints=%q must validate, got %v", v, err)
		}
	}
	c := DefaultConfig()
	c.Server.GatewayKey = "k"
	c.Store.DB = "x.jsonl"
	c.Upstreams = []UpstreamConfig{{
		Name: "claude", BaseURL: "https://api.anthropic.com/v1", APIKey: "k",
		ModelAlias: []string{"claude-sonnet"}, UpstreamModel: "claude-sonnet-4",
		Vendor: "anthropic", CacheBreakpoints: "sometimes",
	}}
	if err := c.validate(); err == nil || !strings.Contains(err.Error(), "cache_breakpoints") {
		t.Errorf("invalid cache_breakpoints must be rejected, got %v", err)
	}
}

// TestP0AEmptyToolResultOmitsContent: an empty tool output omits content
// rather than shipping "content":"".
func TestP0AEmptyToolResultOmitsContent(t *testing.T) {
	body := `{"model":"c","messages":[
		{"role":"user","content":"go"},
		{"role":"assistant","content":"","tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":""}]}`
	raw, err := toAnthropicRequest([]byte(body), anthropicUp(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var w struct {
		Messages []struct {
			Content []map[string]any `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	tr := w.Messages[2].Content[0]
	if tr["type"] != "tool_result" {
		t.Fatalf("want tool_result, got %#v", tr)
	}
	if _, has := tr["content"]; has {
		t.Errorf("empty tool output must omit content: %#v", tr)
	}
}

// TestP0AToolChoiceObjectForm: tool_choice is always an object on the wire.
func TestP0AToolChoiceObjectForm(t *testing.T) {
	for in, want := range map[string]string{`"auto"`: "auto", `"none"`: "none", `"required"`: "any", `null`: "auto"} {
		body := `{"model":"c",` + p0aTools + `,"tool_choice":` + in + `,"messages":[{"role":"user","content":"hi"}]}`
		raw, err := toAnthropicRequest([]byte(body), anthropicUp(), nil)
		if err != nil {
			t.Fatal(err)
		}
		var w struct {
			ToolChoice json.RawMessage `json:"tool_choice"`
		}
		_ = json.Unmarshal(raw, &w)
		var obj map[string]any
		if err := json.Unmarshal(w.ToolChoice, &obj); err != nil || obj["type"] != want {
			t.Errorf("tool_choice %s -> %s, want object {type:%q}", in, w.ToolChoice, want)
		}
	}
}
