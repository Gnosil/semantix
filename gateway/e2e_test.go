package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"semantix/kernel/slice"
	"semantix/kernel/usage"
)

// ---------------------------------------------------------------------------
// helpers

// testUpstream simulates an OpenAI-compatible upstream LLM: it counts calls,
// captures the last forwarded body, and answers with a canned response.
type testUpstream struct {
	mu         sync.Mutex
	calls      int
	lastBody   map[string]any
	stream     string                      // canned SSE body when the request asked for stream
	plain      string                      // canned JSON body otherwise
	streamFunc func(w http.ResponseWriter) // optional; overrides stream
}

func (u *testUpstream) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		u.mu.Lock()
		u.calls++
		u.lastBody = parsed
		u.mu.Unlock()
		if stream, _ := parsed["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			if u.streamFunc != nil {
				u.streamFunc(w)
				return
			}
			_, _ = io.WriteString(w, u.stream)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, u.plain)
	}
}

func (u *testUpstream) callCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls
}

func (u *testUpstream) forwardedMessages() []map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	m, _ := u.lastBody["messages"].([]any)
	out := make([]map[string]any, 0, len(m))
	for _, item := range m {
		if mm, ok := item.(map[string]any); ok {
			out = append(out, mm)
		}
	}
	return out
}

// newTestGateway builds a gateway wired to the fake upstream in a temp dir.
func newTestGateway(t *testing.T, upURL string) *Gateway {
	t.Helper()
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Server.GatewayKey = "test-key"
	cfg.Store.DB = filepath.Join(dir, "db.jsonl")
	cfg.Store.DepsRoot = dir
	cfg.Ingest.SessionsDir = filepath.Join(dir, "sessions")
	cfg.Ingest.UsageLog = filepath.Join(dir, "usage.jsonl")
	cfg.Upstreams = []UpstreamConfig{{
		Name: "deepseek", BaseURL: upURL, APIKey: "up-key",
		ModelAlias: []string{"deepseek-chat"}, UpstreamModel: "deepseek-chat",
		Vendor: "deepseek",
	}}
	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g
}

// seed puts a slice into the store and index directly.
func seed(t *testing.T, g *Gateway, s *slice.Slice) {
	t.Helper()
	if s.CreatedAt == 0 {
		s.CreatedAt = time.Now().Unix()
	}
	if s.Meta.Origin == "" {
		// Test fixtures model gateway-ingested slices; the default
		// [slice] min_inject_origin=session-auto floor requires the tag
		// (Issue #279).
		s.Meta.Origin = slice.OriginSessionAuto
	}
	if err := g.store.Put(s); err != nil {
		t.Fatal(err)
	}
	if err := g.index.Insert(s); err != nil {
		t.Fatal(err)
	}
}

func chatBody(model, user string, stream bool) string {
	raw, _ := json.Marshal(map[string]any{
		"model":    model,
		"stream":   stream,
		"messages": []map[string]string{{"role": "user", "content": user}},
	})
	return string(raw)
}

func postChat(t *testing.T, srv *httptest.Server, key, body string) (*http.Response, []byte) {
	t.Helper()
	return postChatWithHeaders(t, srv, key, nil, body)
}

// postChatWithHeaders posts a chat completion with extra headers — e.g. a
// fixed x-semantix-session id so the sidecar file is deterministic.
func postChatWithHeaders(t *testing.T, srv *httptest.Server, key string, headers map[string]string, body string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp, out
}

// readSidecarLines parses the JSONL lines of a session sidecar file.
func readSidecarLines(t *testing.T, g *Gateway, sessionID string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(g.cfg.Ingest.SessionsDir, sessionID+".jsonl"))
	if err != nil {
		t.Fatalf("read sidecar %s: %v", sessionID, err)
	}
	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("bad sidecar line %q: %v", l, err)
		}
		lines = append(lines, m)
	}
	return lines
}

func respContent(t *testing.T, body []byte) string {
	t.Helper()
	var r struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("unmarshal response %s: %v", body, err)
	}
	if len(r.Choices) == 0 {
		t.Fatalf("no choices in %s", body)
	}
	return r.Choices[0].Message.Content
}

// ---------------------------------------------------------------------------
// L3 hit: zero upstream calls (issue acceptance #1)

func TestE2EL3HitZeroUpstreamCalls(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"unused"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	chash, _ := contextHash([]chatMessage{msg("user", "hello world")})
	seed(t, g, &slice.Slice{
		ID: "l3-a", Type: slice.Result, Scope: slice.Project,
		Content: []byte("hello world hello world cached answer"),
		Meta:    slice.SliceMeta{L3Safe: true, ContextHash: chash, Model: "deepseek-chat", Origin: slice.OriginSessionAuto},
	})

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	if resp.Header.Get("x-semantix-cache") != "hit" {
		t.Errorf("x-semantix-cache = %q, want hit", resp.Header.Get("x-semantix-cache"))
	}
	if got := respContent(t, out); got != "hello world hello world cached answer" {
		t.Errorf("content = %q, want cached answer", got)
	}
	if n := up.callCount(); n != 0 {
		t.Errorf("upstream calls = %d, want 0 (L3 must not reach upstream)", n)
	}
}

