package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEvaluateAllAblationArms(t *testing.T) {
	var input dialogue
	err := json.Unmarshal([]byte(`{
		"conversation": {
			"session_1": [{"dia_id":"D1:1","text":"cobalt lighthouse"},{"dia_id":"D1:2","text":"filler"},{"dia_id":"D1:3","text":"filler"},{"dia_id":"D1:4","text":"filler"}],
			"session_2": [{"dia_id":"D2:1","text":"cobalt lighthouse"},{"dia_id":"D2:2","text":"filler"},{"dia_id":"D2:3","text":"filler"},{"dia_id":"D2:4","text":"filler"}],
			"session_3": [{"dia_id":"D3:1","text":"cobalt lighthouse"},{"dia_id":"D3:2","text":"filler"},{"dia_id":"D3:3","text":"filler"},{"dia_id":"D3:4","text":"filler"}],
			"session_4": [{"dia_id":"D4:1","text":"lighthouse beacon"},{"dia_id":"D4:2","text":"filler"},{"dia_id":"D4:3","text":"filler"},{"dia_id":"D4:4","text":"filler"}],
			"session_5": [{"dia_id":"D5:1","text":"cobalt spotlight"},{"dia_id":"D5:2","text":"other"}]
		},
		"qa": [
			{"category":1,"question":"cobalt","evidence":["D4:1"]},
			{"category":4,"question":"spotlight","evidence":["D5:1"]},
			{"category":5,"question":"cobalt","evidence":["D5:1"]}
		]
	}`), &input)
	if err != nil {
		t.Fatal(err)
	}
	arms, err := evaluateAll([]dialogue{input})
	if err != nil {
		t.Fatal(err)
	}
	if len(arms) != 11 {
		t.Fatalf("got %d arms, want 11", len(arms))
	}
	for _, a := range arms {
		if a.All.Queries != 2 {
			t.Fatalf("arm %s: got %d queries, want 2", a.Name, a.All.Queries)
		}
		if a.HeldOut.Queries != 1 {
			t.Fatalf("arm %s: got %d held_out queries, want 1", a.Name, a.HeldOut.Queries)
		}
	}
	// ArmB (weighted fusion) must expand the "cobalt" query via the cobalt<->lighthouse edge.
	if arms[2].Expanded != 1 {
		t.Fatalf("ArmB expanded = %d, want 1", arms[2].Expanded)
	}
	if arms[2].GraphEdges == 0 {
		t.Fatalf("ArmB expected non-zero graph edges")
	}
	// Only the prefix contains the repeated pair. The soft graph credits
	// lighthouse for cobalt, and cannot expand the late-only word spotlight.
	if arms[6].GraphEdges != 1 || arms[6].Expanded != 1 {
		t.Fatalf("unexpected prefix graph report: %+v", arms[6])
	}
	if arms[7].GraphEdges != 13 || arms[10].GraphEdges != 12 {
		t.Fatalf("turn edges: full=%d, prefix=%d", arms[7].GraphEdges, arms[10].GraphEdges)
	}
}

func TestDuplicateTurnAcrossCutoff(t *testing.T) {
	input := dialogue{Conversation: map[string]json.RawMessage{
		"session_1": json.RawMessage(`[{"dia_id":"D1:1","text":"early"}]`),
		"session_2": json.RawMessage(`[{"dia_id":"D1:1","text":"late"}]`),
	}}
	if _, err := evaluateAll([]dialogue{input}); err == nil || !strings.Contains(err.Error(), "duplicate turn ID D1:1") {
		t.Fatalf("expected duplicate ID error, got %v", err)
	}
}
