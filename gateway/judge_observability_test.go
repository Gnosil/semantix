package gateway

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"semantix/kernel/slice"
	"semantix/kernel/usage"
	"semantix/kernel/zone"
)

// ---------------------------------------------------------------------------
// Issue #242 gap 1: grey-zone judge verdicts must land on disk.
//
// GW4 acceptance §8.3: t10 (rel 0.675, unambiguously grey) did not hit and
// no durable evidence distinguished "the judge ruled it not reusable" from
// "the judge call failed and we fell back to fail-closed" — and the judge's
// own model call was never metered, so its cost could not be accounted for.

// judgeServer answers the OpenAI judge protocol with a fixed verdict word,
// or fails with the given status when status != 200.
func judgeServer(t *testing.T, reply string, status int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"error":"judge down"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"`+reply+`"}}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// newJudgingGateway wires a real LLM judge and forces every L3 candidate
// into the grey zone (TauHigh above 1 is unreachable for any score/top1).
func newJudgingGateway(t *testing.T, upURL, judgeBase string) *Gateway {
	t.Helper()
	g := newTestGatewayCfg(t, upURL, func(c *Config) {
		c.Cache.JudgeAPIKey = "jk"
		c.Cache.JudgeBaseURL = judgeBase
		c.Cache.JudgeModel = "judge-model"
	})
	// S8 moved recordSliceStats after the 2xx forward, so the async stats
	// write can outlive the assertions; join it before TempDir cleanup or
	// Windows RemoveAll flakes on the still-held project.db handle.
	t.Cleanup(func() { g.ingestWG.Wait() })
	z := zone.Zones{TauHigh: 1.5, TauLow: 0.5, AbsHigh: 0.7, AbsLow: 0.45}
	g.decider.Zones = &z
	return g
}

// seedGreyCandidate indexes a Result slice that the query will retrieve.
func seedGreyCandidate(t *testing.T, g *Gateway, id, query string) {
	t.Helper()
	chash, _ := contextHash([]chatMessage{msg("user", query)})
	seed(t, g, &slice.Slice{
		ID: id, Type: slice.Result, Scope: slice.Project,
		Content: []byte(query + " " + query + " cached answer"),
		Meta:    slice.SliceMeta{L3Safe: true, ContextHash: chash, Model: "deepseek-chat", Origin: slice.OriginSessionAuto},
	})
}

// readUsageEvents parses the gateway's usage JSONL.
func readUsageEvents(t *testing.T, g *Gateway) []usage.Event {
	t.Helper()
	f, err := os.Open(g.cfg.Ingest.UsageLog)
	if err != nil {
		t.Fatalf("open usage log: %v", err)
	}
	defer f.Close()
	var out []usage.Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e usage.Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("bad usage line %q: %v", line, err)
		}
		out = append(out, e)
	}
	return out
}

// judgeDecisions flattens every recorded judge decision in the usage log.
func judgeDecisions(t *testing.T, g *Gateway) []usage.JudgeDecision {
	t.Helper()
	var out []usage.JudgeDecision
	for _, e := range readUsageEvents(t, g) {
		out = append(out, e.JudgeDecisions...)
	}
	return out
}

func TestJudgeDeclineLandsInUsageLog(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"fresh"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newJudgingGateway(t, upSrv.URL, judgeServer(t, "no", http.StatusOK))
	srv := httptest.NewServer(g)
	defer srv.Close()
	seedGreyCandidate(t, g, "grey-decline", "hello world")

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	if resp.Header.Get("x-semantix-cache") != "miss" {
		t.Fatalf("cache = %q, want miss (judge declined)", resp.Header.Get("x-semantix-cache"))
	}

	ds := judgeDecisions(t, g)
	if len(ds) != 1 {
		t.Fatalf("judge decisions in usage log = %d, want 1", len(ds))
	}
	d := ds[0]
	if d.Verdict != "declined" {
		t.Errorf("verdict = %q, want declined", d.Verdict)
	}
	if d.FailClosed {
		t.Error("fail_closed = true: a real 'no' must not be recorded as a fallback")
	}
	if !d.Called {
		t.Error("called = false: the judge model was invoked and is billable")
	}
	if d.SliceID != "grey-decline" {
		t.Errorf("slice_id = %q, want grey-decline", d.SliceID)
	}
	if d.RelConfidence <= 0 {
		t.Errorf("rel_confidence = %v, want the grey ratio", d.RelConfidence)
	}
	if d.PromptTokens <= 0 {
		t.Errorf("prompt_tokens = %d, want a judge cost estimate", d.PromptTokens)
	}
}

// The decisive case: an unreachable/erroring judge must be recorded as
// fail_closed with the error, never as a verdict.
func TestJudgeFailClosedLandsInUsageLog(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"fresh"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newJudgingGateway(t, upSrv.URL, judgeServer(t, "", http.StatusInternalServerError))
	srv := httptest.NewServer(g)
	defer srv.Close()
	seedGreyCandidate(t, g, "grey-error", "hello world")

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}

	ds := judgeDecisions(t, g)
	if len(ds) != 1 {
		t.Fatalf("judge decisions in usage log = %d, want 1", len(ds))
	}
	d := ds[0]
	if d.Verdict != "fail_closed" || !d.FailClosed {
		t.Errorf("verdict = %q fail_closed = %v, want fail_closed/true", d.Verdict, d.FailClosed)
	}
	if d.Error == "" {
		t.Error("error is empty: the whole point is knowing the call failed")
	}
	if strings.Contains(d.Error, "jk") {
		t.Errorf("error leaks the judge API key: %q", d.Error)
	}
}