func TestE2EL3UnknownCreatedAtFailsClosed(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"fresh upstream"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	chash, _ := contextHash([]chatMessage{msg("user", "hello world")})
	s := &slice.Slice{
		ID: "l3-legacy", Type: slice.Result, Scope: slice.Project,
		Content: []byte("hello world legacy cached answer"),
		Meta:    slice.SliceMeta{L3Safe: true, ContextHash: chash, Model: "deepseek-chat", Origin: slice.OriginSessionAuto},
	}
	// Bypass seed: it intentionally backfills CreatedAt for ordinary tests.
	if err := g.store.Put(s); err != nil {
		t.Fatal(err)
	}
	if err := g.index.Insert(s); err != nil {
		t.Fatal(err)
	}

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))
	if resp.Header.Get("x-semantix-cache") != "miss" {
		t.Fatalf("cache = %q, want miss for unknown CreatedAt (%s)", resp.Header.Get("x-semantix-cache"), out)
	}
	if n := up.callCount(); n != 1 {
		t.Fatalf("upstream calls = %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// L2 injection: forwarded request carries the reuse block (acceptance #2)

func TestE2EL2InjectionForwarded(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"upstream reply"}}],"usage":{"prompt_tokens":5,"completion_tokens":3}}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	seed(t, g, &slice.Slice{
		ID: "l2-a", Type: slice.Prompt, Scope: slice.Project,
		Content: []byte("prior knowledge about widgets"),
	})
	// Keep the target's BM25 score above the default absolute hit threshold.
	// A single-document corpus scores too low and only produces an empty
	// marker block, which is not an L2 slice hit.
	for i, content := range []string{"alpha", "bravo", "charlie", "delta"} {
		seed(t, g, &slice.Slice{
			ID: fmt.Sprintf("distractor-%d", i), Type: slice.Prompt, Scope: slice.Project,
			Content: []byte(content),
		})
	}

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "widgets", false))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	if resp.Header.Get("x-semantix-cache") != "miss" {
		t.Errorf("x-semantix-cache = %q, want miss", resp.Header.Get("x-semantix-cache"))
	}
	if got := respContent(t, out); got != "upstream reply" {
		t.Errorf("content = %q, want upstream reply (passthrough)", got)
	}
	if n := up.callCount(); n != 1 {
		t.Fatalf("upstream calls = %d, want 1", n)
	}
	msgs := up.forwardedMessages()
	if len(msgs) == 0 {
		t.Fatal("no messages forwarded")
	}
	sys, _ := msgs[0]["content"].(string)
	if msgs[0]["role"] != "system" || !strings.Contains(sys, "[semantix-reuse]") {
		t.Errorf("injection block not prepended as system message: %#v", msgs[0])
	}
	summary, err := usage.Summarize(g.cfg.Ingest.UsageLog, usage.DefaultCostMissPerMTok, usage.DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	if summary.SliceHits != 1 {
		t.Errorf("usage slice hits = %d, want 1", summary.SliceHits)
	}
}

// ---------------------------------------------------------------------------
// SSE passthrough (design §3.4)

func TestE2EStreamPassthrough(t *testing.T) {
	up := &testUpstream{stream: "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\ndata: [DONE]\n\n"}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "stream me", true))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if !strings.Contains(string(out), "[DONE]") || !strings.Contains(string(out), `"content":"a"`) {
		t.Errorf("stream not relayed verbatim: %s", out)
	}
}

// ---------------------------------------------------------------------------
// auth + errors

