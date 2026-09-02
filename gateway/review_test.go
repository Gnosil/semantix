package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

// TestToolCallMessagesPreservedThroughInjection: an L2 injection rewrites
// the messages array — tool_calls / name / tool_call_id fields must survive
// the rewrite or tool-calling conversations break upstream (regression).
func TestToolCallMessagesPreservedThroughInjection(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	seed(t, g, &slice.Slice{
		ID: "l2-tools", Type: slice.Prompt, Scope: slice.Project,
		Content: []byte("prior widgets knowledge to inject"),
	})
	// Keep the target's BM25 score above the absolute L2 hit threshold. Before
	// empty admissions stopped emitting marker-only blocks, this single-item
	// corpus made the test pass without injecting the seeded slice at all.
	for _, distractor := range []struct{ id, content string }{
		{"l2-tools-alpha", "alpha"},
		{"l2-tools-bravo", "bravo"},
		{"l2-tools-charlie", "charlie"},
		{"l2-tools-delta", "delta"},
	} {
		seed(t, g, &slice.Slice{
			ID: distractor.id, Type: slice.Prompt, Scope: slice.Project,
			Content: []byte(distractor.content),
		})
	}

	body := `{"model":"deepseek-chat","messages":[
		{"role":"user","content":"widgets please"},
		{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a\"}"}}]},
		{"role":"tool","tool_call_id":"call_1","name":"read_file","content":"file contents"}
	]}`
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	msgs := up.forwardedMessages()
	if len(msgs) != 4 {
		t.Fatalf("forwarded messages = %d, want 4 (injected system + 3 originals)", len(msgs))
	}
	// The tool message must keep its tool_call_id and name.
	toolMsg := msgs[3]
	if toolMsg["role"] != "tool" {
		t.Fatalf("tool message role = %v", toolMsg["role"])
	}
	if toolMsg["tool_call_id"] != "call_1" {
		t.Fatalf("tool_call_id lost in rewrite: %#v", toolMsg)
	}
	if toolMsg["name"] != "read_file" {
		t.Fatalf("tool name lost in rewrite: %#v", toolMsg)
	}
	// The assistant message must keep its tool_calls array.
	asstMsg := msgs[2]
	calls, ok := asstMsg["tool_calls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("tool_calls lost in rewrite: %#v", asstMsg)
	}
	first, ok := calls[0].(map[string]any)
	if !ok || first["id"] != "call_1" {
		t.Fatalf("tool call detail lost in rewrite: %#v", asstMsg)
	}
}

// TestStreamAbnormalEndGetsDone: an upstream stream that dies without
// [DONE] must still terminate the client stream (regression: clients hung
// forever on abnormal upstream disconnects).
func TestStreamAbnormalEndGetsDone(t *testing.T) {
	up := &testUpstream{stream: "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "stream me", true))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	body := string(out)
	if !strings.Contains(body, "partial") {
		t.Fatalf("stream content missing: %s", body)
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "data: [DONE]") {
		t.Fatalf("stream must be terminated with [DONE]: %q", body)
	}
}

// TestDisableFalseKeepsCache: SEMANTIX_GATEWAY_DISABLE=0 must NOT disable
// the semantic cache (regression: any non-empty value used to disable).
func TestDisableFalseKeepsCache(t *testing.T) {
	t.Setenv("SEMANTIX_GATEWAY_DISABLE", "0")
	if disableEnv() {
		t.Fatal("SEMANTIX_GATEWAY_DISABLE=0 must keep the cache enabled")
	}
	t.Setenv("SEMANTIX_GATEWAY_DISABLE", "false")
	if disableEnv() {
		t.Fatal("SEMANTIX_GATEWAY_DISABLE=false must keep the cache enabled")
	}
	t.Setenv("SEMANTIX_GATEWAY_DISABLE", "1")
	if !disableEnv() {
		t.Fatal("SEMANTIX_GATEWAY_DISABLE=1 must disable the cache")
	}
	t.Setenv("SEMANTIX_GATEWAY_DISABLE", "")
	if disableEnv() {
		t.Fatal("unset SEMANTIX_GATEWAY_DISABLE must keep the cache enabled")
	}
}

// TestAttachBlockLastSystemMessage: with multiple system messages the
// injection block lands at the END of the LAST one (design §3.6 system
// prompt tail), earlier system messages untouched.
func TestAttachBlockLastSystemMessage(t *testing.T) {
	msgs := []chatMessage{
		{Role: "system", Content: "first"},
		{Role: "user", Content: "u"},
		{Role: "system", Content: "second"},
	}
	out := attachBlock(msgs, "block")
	if len(out) != 3 {
		t.Fatalf("len = %d", len(out))
	}
	if out[0].Content != "first" {
		t.Fatalf("first system mutated: %v", out[0].Content)
	}
	if out[2].Content != "second\n\nblock" {
		t.Fatalf("last system = %v, want tail-injected", out[2].Content)
	}
}

// TestForwardRetriesNetworkError: a transport failure is retried once and
// the response succeeds (design §3.8 gateway best-effort retry).
func TestForwardRetriesNetworkError(t *testing.T) {
	var calls int
	upSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			// Force a transport error: hijack and kill the connection.
			if hj, ok := w.(http.Hijacker); ok {
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
				return
			}
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"after retry"}}]}`)
	}))
	defer upSrv.Close()

	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hi", false))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	if got := respContent(t, out); got != "after retry" {
		t.Fatalf("content = %q", got)
	}
	if calls != 2 {
		t.Fatalf("upstream calls = %d, want 2 (first transport-failed, retried)", calls)
	}
}

// TestHopByHopHeadersFiltered: connection-scoped headers from the upstream
// must not leak to the client.
func TestHopByHopHeadersFiltered(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`}
	upSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Keep-Alive", "timeout=5")
		w.Header().Set("X-Upstream-Marker", "kept")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, up.plain)
	}))
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	resp, _ := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hi", false))
	if resp.Header.Get("Connection") != "" {
		t.Errorf("Connection header leaked: %q", resp.Header.Get("Connection"))
	}
	if resp.Header.Get("Keep-Alive") != "" {
		t.Errorf("Keep-Alive header leaked: %q", resp.Header.Get("Keep-Alive"))
	}
	if resp.Header.Get("X-Upstream-Marker") != "kept" {
		t.Errorf("end-to-end header dropped: %q", resp.Header.Get("X-Upstream-Marker"))
	}
}
