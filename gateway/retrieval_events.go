package gateway

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"

	"semantix/kernel/inject"
)

// This file implements [retrieval] events_log (docs/specs/
// local-retrieval-model.md §3 G1): one JSONL line per L2 retrieval carrying
// the query and the replayable per-candidate admission trace that
// inject.BuildHits already computes (Injection.Decisions). The offline
// trainer consumes the log; nothing in the serving path reads it back.
//
// The log carries query plaintext, so the collector is strictly opt-in
// (empty path — the default — writes nothing) and the file is 0600.

// retrievalEvent is the wire shape of one logged retrieval.
type retrievalEvent struct {
	At        int64               `json:"at"`
	SessionID string              `json:"session,omitempty"`
	Query     string              `json:"query"`
	TopMargin float64             `json:"top_margin,omitempty"`
	Decisions []retrievalDecision `json:"decisions"`
}

// retrievalDecision mirrors inject.CandidateDecision field for field. The
// copy is deliberate: the log is a persisted format, and mirroring keeps it
// stable even if the in-memory decision struct grows.
type retrievalDecision struct {
	ID       string  `json:"id,omitempty"`
	Score    float64 `json:"score"`
	Coverage float64 `json:"coverage,omitempty"`
	Zone     string  `json:"zone"`
	Admitted bool    `json:"admitted,omitempty"`
	Reason   string  `json:"reason"`
}

// retrievalEventLog is a lazily-opened append-only JSONL writer. The zero
// value is ready; the file opens on first append so an unset path costs one
// string compare per request and no fd.
type retrievalEventLog struct {
	mu sync.Mutex
	f  *os.File
}

func (l *retrievalEventLog) append(path string, ev retrievalEvent) error {
	raw, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		l.f = f
	}
	_, err = l.f.Write(raw)
	return err
}

func (l *retrievalEventLog) close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// recordRetrieval logs one L2 retrieval observation (best-effort, never
// blocks the main path — same contract as recordUsage). Empty injections
// are logged too: "this query retrieved nothing" is a training signal.
func (g *Gateway) recordRetrieval(sessionID, query string, inj *inject.Injection, now time.Time) {
	path := g.cfg.Retrieval.EventsLog
	if path == "" || inj == nil {
		return
	}
	ds := make([]retrievalDecision, 0, len(inj.Decisions))
	for _, d := range inj.Decisions {
		ds = append(ds, retrievalDecision{
			ID: d.ID, Score: d.Score, Coverage: d.Coverage,
			Zone: d.Zone, Admitted: d.Admitted, Reason: d.Reason,
		})
	}
	err := g.retrievalEvents.append(path, retrievalEvent{
		At: now.Unix(), SessionID: sessionID, Query: query,
		TopMargin: inj.TopMargin, Decisions: ds,
	})
	if err != nil {
		log.Printf("gateway: retrieval events append: %v", err)
	}
}