func TestE2EAuthAndErrors(t *testing.T) {
	upSrv := httptest.NewServer((&testUpstream{plain: `{}`}).handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	// no key
	resp, out := postChat(t, srv, "", chatBody("deepseek-chat", "hi", false))
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(string(out), "authentication_error") {
		t.Errorf("no key: status=%d body=%s", resp.StatusCode, out)
	}
	// wrong key
	resp, out = postChat(t, srv, "wrong", chatBody("deepseek-chat", "hi", false))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong key: status=%d", resp.StatusCode)
	}
	// unknown model
	resp, out = postChat(t, srv, "test-key", chatBody("nope", "hi", false))
	if resp.StatusCode != http.StatusNotFound || !strings.Contains(string(out), "model_not_found") {
		t.Errorf("unknown model: status=%d body=%s", resp.StatusCode, out)
	}
	// bad request
	resp, out = postChat(t, srv, "test-key", `{"model":"deepseek-chat","messages":[]}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty messages: status=%d body=%s", resp.StatusCode, out)
	}
	// healthz is unauthenticated
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(out), `"status":"ok"`) {
		t.Errorf("healthz: status=%d body=%s", resp.StatusCode, out)
	}
	// models endpoint lists the routed model
	resp, err = http.Get(srv.URL + "/v1/models")
	if err != nil {
		t.Fatal(err)
	}
	out, _ = io.ReadAll(resp.Body)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("/v1/models without key: status=%d", resp.StatusCode)
	}
}

// ---------------------------------------------------------------------------
// L3 streaming replay (design §3.4)

func TestE2EL3StreamingReplay(t *testing.T) {
	up := &testUpstream{stream: "data: [DONE]\n\n"}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	chash, _ := contextHash([]chatMessage{msg("user", "widgets cached")})
	seed(t, g, &slice.Slice{
		ID: "l3-s", Type: slice.Result, Scope: slice.Project,
		Content: []byte("widgets cached widgets cached stream answer"),
		Meta:    slice.SliceMeta{L3Safe: true, ContextHash: chash, Model: "deepseek-chat", Origin: slice.OriginSessionAuto},
	})

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "widgets cached", true))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	body := string(out)
	if resp.Header.Get("x-semantix-cache") != "hit" {
		t.Errorf("x-semantix-cache = %q, want hit", resp.Header.Get("x-semantix-cache"))
	}
	if !strings.Contains(body, "cached stream answer") || !strings.Contains(body, "[DONE]") {
		t.Errorf("replay stream missing content or terminator: %s", body)
	}
	if n := up.callCount(); n != 0 {
		t.Errorf("upstream calls = %d, want 0", n)
	}
}

// ---------------------------------------------------------------------------
// sidecar write memory (design §3.7)

func TestE2ESidecarWrittenAndIngested(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"fresh reply"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	_, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "brand new topic", false))
	if respContent(t, out) != "fresh reply" {
		t.Fatalf("unexpected passthrough: %s", out)
	}

	// sidecar JSONL exists with the request + response turns
	dir := g.cfg.Ingest.SessionsDir
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("no sidecar files in %s: %v", dir, err)
	}
	path := filepath.Join(dir, entries[0].Name())
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "brand new topic") || !strings.Contains(string(raw), "fresh reply") {
		t.Errorf("sidecar missing turns: %s", raw)
	}

	// async ingest eventually extracts slices into the store
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		items, err := g.store.ListAll()
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range items {
			if s.ID == "l3-a" || s.ID == "l2-a" { // seeds of other tests
				continue
			}
			if bytes.Contains(s.Content, []byte("brand new topic")) {
				return // ingested
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Error("async ingest did not extract the session into the store within 3s")
}

// ---------------------------------------------------------------------------
// scope header override

func TestE2EScopeHeader(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions",
		strings.NewReader(chatBody("deepseek-chat", "scope test", false)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("x-semantix-scope", "user")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("scope header: status=%d body=%s", resp.StatusCode, out)
	}
	if sc, err := g.resolveScope(req); err != nil || sc != slice.User {
		t.Errorf("scope not resolved to user: %v (%v)", sc, err)
	}

	// invalid scope header is rejected
	req2, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions",
		strings.NewReader(chatBody("deepseek-chat", "scope test", false)))
	req2.Header.Set("Authorization", "Bearer test-key")
	req2.Header.Set("x-semantix-scope", "bogus")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	out2, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("bad scope header: status=%d body=%s", resp2.StatusCode, out2)
	}
}

var _ = fmt.Sprintf

// ---------------------------------------------------------------------------
// model alias mapping (review fix: upstream_model must reach the upstream)

func TestE2EAliasMapping(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"mapped"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	g.cfg.Upstreams[0].ModelAlias = []string{"client-model"}
	g.cfg.Upstreams[0].UpstreamModel = "real-model"
	srv := httptest.NewServer(g)
	defer srv.Close()

	_, out := postChat(t, srv, "test-key", chatBody("client-model", "hi", false))
	if respContent(t, out) != "mapped" {
		t.Fatalf("unexpected: %s", out)
	}
	up.mu.Lock()
	model, _ := up.lastBody["model"].(string)
	up.mu.Unlock()
	if model != "real-model" {
		t.Errorf("forwarded model = %q, want real-model (alias must be mapped)", model)
	}
}

// ---------------------------------------------------------------------------
// L3 context isolation (review fix: different history must not reuse)

func TestE2ECrossContextIsolation(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"fresh"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	messagesA := []chatMessage{msg("system", "project alpha"), msg("user", "hello world cached")}
	chashA, _ := contextHash(messagesA)
	seed(t, g, &slice.Slice{
		ID: "l3-ctx", Type: slice.Result, Scope: slice.Project,
		Content: []byte("hello world cached answer from context A"),
		Meta: slice.SliceMeta{
			L3Safe: true, ContextHash: chashA, Model: "deepseek-chat",
		},
	})

	body := func(messages []chatMessage) string {
		raw, _ := json.Marshal(map[string]any{
			"model": "deepseek-chat", "messages": messages,
		})
		return string(raw)
	}

	// same context → hit, zero upstream calls
	resp, out := postChat(t, srv, "test-key", body(messagesA))
	if resp.Header.Get("x-semantix-cache") != "hit" {
		t.Errorf("same context: cache = %q, want hit (%s)", resp.Header.Get("x-semantix-cache"), out)
	}

	// different system prompt (different context) → miss, upstream called
	other := []chatMessage{msg("system", "project beta"), msg("user", "hello world cached")}
	resp2, out2 := postChat(t, srv, "test-key", body(other))
	if resp2.Header.Get("x-semantix-cache") != "miss" {
		t.Errorf("different context: cache = %q, want miss (%s)", resp2.Header.Get("x-semantix-cache"), out2)
	}
	if n := up.callCount(); n != 1 {
		t.Errorf("upstream calls = %d, want exactly 1 (only the different-context request)", n)
	}
}

// ---------------------------------------------------------------------------
// L3Safe=false rejection (design §3.5: gateway results are not L3 by default)

func TestE2EL3SafeFalseRejected(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"not cached"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	seed(t, g, &slice.Slice{
		ID: "l3-unsafe", Type: slice.Result, Scope: slice.Project,
		Content: []byte("hello world hello world unsafe answer"),
		Meta:    slice.SliceMeta{L3Safe: false}, // no deps + not opted in
	})

	resp, _ := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))
	if resp.Header.Get("x-semantix-cache") != "miss" {
		t.Errorf("cache = %q, want miss (L3Safe=false must not be served)", resp.Header.Get("x-semantix-cache"))
	}
	if n := up.callCount(); n != 1 {
		t.Errorf("upstream calls = %d, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// Anthropic vendor e2e (Issue #185): a fake /v1/messages upstream behind the
// full pipeline. The client surface must stay OpenAI-compatible while the
// upstream hop speaks Anthropic protocol (headers + body + response shape).

// testAnthropicUpstream simulates POST {base}/v1/messages: it records the
// request (headers + body) and answers with canned Anthropic responses.
type testAnthropicUpstream struct {
	mu         sync.Mutex
	calls      int
	lastPath   string
	lastBody   map[string]any
	lastHeader http.Header
	stream     string // canned Anthropic SSE body when stream=true
	plain      string // canned /v1/messages JSON otherwise
}

func (u *testAnthropicUpstream) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		_ = json.Unmarshal(body, &parsed)
		u.mu.Lock()
		u.calls++
		u.lastPath = r.URL.Path
		u.lastBody = parsed
		u.lastHeader = r.Header.Clone()
		u.mu.Unlock()
		if stream, _ := parsed["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, u.stream)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, u.plain)
	}
}

func (u *testAnthropicUpstream) snapshot() (int, string, map[string]any, http.Header) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls, u.lastPath, u.lastBody, u.lastHeader
}

const anthropicPlainReply = `{"id":"msg_e2e","type":"message","role":"assistant","model":"claude-sonnet-4",
	"content":[{"type":"text","text":"claude hello"}],
	"stop_reason":"end_turn",
	"usage":{"input_tokens":50,"output_tokens":10,"cache_read_input_tokens":20}}`

// newAnthropicGateway wires a gateway whose only upstream is vendor=anthropic
// (base_url points at the fake /v1/messages server).
func newAnthropicGateway(t *testing.T, upURL string) *Gateway {
	t.Helper()
	dir := t.TempDir()
	cfg := DefaultConfig()
	cfg.Server.GatewayKey = "test-key"
	cfg.Store.DB = filepath.Join(dir, "db.jsonl")
	cfg.Store.DepsRoot = dir
	cfg.Ingest.SessionsDir = filepath.Join(dir, "sessions")
	cfg.Ingest.UsageLog = filepath.Join(dir, "usage.jsonl")
	cfg.Upstreams = []UpstreamConfig{{
		Name: "claude", BaseURL: upURL, APIKey: "claude-key",
		ModelAlias: []string{"claude-sonnet"}, UpstreamModel: "claude-sonnet-4",
		Vendor: "anthropic",
	}}
	g, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g
}

func TestE2EAnthropicNonStreaming(t *testing.T) {
	up := &testAnthropicUpstream{plain: anthropicPlainReply}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newAnthropicGateway(t, upSrv.URL+"/v1") // base_url carries /v1 like api.anthropic.com
	srv := httptest.NewServer(g)
	defer srv.Close()

	resp, out := postChat(t, srv, "test-key", `{"model":"claude-sonnet","stream":false,"messages":[
		{"role":"system","content":"you are helpful"},
		{"role":"user","content":"hello claude"}
	]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	if got := respContent(t, out); got != "claude hello" {
		t.Errorf("content = %q, want claude hello (translated response)", got)
	}

	calls, path, last, hdr := up.snapshot()
	if calls != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls)
	}
	if path != "/v1/messages" {
		t.Errorf("upstream path = %q, want /v1/messages", path)
	}
	if hdr.Get("x-api-key") != "claude-key" || hdr.Get("anthropic-version") == "" {
		t.Errorf("anthropic auth headers wrong: x-api-key=%q version=%q", hdr.Get("x-api-key"), hdr.Get("anthropic-version"))
	}
	if hdr.Get("Authorization") != "" {
		t.Errorf("anthropic upstream must not receive Authorization: %q", hdr.Get("Authorization"))
	}
	if model, _ := last["model"].(string); model != "claude-sonnet-4" {
		t.Errorf("forwarded model = %q, want upstream model claude-sonnet-4", model)
	}
	// system lifted out of messages into the top-level field; user message
	// carries the query
	if sys, _ := last["system"].(string); sys != "you are helpful" {
		t.Errorf("system = %#v, want lifted system string", last["system"])
	}
	msgs, _ := last["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1 (system lifted): %v", len(msgs), msgs)
	}
}