// An approved verdict serves the turn from cache; the decision must ride on
// the L3-hit usage event, not vanish because no upstream call happened.
func TestJudgeApprovalLandsOnL3HitEvent(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"unused"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newJudgingGateway(t, upSrv.URL, judgeServer(t, "yes", http.StatusOK))
	srv := httptest.NewServer(g)
	defer srv.Close()
	seedGreyCandidate(t, g, "grey-approve", "hello world")

	resp, out := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, out)
	}
	if resp.Header.Get("x-semantix-cache") != "hit" {
		t.Fatalf("cache = %q, want hit (judge approved), body %s", resp.Header.Get("x-semantix-cache"), out)
	}
	evs := readUsageEvents(t, g)
	if len(evs) != 1 {
		t.Fatalf("usage events = %d, want 1", len(evs))
	}
	if !evs[0].L3Reuse {
		t.Fatal("the hit event must be the L3 reuse event")
	}
	if evs[0].L3SliceID != "grey-approve" {
		t.Fatalf("L3SliceID = %q, want grey-approve (veto entry point)", evs[0].L3SliceID)
	}
	if len(evs[0].JudgeDecisions) != 1 || evs[0].JudgeDecisions[0].Verdict != "approved" {
		t.Fatalf("judge decisions on the hit event = %+v, want one approved", evs[0].JudgeDecisions)
	}
}

// Streaming turns must meter the judge too — the SSE relay is a separate
// code path from the plain passthrough.
func TestJudgeDecisionLandsOnStreamingTurn(t *testing.T) {
	up := &testUpstream{stream: "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\ndata: [DONE]\n\n"}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newJudgingGateway(t, upSrv.URL, judgeServer(t, "no", http.StatusOK))
	srv := httptest.NewServer(g)
	defer srv.Close()
	seedGreyCandidate(t, g, "grey-stream", "hello world")

	resp, _ := postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", true))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	ds := judgeDecisions(t, g)
	if len(ds) != 1 || ds[0].Verdict != "declined" {
		t.Fatalf("streaming judge decisions = %+v, want one declined", ds)
	}
}

// Summarize must make the judge cost countable straight from the log.
func TestJudgeCostIsSummarizable(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"fresh"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newJudgingGateway(t, upSrv.URL, judgeServer(t, "no", http.StatusOK))
	srv := httptest.NewServer(g)
	defer srv.Close()
	seedGreyCandidate(t, g, "grey-cost", "hello world")

	postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))

	s, err := usage.Summarize(g.cfg.Ingest.UsageLog, usage.DefaultCostMissPerMTok, usage.DefaultCostHitPerMTok)
	if err != nil {
		t.Fatal(err)
	}
	if s.JudgeCalls != 1 || s.JudgeDeclined != 1 {
		t.Fatalf("summary judge counters = calls %d declined %d, want 1/1", s.JudgeCalls, s.JudgeDeclined)
	}
	if s.JudgeCostUSD(usage.DefaultCostMissPerMTok) <= 0 {
		t.Fatal("judge cost must be computable and non-zero once a judge call happened")
	}
}

// The structured log line is the operator-facing half: greppable at
// debugging time without parsing the usage JSONL.
func TestJudgeDecisionIsLogged(t *testing.T) {
	var buf strings.Builder
	old := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(old) })

	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"fresh"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newJudgingGateway(t, upSrv.URL, judgeServer(t, "no", http.StatusOK))
	srv := httptest.NewServer(g)
	defer srv.Close()
	seedGreyCandidate(t, g, "grey-log", "hello world")

	postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))

	got := buf.String()
	for _, want := range []string{"l3 judge", "slice=grey-log", "verdict=declined", "fail_closed=false", "rel="} {
		if !strings.Contains(got, want) {
			t.Errorf("log line missing %q; got:\n%s", want, got)
		}
	}
}

// Without a judge configured, grey rejection is deterministic and free —
// the usage log must stay exactly as it was.
func TestNoJudgeConfiguredRecordsNoDecisions(t *testing.T) {
	up := &testUpstream{plain: `{"choices":[{"message":{"role":"assistant","content":"fresh"}}]}`}
	upSrv := httptest.NewServer(up.handler())
	defer upSrv.Close()
	g := newTestGateway(t, upSrv.URL)
	z := zone.Zones{TauHigh: 1.5, TauLow: 0.5, AbsHigh: 0.7, AbsLow: 0.45}
	g.decider.Zones = &z
	srv := httptest.NewServer(g)
	defer srv.Close()
	seedGreyCandidate(t, g, "grey-none", "hello world")

	postChat(t, srv, "test-key", chatBody("deepseek-chat", "hello world", false))

	if ds := judgeDecisions(t, g); len(ds) != 0 {
		t.Fatalf("judge decisions = %+v, want none when no judge is wired", ds)
	}
}
