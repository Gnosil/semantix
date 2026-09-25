package gateway

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	kernelevent "semantix/kernel/event"
	"semantix/kernel/slice"
)

func waitSliceStats(t *testing.T, g *Gateway, id string, ok func(slice.SliceStats) bool) slice.SliceStats {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, err := g.store.Get(id)
		if err == nil && got != nil && ok(got.Stats) {
			return got.Stats
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, err := g.store.Get(id)
	if err != nil || got == nil {
		t.Fatalf("get slice %q: slice=%v err=%v", id, got, err)
	}
	t.Fatalf("slice %q stats did not converge: %+v", id, got.Stats)
	return slice.SliceStats{}
}

func readKernelEvents(t *testing.T, g *Gateway, sessionID string) []kernelevent.Event {
	t.Helper()
	f, err := os.Open(filepath.Join(g.cfg.Ingest.SessionsDir, sessionID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var events []kernelevent.Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if e, err := kernelevent.FromJSON(sc.Bytes()); err == nil {
			events = append(events, e)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}

func TestE2EL3HitRecordsStatsAndEvent(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"unused"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	chash, _ := contextHash([]chatMessage{msg("user", "hello world")})
	seed(t, g, &slice.Slice{
		ID: "l3-stats", Type: slice.Result, Scope: slice.Project,
		Content: []byte("hello world hello world cached answer"),
		Meta:    slice.SliceMeta{L3Safe: true, ContextHash: chash, Model: "deepseek-chat", Origin: slice.OriginSessionAuto},
	})

	resp, out := postChatWithHeaders(t, srv, "test-key", map[string]string{
		"x-semantix-session": "stats-l3",
	}, chatBody("deepseek-chat", "hello world", false))
	if resp.StatusCode != http.StatusOK || resp.Header.Get("x-semantix-cache") != "hit" {
		t.Fatalf("expected L3 hit, got status %d cache %q body %s", resp.StatusCode, resp.Header.Get("x-semantix-cache"), out)
	}
	stats := waitSliceStats(t, g, "l3-stats", func(s slice.SliceStats) bool { return s.Hits == 1 && s.LastUsed > 0 })
	if stats.Injected != 0 {
		t.Fatalf("L3 hit counted as injection: %+v", stats)
	}
	events := readKernelEvents(t, g, "stats-l3")
	if len(events) != 1 || events[0].Kind != kernelevent.SliceHit {
		t.Fatalf("events=%+v, want one SliceHit", events)
	}
}

func TestE2EL2InjectionRecordsStatsAndEvent(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"upstream reply"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()

	seed(t, g, &slice.Slice{
		ID: "l2-stats", Type: slice.Prompt, Scope: slice.Project,
		Content: []byte("prior knowledge about widgets"),
	})
	for _, content := range []string{"alpha", "bravo", "charlie", "delta"} {
		seed(t, g, &slice.Slice{
			ID: "distractor-" + content, Type: slice.Prompt, Scope: slice.Project,
			Content: []byte(content),
		})
	}
	resp, out := postChatWithHeaders(t, srv, "test-key", map[string]string{
		"x-semantix-session": "stats-l2",
	}, chatBody("deepseek-chat", "widgets", false))
	if resp.StatusCode != http.StatusOK || resp.Header.Get("x-semantix-cache") != "miss" {
		t.Fatalf("expected forwarded miss, got status %d cache %q body %s", resp.StatusCode, resp.Header.Get("x-semantix-cache"), out)
	}
	stats := waitSliceStats(t, g, "l2-stats", func(s slice.SliceStats) bool { return s.Injected == 1 && s.LastUsed > 0 })
	if stats.Hits != 0 {
		t.Fatalf("L2 injection counted as hit: %+v", stats)
	}
	events := readKernelEvents(t, g, "stats-l2")
	found := false
	for _, e := range events {
		found = found || e.Kind == kernelevent.SliceInject
	}
	if !found {
		t.Fatalf("events=%+v, want SliceInject", events)
	}
}

func TestE2EL2FailedForwardDoesNotRecordDelivery(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "rejected", http.StatusBadRequest) }))
	defer up.Close()
	g := newTestGateway(t, up.URL)
	srv := httptest.NewServer(g)
	defer srv.Close()
	seed(t, g, &slice.Slice{ID: "selected", Type: slice.Prompt, Scope: slice.Project, Content: []byte("prior knowledge about widgets")})
	for _, s := range []string{"alpha", "bravo", "charlie", "delta"} {
		seed(t, g, &slice.Slice{ID: s, Type: slice.Prompt, Scope: slice.Project, Content: []byte(s)})
	}
	resp, _ := postChatWithHeaders(t, srv, "test-key", map[string]string{"x-semantix-session": "failed-l2"}, chatBody("deepseek-chat", "widgets", false))
	if resp.StatusCode == http.StatusOK {
		t.Fatal("fixture did not reject")
	}
	g.ingestWG.Wait()
	got, err := g.store.Get("selected")
	if err != nil {
		t.Fatal(err)
	}
	if got.Stats.Injected != 0 {
		t.Fatalf("upstream rejected but credited delivery: %+v", got.Stats)
	}
}