func TestE2EAnthropicStreaming(t *testing.T) {
	up := &testAnthropicUpstream{stream: `event: message_start
data: {"type":"message_start","message":{"id":"msg_s","type":"message","role":"assistant","model":"claude-sonnet-4"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"stream "}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"reply"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":10}}

event: message_stop
data: {"type":"message_stop"}
`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newAnthropicGateway(t, upSrv.URL+"/v1")
	srv := httptest.NewServer(g)
	defer srv.Close()

	resp, out := postChat(t, srv, "test-key", chatBody("claude-sonnet", "stream please", true))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	body := string(out)
	for _, want := range []string{`"role":"assistant"`, `"content":"stream "`, `"content":"reply"`, `"finish_reason":"stop"`, "data: [DONE]"} {
		if !strings.Contains(body, want) {
			t.Errorf("stream missing %q:\n%s", want, body)
		}
	}
	// no raw anthropic events may leak through
	if strings.Contains(body, "message_start") || strings.Contains(body, "content_block") {
		t.Errorf("raw anthropic events leaked to client:\n%s", body)
	}
}

func TestE2EAnthropicL2Breakpoint(t *testing.T) {
	up := &testAnthropicUpstream{plain: anthropicPlainReply}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newAnthropicGateway(t, upSrv.URL+"/v1")
	srv := httptest.NewServer(g)
	defer srv.Close()

	// L2 injection material: seed the reuse slices so Build(query) returns a
	// non-empty injection block (same distractor trick as the OpenAI e2e).
	for i, content := range []string{"alpha", "bravo", "charlie", "delta", "widgets knowhow"} {
		seed(t, g, &slice.Slice{
			ID: fmt.Sprintf("an-th-%d", i), Type: slice.Prompt, Scope: slice.Project,
			Content: []byte(content),
		})
	}

	resp, out := postChat(t, srv, "test-key", chatBody("claude-sonnet", "widgets", false))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	if resp.Header.Get("x-semantix-cache") != "miss" {
		t.Errorf("x-semantix-cache = %q, want miss", resp.Header.Get("x-semantix-cache"))
	}

	calls, _, last, _ := up.snapshot()
	if calls != 1 {
		t.Fatalf("upstream calls = %d, want 1", calls)
	}
	sys, ok := last["system"].([]any)
	if !ok || len(sys) != 1 {
		t.Fatalf("system = %#v, want block array with one text block", last["system"])
	}
	block, ok := sys[0].(map[string]any)
	if !ok || block["type"] != "text" {
		t.Fatalf("system block = %#v, want text block", sys[0])
	}
	text, _ := block["text"].(string)
	if !strings.Contains(text, "[semantix-reuse]") {
		t.Errorf("injection block not appended to system: %q", text)
	}
	cc, _ := block["cache_control"].(map[string]any)
	if cc == nil || cc["type"] != "ephemeral" {
		t.Errorf("system tail missing cache_control breakpoint: %#v", block)
	}
}

// GW2: streaming sidecar memory (Issue #182)
// streamThrough must aggregate choices[0].delta.content while relaying
// verbatim, write the complete assistant reply into the sidecar (same shape
// as the non-streaming path), and fail closed on aborted streams.

func TestE2EStreamSidecarAggregatesContent(t *testing.T) {
	// role-first chunk + three content chunks + finish_reason + [DONE]
	up := &testUpstream{stream: "" +
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"},\"index\":0}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"hello \"},\"index\":0}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"streaming \"},\"index\":0}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"world\"},\"index\":0}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\",\"index\":0}]}\n\n" +
		"data: [DONE]\n\n"}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	resp, out := postChatWithHeaders(t, srv, "test-key",
		map[string]string{"x-semantix-session": "stream-sess-1"},
		chatBody("deepseek-chat", "stream me", true))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	// relay stays verbatim
	if !strings.Contains(string(out), `"content":"world"`) || !strings.Contains(string(out), "[DONE]") {
		t.Errorf("stream not relayed verbatim: %s", out)
	}

	// sidecar ends with the aggregated assistant reply (non-streaming shape)
	lines := readSidecarLines(t, g, "stream-sess-1")
	last := lines[len(lines)-1]
	if last["role"] != "assistant" {
		t.Fatalf("last sidecar line role = %v, want assistant: %#v", last["role"], lines)
	}
	if got := last["content"]; got != "hello streaming world" {
		t.Errorf("aggregated content = %q, want %q", got, "hello streaming world")
	}
}

func TestE2EStreamSidecarSkipsToolCalls(t *testing.T) {
	// tool_calls delta chunks interleaved with content: only content is
	// aggregated, matching the non-streaming extractAssistantContent shape
	up := &testUpstream{stream: "" +
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get_weather\",\"arguments\":\"\"}}]},\"index\":0}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"Let me check the weather.\"},\"index\":0}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"city\\\":\\\"Berlin\\\"}\"}}]},\"index\":0}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\",\"index\":0}]}\n\n" +
		"data: [DONE]\n\n"}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	_, out := postChatWithHeaders(t, srv, "test-key",
		map[string]string{"x-semantix-session": "tool-sess-1"},
		chatBody("deepseek-chat", "weather", true))
	if !strings.Contains(string(out), "[DONE]") {
		t.Fatalf("stream not relayed: %s", out)
	}

	lines := readSidecarLines(t, g, "tool-sess-1")
	last := lines[len(lines)-1]
	if last["role"] != "assistant" || last["content"] != "Let me check the weather." {
		t.Fatalf("last line = %#v, want assistant with plain content only", last)
	}
	if _, has := last["tool_calls"]; has {
		t.Errorf("sidecar assistant line carries tool_calls, want skipped: %#v", last)
	}
}

func TestE2EStreamAbortFailClosed(t *testing.T) {
	// upstream dies mid-stream: the partially aggregated reply must never
	// reach the sidecar (fail-closed, half replies are not reusable)
	up := &testUpstream{streamFunc: func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"half reply"}}]}`+"\n\n")
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
	}}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	_, _ = postChatWithHeaders(t, srv, "test-key",
		map[string]string{"x-semantix-session": "abort-sess-1"},
		chatBody("deepseek-chat", "do it", true))

	if _, err := os.Stat(filepath.Join(g.cfg.Ingest.SessionsDir, "abort-sess-1.jsonl")); err == nil {
		t.Error("sidecar exists after aborted stream, want no write (fail-closed)")
	}
}

func TestE2EStreamNoTerminatorFailClosed(t *testing.T) {
	// upstream ends the stream cleanly but never sends [DONE] or a
	// finish_reason: without a termination signal nothing is persisted
	up := &testUpstream{streamFunc: func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"no terminator"}}]}`+"\n\n")
	}}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	_, _ = postChatWithHeaders(t, srv, "test-key",
		map[string]string{"x-semantix-session": "noterm-sess-1"},
		chatBody("deepseek-chat", "go on", true))

	if _, err := os.Stat(filepath.Join(g.cfg.Ingest.SessionsDir, "noterm-sess-1.jsonl")); err == nil {
		t.Error("sidecar exists without a termination signal, want no write (fail-closed)")
	}
}

