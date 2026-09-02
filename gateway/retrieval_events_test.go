package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"semantix/kernel/slice"
)

// seedInjectableCorpus seeds one target slice plus distractors so the target
// clears the BM25 absolute hit threshold (single-document corpora score too
// low — same shape as TestE2EL2InjectionForwarded).
func seedInjectableCorpus(t *testing.T, g *Gateway) {
	t.Helper()
	seed(t, g, &slice.Slice{
		ID: "l2-a", Type: slice.Prompt, Scope: slice.Project,
		Content: []byte("prior knowledge about widgets"),
	})
	for i, content := range []string{"alpha", "bravo", "charlie", "delta"} {
		seed(t, g, &slice.Slice{
			ID: fmt.Sprintf("distractor-%d", i), Type: slice.Prompt, Scope: slice.Project,
			Content: []byte(content),
		})
	}
}

// TestRetrievalEventsLogRecordsDecisions: with [retrieval] events_log set,
// one chat turn that runs L2 retrieval must append one JSONL event carrying
// the query and the full per-candidate admission trace (Injection.Decisions),
// so offline training can replay exactly what retrieval saw.
func TestRetrievalEventsLogRecordsDecisions(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"upstream reply"}}],"usage":{"prompt_tokens":5,"completion_tokens":3}}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()

	dir := t.TempDir()
	logPath := filepath.Join(dir, "events.jsonl")
	g := newTestGateway(t, upSrv.URL)
	g.cfg.Retrieval.EventsLog = logPath
	srv := httptest.NewServer(g)
	defer srv.Close()

	seedInjectableCorpus(t, g)

	resp, out := postChatWithHeaders(t, srv, "test-key",
		map[string]string{"x-semantix-session": "ev-sess"},
		chatBody("deepseek-chat", "widgets", false))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("events log not written: %v", err)
	}
	var ev struct {
		At        int64  `json:"at"`
		SessionID string `json:"session"`
		Query     string `json:"query"`
		Decisions []struct {
			ID       string  `json:"id"`
			Score    float64 `json:"score"`
			Zone     string  `json:"zone"`
			Admitted bool    `json:"admitted"`
			Reason   string  `json:"reason"`
		} `json:"decisions"`
	}
	if err := json.Unmarshal(firstLine(raw), &ev); err != nil {
		t.Fatalf("event line not valid JSON: %v (line=%q)", err, firstLine(raw))
	}
	if ev.Query != "widgets" {
		t.Errorf("query = %q, want widgets", ev.Query)
	}
	if ev.SessionID != "ev-sess" {
		t.Errorf("session = %q, want ev-sess", ev.SessionID)
	}
	if ev.At == 0 {
		t.Error("at = 0, want a timestamp")
	}
	if len(ev.Decisions) == 0 {
		t.Fatal("decisions empty, want the full candidate trace")
	}
	foundAdmitted := false
	for _, d := range ev.Decisions {
		if d.ID == "l2-a" && d.Admitted && d.Reason == "admitted" {
			foundAdmitted = true
		}
	}
	if !foundAdmitted {
		t.Errorf("no admitted decision for l2-a in %s", raw)
	}
}

// TestRetrievalEventsLogOffByDefault: with events_log unset (the default)
// no event file may appear anywhere — the collector is strictly opt-in.
func TestRetrievalEventsLogOffByDefault(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()

	g := newTestGateway(t, upSrv.URL)
	if g.cfg.Retrieval.EventsLog != "" {
		t.Fatalf("EventsLog default = %q, want empty (opt-in)", g.cfg.Retrieval.EventsLog)
	}
	srv := httptest.NewServer(g)
	defer srv.Close()

	seedInjectableCorpus(t, g)
	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "widgets", false))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}

	entries, err := os.ReadDir(g.cfg.Store.DepsRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "events.jsonl" {
			t.Errorf("events.jsonl appeared without opt-in")
		}
	}
}

// firstLine returns raw up to (excluding) the first newline.
func firstLine(raw []byte) []byte {
	for i, b := range raw {
		if b == '\n' {
			return raw[:i]
		}
	}
	return raw
}
