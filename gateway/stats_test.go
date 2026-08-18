package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"semantix/kernel/slice"
)

// An L3 hit must land in the served slice's stats (Hits+1, LastUsed set) —
// the raw material the value scorer消费 (spec slice-value-eviction §3).
func TestE2EL3HitRecordsSliceStats(t *testing.T) {
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
		Meta:    slice.SliceMeta{L3Safe: true, ContextHash: chash, Model: "deepseek-chat"},
	})

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))
	if resp.StatusCode != http.StatusOK || resp.Header.Get("x-semantix-cache") != "hit" {
		t.Fatalf("expected L3 hit, got status %d cache %q body %s",
			resp.StatusCode, resp.Header.Get("x-semantix-cache"), out)
	}
	got, err := g.store.Get("l3-a")
	if err != nil || got == nil {
		t.Fatalf("get served slice: %v %v", got, err)
	}
	if got.Stats.Hits != 1 || got.Stats.LastUsed == 0 {
		t.Fatalf("L3 hit not recorded: %+v", got.Stats)
	}
}

// An L2 injection must record Injected+1 on exactly the injected slices —
// distractors that were not in the block stay untouched.
func TestE2EL2InjectionRecordsSliceStats(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"upstream reply"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	seed(t, g, &slice.Slice{
		ID: "l2-a", Type: slice.Prompt, Scope: slice.Project,
		Content: []byte("prior knowledge about widgets"),
	})
	for i, content := range []string{"alpha", "bravo", "charlie", "delta"} {
		seed(t, g, &slice.Slice{
			ID: "distractor-" + content, Type: slice.Prompt, Scope: slice.Project,
			Content: []byte(content + " filler"),
		})
		_ = i
	}

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "widgets", false))
	if resp.StatusCode != http.StatusOK || resp.Header.Get("x-semantix-cache") != "miss" {
		t.Fatalf("expected forwarded miss, got status %d cache %q body %s",
			resp.StatusCode, resp.Header.Get("x-semantix-cache"), out)
	}
	got, err := g.store.Get("l2-a")
	if err != nil || got == nil {
		t.Fatalf("get injected slice: %v %v", got, err)
	}
	if got.Stats.Injected != 1 || got.Stats.LastUsed == 0 {
		t.Fatalf("L2 injection not recorded: %+v", got.Stats)
	}
	if got.Stats.Hits != 0 {
		t.Fatalf("injection must not count as a hit: %+v", got.Stats)
	}
	d, _ := g.store.Get("distractor-alpha")
	if d == nil || d.Stats.Injected != 0 {
		t.Fatalf("distractor wrongly credited: %+v", d)
	}
}