func TestE2EStreamAccumulatesL3(t *testing.T) {
	// the full loop (issue #182 acceptance e2e): streaming request →
	// sidecar with complete assistant → ingest → second identical request
	// hits L3 with zero additional upstream calls.
	// Query is multi-word on purpose: the ingest chain also lands a Prompt
	// slice (a copy of the user message) that would otherwise top the BM25
	// ranking below the zone absolute floor (AbsHigh 0.7), fail-closing L3.
	up := &testUpstream{stream: "" +
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"},\"index\":0}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"weather widgets forecast today \"},\"index\":0}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"weather widgets forecast today streaming answer\"},\"index\":0}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\",\"index\":0}]}\n\n" +
		"data: [DONE]\n\n"}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	g.cfg.Ingest.L3SafeDefault = true // gateway results enter L3 only when opted in
	srv := httptest.NewServer(g)
	defer srv.Close()

	body := chatBody("deepseek-chat", "weather widgets forecast today", true)
	resp, out := postChatWithHeaders(t, srv, "test-key",
		map[string]string{"x-semantix-session": "l3-accum-1"}, body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(out), "[DONE]") {
		t.Fatalf("first stream: status=%d body=%s", resp.StatusCode, out)
	}

	// async ingest must land a Result slice carrying the aggregated reply.
	// Poll the INDEX, not the store: ingest.Pipeline persists (Store.Put)
	// before indexing (Index.Insert), and the second request's L3 lookup
	// only sees indexed slices — polling the store can pass inside that
	// gap and race the lookup (flaked on CI, see PR #234 run 32330979045).
	deadline := time.Now().Add(3 * time.Second)
	ingested := false
	for time.Now().Before(deadline) {
		hits, err := g.index.Search("weather widgets forecast today", 5, g.injector.Scope)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range hits {
			if h.Slice.Type == slice.Result && bytes.Contains(h.Slice.Content, []byte("streaming answer")) {
				ingested = true
				break
			}
		}
		if ingested {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ingested {
		t.Fatal("streamed assistant reply was not indexed as a Result slice within 3s")
	}

	// second identical request: L3 hit, zero additional upstream calls
	resp2, out2 := postChatWithHeaders(t, srv, "test-key",
		map[string]string{"x-semantix-session": "l3-accum-1"}, body)
	if resp2.Header.Get("x-semantix-cache") != "hit" {
		t.Errorf("second request cache = %q, want hit (%s)", resp2.Header.Get("x-semantix-cache"), out2)
	}
	if !strings.Contains(string(out2), "streaming answer") || !strings.Contains(string(out2), "[DONE]") {
		t.Errorf("replayed stream missing content or terminator: %s", out2)
	}
	if n := up.callCount(); n != 1 {
		t.Errorf("upstream calls = %d, want 1 (second request must not reach upstream)", n)
	}
}

// ---------------------------------------------------------------------------
// GW7: streaming usage accounting (Issue #187)
// streamThrough must synthesize a usage chunk before [DONE] when the
// upstream carried none (carrying injection stats, flagged bytes/4), leave
// upstream-provided usage verbatim, and never meter half streams.

func TestE2EStreamUsageChunkSynthesized(t *testing.T) {
	up := &testUpstream{stream: "" +
		"data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"},\"index\":0}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"hello \"},\"index\":0}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"streaming world\"},\"index\":0}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\",\"index\":0}]}\n\n" +
		"data: [DONE]\n\n"}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	// seed so L2 injection happens: the synthesized chunk must carry the
	// injection statistics (cached_tokens > 0), not a flat zero
	seed(t, g, &slice.Slice{
		ID: "l2-gw7", Type: slice.Prompt, Scope: slice.Project,
		Content: []byte("prior knowledge about widgets"),
	})
	for i, content := range []string{"alpha", "bravo", "charlie", "delta"} {
		seed(t, g, &slice.Slice{
			ID: fmt.Sprintf("gw7-distractor-%d", i), Type: slice.Prompt, Scope: slice.Project,
			Content: []byte(content),
		})
	}
	srv := httptest.NewServer(g)
	defer srv.Close()

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "widgets", true))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	body := string(out)
	usageIdx := strings.Index(body, `"usage"`)
	doneIdx := strings.Index(body, "data: [DONE]")
	if usageIdx < 0 {
		t.Fatalf("no synthesized usage chunk in stream: %s", body)
	}
	if doneIdx < 0 || usageIdx > doneIdx {
		t.Errorf("usage chunk must precede [DONE] (usage@%d done@%d)", usageIdx, doneIdx)
	}
	if !strings.Contains(body, `"estimator":"bytes/4"`) {
		t.Errorf("usage chunk missing estimator flag: %s", body)
	}
	if strings.Contains(body, `"cached_tokens":0`) {
		t.Errorf("usage chunk missing injection stats (cached_tokens=0), want > 0: %s", body)
	}
}

