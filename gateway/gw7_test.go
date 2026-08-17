package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

// ---------------------------------------------------------------------------
// Issue #187 / GW7: streaming usage block + token estimator

// TestE2EStreamUsageBlockInjectedWhenUpstreamOmitsUsage: a streaming upstream
// that never sends a usage event must get a synthetic usage block before
// [DONE], carrying the injection stats (spec §3.4) and the bytes/4 estimator.
func TestE2EStreamUsageBlockInjectedWhenUpstreamOmitsUsage(t *testing.T) {
	up := &testUpstream{stream: "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\ndata: [DONE]\n\n"}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	// Seed a Prompt slice so the L2 injector reports nonzero injection stats.
	seed(t, g, &slice.Slice{
		ID: "l2-gw7", Type: slice.Prompt, Scope: slice.Project,
		Content: []byte("prior widgets knowledge to inject"),
	})

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "widgets please", true))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	body := string(out)
	if !strings.Contains(body, "[DONE]") {
		t.Fatalf("stream missing [DONE]: %s", body)
	}
	// The synthetic usage block must appear before [DONE].
	usageIdx := strings.Index(body, `"usage":`)
	doneIdx := strings.Index(body, "data: [DONE]")
	if usageIdx < 0 {
		t.Fatalf("no usage block synthesized: %s", body)
	}
	if doneIdx < 0 || usageIdx > doneIdx {
		t.Fatalf("usage block (%d) must precede [DONE] (%d): %s", usageIdx, doneIdx, body)
	}
	if !strings.Contains(body, `"estimator":"bytes/4"`) {
		t.Errorf("usage block missing estimator: %s", body)
	}
	if !strings.Contains(body, `"cached_tokens"`) {
		t.Errorf("usage block missing cached_tokens (injection stats): %s", body)
	}
}

// TestE2EStreamUsageNotDuplicatedWhenUpstreamProvidesIt: when the upstream
// already sends a usage event, the gateway must not add a second one.
func TestE2EStreamUsageNotDuplicatedWhenUpstreamProvidesIt(t *testing.T) {
	up := &testUpstream{stream: "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":7,\"completion_tokens\":3,\"total_tokens\":10}}\n\ndata: [DONE]\n\n"}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "stream me", true))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	if got := strings.Count(string(out), `"usage"`); got != 1 {
		t.Errorf(`usage occurrences = %d, want exactly 1 (no duplication): %s`, got, out)
	}
	if strings.Contains(string(out), `"estimator"`) {
		t.Errorf("relayed upstream usage must not carry a gateway estimator: %s", out)
	}
}

// TestE2EL3HitUsageCarriesEstimator: the synthetic non-streaming L3 usage
// reports its byte-estimate origin so GW4 accounting knows the discrepancy.
func TestE2EL3HitUsageCarriesEstimator(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"unused"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	chash, _ := contextHash([]chatMessage{msg("user", "hello world")})
	seed(t, g, &slice.Slice{
		ID: "l3-gw7", Type: slice.Result, Scope: slice.Project,
		Content: []byte("hello world hello world cached answer"),
		Meta:    slice.SliceMeta{L3Safe: true, ContextHash: chash, Model: "deepseek-chat"},
	})

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	if resp.Header.Get("x-semantix-cache") != "hit" {
		t.Fatalf("x-semantix-cache = %q, want hit", resp.Header.Get("x-semantix-cache"))
	}
	var body struct {
		Usage struct {
			Estimator string `json:"estimator"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("parse response: %v", err)
	}
	if body.Usage.Estimator != "bytes/4" {
		t.Errorf(`usage.estimator = %q, want "bytes/4"`, body.Usage.Estimator)
	}
}
