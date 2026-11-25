package main

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"semantix/kernel/slice"
)

func TestSoftMatchOnlyCreditsMissingSeeds(t *testing.T) {
	g := softGraph{
		"beacon": {"harbor": 0.25, "light": 0.75},
		"trail":  {"forest": 0.5, "beacon": 0.8},
	}
	doc := func(id, text string) termDocument {
		return newTermDocument(&slice.Slice{ID: id, Content: []byte(text)})
	}
	docs := []termDocument{
		doc("exact", "beacon harbor light"), doc("both", "harbor light forest"), doc("weak", "harbor"),
	}
	primary := []slice.Hit{{Slice: docs[0].doc, Score: 2}}
	got, applied := g.rerank("beacon trail", primary, docs)
	// Query words cannot substitute for each other. Two neighbors of the same
	// missing seed earn its maximum credit once; distinct seeds can add credit.
	want := []slice.Hit{primary[0], {Slice: docs[1].doc, Score: 1.25}, {Slice: docs[2].doc, Score: 0.25}}
	if !applied || !reflect.DeepEqual(got, want) {
		t.Fatalf("applied=%v hits=%+v, want %+v", applied, got, want)
	}
	if reverse, _ := g.rerank("trail beacon", primary, docs); !reflect.DeepEqual(reverse, got) {
		t.Fatal("reordering seeds changed the ranking")
	}
	if unchanged, applied := g.rerank("unknown", primary, docs); applied || !reflect.DeepEqual(unchanged, primary) {
		t.Fatal("unknown seed changed baseline")
	}
}

func TestLateSessionsCannotConfirmPrefixEdge(t *testing.T) {
	input := dialogue{
		Conversation: make(map[string]json.RawMessage),
		QA:           []question{{Text: "beacon", Category: 1, Evidence: []string{"D10:1"}}},
	}
	for s := 1; s <= 10; s++ {
		text := "filler"
		if s > 8 {
			text = "beacon harbor"
		}
		input.Conversation[fmt.Sprintf("session_%d", s)] = json.RawMessage(fmt.Sprintf(
			`[{"dia_id":"D%d:1","text":%q}]`, s, text))
	}
	arms, err := evaluateAll([]dialogue{input})
	if err != nil {
		t.Fatal(err)
	}
	if arms[6].GraphEdges != 0 || arms[6].Expanded != 0 || arms[6].HeldOut != arms[0].HeldOut {
		t.Fatalf("late-only pair affected prefix graph: %+v", arms[6])
	}
}