func TestE2EStreamUsageChunkNotDuplicated(t *testing.T) {
	// upstream provides its own usage: relay verbatim, no synthesis
	up := &testUpstream{stream: "" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"index\":0}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2,\"total_tokens\":12}}\n\n" +
		"data: [DONE]\n\n"}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	_, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hi", true))
	body := string(out)
	if !strings.Contains(body, `"prompt_tokens":10`) {
		t.Errorf("upstream usage not relayed: %s", body)
	}
	if strings.Contains(body, "estimator") {
		t.Errorf("upstream usage must not gain the estimator flag: %s", body)
	}
	if n := strings.Count(body, `"usage"`); n != 1 {
		t.Errorf("usage field count = %d, want exactly 1 (no synthesized duplicate): %s", n, body)
	}
}

func TestE2EStreamUsageChunkSkippedOnAbort(t *testing.T) {
	// upstream dies mid-stream: [DONE] is appended for the client (U28)
	// but no usage chunk — half streams must not be metered
	up := &testUpstream{streamFunc: func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"half reply"}}]}`+"\n\n")
		if fl, ok := w.(http.Flusher); ok {
			fl.Flush()
		}
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
			}
		}
	}}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	_, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "do it", true))
	body := string(out)
	if !strings.Contains(body, "data: [DONE]") {
		t.Errorf("aborted stream missing appended [DONE]: %s", body)
	}
	if strings.Contains(body, `"usage"`) {
		t.Errorf("aborted stream must not carry a usage chunk: %s", body)
	}
}

func TestE2EL3StreamingReplayHasUsage(t *testing.T) {
	up := &testUpstream{stream: "data: [DONE]\n\n"}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	chash, _ := contextHash([]chatMessage{msg("user", "widgets cached")})
	seed(t, g, &slice.Slice{
		ID: "l3-gw7", Type: slice.Result, Scope: slice.Project,
		Content: []byte("widgets cached widgets cached stream answer"),
		Meta:    slice.SliceMeta{L3Safe: true, ContextHash: chash, Model: "deepseek-chat", Origin: slice.OriginSessionAuto},
	})

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "widgets cached", true))
	if resp.Header.Get("x-semantix-cache") != "hit" {
		t.Fatalf("cache = %q, want hit", resp.Header.Get("x-semantix-cache"))
	}
	body := string(out)
	if !strings.Contains(body, `"estimator":"bytes/4"`) || !strings.Contains(body, `"cached_tokens"`) {
		t.Errorf("replayed stream missing synthetic usage: %s", body)
	}
	if usageIdx, doneIdx := strings.Index(body, `"usage"`), strings.Index(body, "data: [DONE]"); usageIdx < 0 || doneIdx < 0 || usageIdx > doneIdx {
		t.Errorf("usage chunk must precede [DONE] (usage@%d done@%d)", usageIdx, doneIdx)
	}
}

func TestE2EL3HitSyntheticUsageEstimator(t *testing.T) {
	up := &testUpstream{plain: `{}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	chash, _ := contextHash([]chatMessage{msg("user", "hello world")})
	seed(t, g, &slice.Slice{
		ID: "l3-gw7b", Type: slice.Result, Scope: slice.Project,
		Content: []byte("hello world hello world cached answer"),
		Meta:    slice.SliceMeta{L3Safe: true, ContextHash: chash, Model: "deepseek-chat", Origin: slice.OriginSessionAuto},
	})

	_, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))
	if !strings.Contains(string(out), `"estimator":"bytes/4"`) {
		t.Errorf("synthetic L3 usage missing estimator: %s", out)
	}
}

