package main

import (
	"reflect"
	"testing"

	"semantix/kernel/slice"
)

func TestTurnGraphPropagatesOneHopWithinSession(t *testing.T) {
	sessions := [][]*slice.Slice{
		{{ID: "z"}, {ID: "a"}, {ID: "m"}, {ID: "b"}, {ID: "c"}},
		{{ID: "d"}},
	}
	g := newTurnGraph(sessions, originalOrder, nil)
	if g.edgeCount != 4 {
		t.Fatalf("got %d edges, want 4", g.edgeCount)
	}
	primary := []slice.Hit{
		{Slice: sessions[0][0], Score: 8, QueryCoverageValid: true},
		{Slice: sessions[0][2], Score: 6},
	}
	got, used := g.rerank(primary)
	// a receives max(8,6)/2, b receives 6/2, and c cannot be reached
	// through b. No edge connects the two sessions.
	want := []slice.Hit{primary[0], primary[1], {Slice: sessions[0][1], Score: 4}, {Slice: sessions[0][3], Score: 3}}
	if !used || !reflect.DeepEqual(got, want) {
		t.Fatalf("hits = %+v, want %+v", got, want)
	}
	got, used = g.rerank([]slice.Hit{{Slice: sessions[0][0], Score: 8}, {Slice: sessions[0][1], Score: 6}})
	if !used || got[0].Score != 11 || got[1].Score != 10 {
		t.Fatalf("adjacent lexical hits should accumulate own score and half their neighbor's, got %+v", got)
	}
	singleton := []slice.Hit{{Slice: sessions[1][0], Score: 4, QueryCoverageValid: true}}
	if got, used := g.rerank(singleton); used || !reflect.DeepEqual(got, singleton) {
		t.Fatalf("disconnected query changed from %+v to %+v", singleton, got)
	}
}

func TestTurnGraphShuffledControlPreservesDegrees(t *testing.T) {
	rows := [][]*slice.Slice{
		{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"}, {ID: "f"}, {ID: "g"}, {ID: "h"}},
		{{ID: "i"}, {ID: "j"}}, {{ID: "k"}},
	}
	actual := newTurnGraph(rows, originalOrder, nil)
	shuffled := newTurnGraph(rows, shuffledOrder, nil)
	if actual.edgeCount != shuffled.edgeCount || reflect.DeepEqual(actual.neighbors, shuffled.neighbors) {
		t.Fatalf("control should preserve edge count and change neighbors")
	}
	for _, session := range rows {
		ids := make(map[string]bool)
		for _, turn := range session {
			ids[turn.ID] = true
		}
		for _, turn := range session {
			neighbors := shuffled.neighbors[turn.ID]
			if len(actual.neighbors[turn.ID]) != len(neighbors) {
				t.Fatalf("control changed the degree of %s", turn.ID)
			}
			for _, neighbor := range neighbors {
				if !ids[neighbor.ID] {
					t.Fatalf("control linked %s to another session", turn.ID)
				}
			}
		}
	}
	if !reflect.DeepEqual(actual, newTurnGraph(rows, originalOrder, nil)) || !reflect.DeepEqual(shuffled, newTurnGraph(rows, shuffledOrder, nil)) {
		t.Fatal("control must be reproducible and leave transcript order intact")
	}
}

func TestTurnGraphSpeakerShufflePreservesSessionSpeakerSequence(t *testing.T) {
	rows := [][]*slice.Slice{
		{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"}, {ID: "f"}, {ID: "g"}, {ID: "h"}, {ID: "i"}, {ID: "j"}, {ID: "k"}, {ID: "l"}},
		{{ID: "m"}, {ID: "n"}, {ID: "o"}}, {{ID: "p"}},
	}
	speakers := map[string]string{
		"a": "A", "b": "B", "c": "A", "d": "A", "e": "B", "f": "B", "g": "A",
		"h": "A", "i": "B", "j": "B", "l": "A", "m": "A", "n": "B", "o": "A", "p": "B",
	}
	actual := newTurnGraph(rows, originalOrder, nil)
	shuffled := newTurnGraph(rows, speakerShuffledOrder, speakers)
	if actual.edgeCount != shuffled.edgeCount || reflect.DeepEqual(actual, shuffled) {
		t.Fatal("speaker control should rewire this fixture without changing edge count")
	}
	for _, session := range rows {
		// Walk the path from its original first turn and recover the realized order.
		current, previous := session[0].ID, ""
		for _, turn := range session {
			if len(actual.neighbors[turn.ID]) != len(shuffled.neighbors[turn.ID]) {
				t.Fatalf("control changed degree of %s", turn.ID)
			}
			if speakers[current] != speakers[turn.ID] || (speakers[current] == "" && current != turn.ID) {
				t.Fatalf("%s was replaced by %s at a different speaker's position", turn.ID, current)
			}
			var next string
			for _, n := range shuffled.neighbors[current] {
				if n.ID != previous {
					next = n.ID
				}
			}
			previous, current = current, next
		}
		if previous != session[len(session)-1].ID || current != "" {
			t.Fatalf("control changed endpoint of session starting at %s", session[0].ID)
		}
	}
	if !reflect.DeepEqual(shuffled, newTurnGraph(rows, speakerShuffledOrder, speakers)) || !reflect.DeepEqual(actual, newTurnGraph(rows, originalOrder, nil)) {
		t.Fatal("speaker control must be reproducible and leave transcript order intact")
	}
}
