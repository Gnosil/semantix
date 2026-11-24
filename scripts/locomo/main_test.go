package main

import (
	"encoding/json"
	"testing"
)

func TestEvaluateKeepsHeldOutTurnsOutOfGraph(t *testing.T) {
	var input dialogue
	err := json.Unmarshal([]byte(`{
		"conversation": {
			"session_1": [{"dia_id":"D1:1","text":"cobalt lighthouse"}],
			"session_2": [{"dia_id":"D2:1","text":"cobalt lighthouse"}],
			"session_3": [{"dia_id":"D3:1","text":"cobalt spotlight"}],
			"session_4": [{"dia_id":"D4:1","text":"lighthouse"}],
			"session_5": [{"dia_id":"D5:1","text":"cobalt spotlight"},{"dia_id":"D5:2","text":"other"}]
		},
		"qa": [
			{"category":1,"question":"cobalt","evidence":["D5:01; D:4:1"]},
			{"category":4,"question":"cobalt","evidence":["D99:1"]},
			{"category":5,"question":"cobalt","evidence":["D5:1"]}
		]
	}`), &input)
	if err != nil {
		t.Fatal(err)
	}
	// Background turns make the repeated terms less than a quarter of the
	// training corpus so the common-term filter does not hide this fixture.
	for i := 1; i <= 4; i++ {
		key := "session_" + string(rune('0'+i))
		var turns []turn
		if err := json.Unmarshal(input.Conversation[key], &turns); err != nil {
			t.Fatal(err)
		}
		for _, suffix := range []string{"2", "3", "4"} {
			turns = append(turns, turn{ID: turns[0].ID[:3] + suffix, Text: "filler"})
		}
		input.Conversation[key], err = json.Marshal(turns)
		if err != nil {
			t.Fatal(err)
		}
	}
	r := report{ByCategory: make(map[int]*comparison)}
	if err := r.evaluate(input); err != nil {
		t.Fatal(err)
	}
	if r.GraphEdges != 1 || r.Expanded != 1 || r.HeldOut.BM25.Queries != 1 ||
		r.InvalidEvidence != 1 || r.Adversarial != 1 {
		t.Fatalf("unexpected report: %+v", r)
	}
}