func TestE2EStreamUsageChunkNoFinishReason(t *testing.T) {
	// upstream ends with content chunks then bare [DONE] (no finish_reason
	// chunk): the usage chunk must still be synthesized before [DONE]
	up := &testUpstream{stream: "" +
		"data: {\"id\":\"chatcmpl-abc\",\"choices\":[{\"delta\":{\"content\":\"plain \"},\"index\":0}]}\n\n" +
		"data: {\"id\":\"chatcmpl-abc\",\"choices\":[{\"delta\":{\"content\":\"done\"},\"index\":0}]}\n\n" +
		"data: [DONE]\n\n"}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	_, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "go", true))
	body := string(out)
	usageIdx := strings.Index(body, `"usage"`)
	doneIdx := strings.Index(body, "data: [DONE]")
	if usageIdx < 0 || doneIdx < 0 {
		t.Fatalf("usage or [DONE] missing: %s", body)
	}
	if usageIdx > doneIdx {
		t.Errorf("usage chunk must precede [DONE] (usage@%d done@%d)", usageIdx, doneIdx)
	}
	if !strings.Contains(body, `"id":"chatcmpl-abc"`) {
		t.Errorf("usage chunk should reuse the stream id: %s", body)
	}
}

// ---------------------------------------------------------------------------
// GW3: /healthz upstream probe (Issue #183)
// With health_timeout_seconds > 0 the gateway must probe every upstream
// (GET {base_url}/models, 2xx = healthy); any failure → 503 + envelope.

func TestE2EHealthProbeHealthy(t *testing.T) {
	up := &testUpstream{plain: `{}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(out), `"status":"ok"`) {
		t.Errorf("healthz: status=%d body=%s, want 200 ok", resp.StatusCode, out)
	}
	if n := up.callCount(); n != 1 {
		t.Errorf("upstream calls = %d, want 1 (probe hit /models)", n)
	}
}

func TestE2EHealthProbeUnreachable(t *testing.T) {
	// upstream server already closed: the probe must fail closed with 503
	upSrv := httptest.NewServer(http.NotFoundHandler())
	deadURL := upSrv.URL
	upSrv.Close()
	g := newTestGateway(t, deadURL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("healthz: status=%d body=%s, want 503", resp.StatusCode, out)
	}
	if !strings.Contains(string(out), "upstream_error") || !strings.Contains(string(out), "unreachable") {
		t.Errorf("healthz body missing upstream_error/unreachable: %s", out)
	}
	if strings.Contains(string(out), deadURL) {
		t.Errorf("healthz body leaks the upstream URL: %s", out)
	}
}

func TestE2EHealthProbeAnyUpstreamDown(t *testing.T) {
	alive := &testUpstream{plain: `{}`}
	aliveSrv := httptest.NewServer(alive.handler())
	defer aliveSrv.Close()
	g := newTestGateway(t, aliveSrv.URL)
	// append a second, dead upstream: any failure marks the gateway down
	deadSrv := httptest.NewServer(http.NotFoundHandler())
	deadURL := deadSrv.URL
	deadSrv.Close()
	g.cfg.Upstreams = append(g.cfg.Upstreams, UpstreamConfig{
		Name: "dead", BaseURL: deadURL, APIKey: "up-key",
		ModelAlias: []string{"dead-model"}, UpstreamModel: "dead-model", Vendor: "openai",
	})
	srv := httptest.NewServer(g)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("healthz: status=%d body=%s, want 503 (one dead upstream)", resp.StatusCode, out)
	}
	if !strings.Contains(string(out), "dead") {
		t.Errorf("healthz body should name the failing upstream: %s", out)
	}
}

func TestE2EHealthProbeTimeout(t *testing.T) {
	up := &testUpstream{plain: `{}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	g.cfg.Server.HealthTimeoutSeconds = 1
	g.healthProbe = func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	}
	srv := httptest.NewServer(g)
	defer srv.Close()

	start := time.Now()
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("healthz: status=%d body=%s, want 503 on probe timeout", resp.StatusCode, out)
	}
	if !strings.Contains(string(out), "deadline exceeded") {
		t.Errorf("healthz body should report the timeout cause: %s", out)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("healthz took %v, want bounded by health_timeout_seconds=1", elapsed)
	}
}

func TestE2EHealthProbeDisabled(t *testing.T) {
	// health_timeout_seconds = 0 keeps the old behavior: local readiness
	// only, zero upstream traffic
	up := &testUpstream{plain: `{}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	g.cfg.Server.HealthTimeoutSeconds = 0
	srv := httptest.NewServer(g)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(out), `"status":"ok"`) {
		t.Errorf("healthz with probe disabled: status=%d body=%s, want 200 ok", resp.StatusCode, out)
	}
	if n := up.callCount(); n != 0 {
		t.Errorf("upstream calls = %d, want 0 (probe disabled must not touch upstream)", n)
	}
}

// ---------------------------------------------------------------------------
// L3 suspected false hit (Issue #262 §3.3): a same-session retry of a
// served query bypasses L3, reaches upstream, and is recorded in usage.

func TestE2EFalseHitRetryBypassesL3(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"fresh upstream answer"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	chash, _ := contextHash([]chatMessage{msg("user", "hello world")})
	seed(t, g, &slice.Slice{
		ID: "l3-a", Type: slice.Result, Scope: slice.Project,
		Content: []byte("hello world hello world cached answer"),
		Meta:    slice.SliceMeta{L3Safe: true, ContextHash: chash, Model: "deepseek-chat", Origin: slice.OriginSessionAuto},
	})

	hdr := map[string]string{"x-semantix-session": "fh-sess-1"}
	// First request: L3 hit, zero upstream calls.
	resp, out := postChatWithHeaders(t, srv, "test-key", hdr, chatBody("deepseek-chat", "hello world", false))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("first status = %d, body %s", resp.StatusCode, out)
	}
	if resp.Header.Get("x-semantix-cache") != "hit" {
		t.Fatalf("first x-semantix-cache = %q, want hit", resp.Header.Get("x-semantix-cache"))
	}
	if n := up.callCount(); n != 0 {
		t.Fatalf("upstream calls after first request = %d, want 0", n)
	}

	// Same-session near-identical retry: L3 must be bypassed → upstream.
	resp2, out2 := postChatWithHeaders(t, srv, "test-key", hdr, chatBody("deepseek-chat", "hello world", false))
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("retry status = %d, body %s", resp2.StatusCode, out2)
	}
	if resp2.Header.Get("x-semantix-cache") != "miss" {
		t.Fatalf("retry x-semantix-cache = %q, want miss (bypass)", resp2.Header.Get("x-semantix-cache"))
	}
	if got := respContent(t, out2); got != "fresh upstream answer" {
		t.Fatalf("retry content = %q, want fresh upstream answer", got)
	}
	if n := up.callCount(); n != 1 {
		t.Fatalf("upstream calls after retry = %d, want 1", n)
	}

	// The negative signal lands in the usage log as a suspected false hit.
	summary, err := usage.Summarize(g.cfg.Ingest.UsageLog, usage.DefaultCostMissPerMTok, usage.DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	if summary.L3Reuses != 1 {
		t.Fatalf("usage l3 reuses = %d, want 1", summary.L3Reuses)
	}
	if summary.L3FalseHits != 1 {
		t.Fatalf("usage false hits = %d, want 1", summary.L3FalseHits)
	}
}

// ---------------------------------------------------------------------------
// ablation switch: SEMANTIX_GATEWAY_DISABLE covers every memory mechanism

// TestE2EAblationSwitchDisablesMemory pins the OFF-arm contract for two-arm
// experiments: a disabled gateway forwards verbatim and keeps usage + sidecar
// accounting, but never injects L2 context and never grows the slice library.
func TestE2EAblationSwitchDisablesMemory(t *testing.T) {
	t.Setenv("SEMANTIX_GATEWAY_DISABLE", "1")
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"upstream reply"}}],"usage":{"prompt_tokens":5,"completion_tokens":3}}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	// Same corpus as TestE2EL2InjectionForwarded: on an enabled gateway the
	// "widgets" query produces an L2 slice hit, so a zero here is the switch.
	seed(t, g, &slice.Slice{
		ID: "l2-off", Type: slice.Prompt, Scope: slice.Project,
		Content: []byte("prior knowledge about widgets"),
	})
	for i, content := range []string{"alpha", "bravo", "charlie", "delta"} {
		seed(t, g, &slice.Slice{
			ID: fmt.Sprintf("off-distractor-%d", i), Type: slice.Prompt, Scope: slice.Project,
			Content: []byte(content),
		})
	}
	before, err := g.store.ListAll()
	if err != nil {
		t.Fatal(err)
	}

	resp, out := postChatWithHeaders(t, srv, "test-key",
		map[string]string{"x-semantix-session": "off-arm"},
		chatBody("deepseek-chat", "widgets", false))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	if got := respContent(t, out); got != "upstream reply" {
		t.Errorf("content = %q, want upstream reply (passthrough)", got)
	}
	if n := up.callCount(); n != 1 {
		t.Fatalf("upstream calls = %d, want 1", n)
	}
	for _, m := range up.forwardedMessages() {
		s, _ := m["content"].(string)
		if strings.Contains(s, "[semantix-reuse]") {
			t.Errorf("disabled gateway forwarded an injection block: %#v", m)
		}
	}

	// Sidecar transcript is still written (arms stay auditable offline)…
	lines := readSidecarLines(t, g, "off-arm")
	if len(lines) == 0 {
		t.Fatal("disabled gateway must still write the session sidecar")
	}
	// …usage still accounts the exchange, with zero slice hits…
	summary, err := usage.Summarize(g.cfg.Ingest.UsageLog, usage.DefaultCostMissPerMTok, usage.DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	if summary.SliceHits != 0 {
		t.Errorf("usage slice hits = %d, want 0", summary.SliceHits)
	}
	// …and the library never grows: ingest is skipped, not merely delayed.
	time.Sleep(500 * time.Millisecond)
	after, err := g.store.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Errorf("slice store grew from %d to %d on a disabled gateway", len(before), len(after))
	}
}
