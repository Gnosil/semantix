package main

import (
	"math/rand"

	"semantix/kernel/slice"
)

// A turn's neighbors are the immediately preceding and following turns from
// the same session. Turn order comes from the transcript, independent of IDs.
type turnGraph struct {
	neighbors map[string][]*slice.Slice
	edgeCount int
}

type turnShuffle int

const (
	originalOrder turnShuffle = iota
	shuffledOrder
	speakerShuffledOrder
)

func newTurnGraph(sessions [][]*slice.Slice, order turnShuffle, speakerByID map[string]string) turnGraph {
	g := turnGraph{neighbors: make(map[string][]*slice.Slice)}
	rng := rand.New(rand.NewSource(587))
	for _, turns := range sessions {
		if order != originalOrder && len(turns) > 3 {
			// Permuting only interior vertices preserves every turn's degree,
			// including both endpoints. Small paths can have no alternative.
			turns = append([]*slice.Slice(nil), turns...)
			interior := turns[1 : len(turns)-1]
			switch order {
			case shuffledOrder:
				rng.Shuffle(len(interior), func(i, j int) {
					interior[i], interior[j] = interior[j], interior[i]
				})
			case speakerShuffledOrder:
				// Forward Fisher-Yates within each speaker's positions. Traverse
				// the transcript so the RNG sequence is independent of map order.
				positions := make(map[string][]int)
				for i, turn := range interior {
					speaker := speakerByID[turn.ID]
					if speaker == "" {
						continue
					}
					positions[speaker] = append(positions[speaker], i)
					slots := positions[speaker]
					j := slots[rng.Intn(len(slots))]
					interior[i], interior[j] = interior[j], interior[i]
				}
			}
		}
		for i := 1; i < len(turns); i++ {
			a, b := turns[i-1], turns[i]
			g.neighbors[a.ID] = append(g.neighbors[a.ID], b)
			g.neighbors[b.ID] = append(g.neighbors[b.ID], a)
			g.edgeCount++
		}
	}
	return g
}

func (g turnGraph) rerank(primary []slice.Hit) ([]slice.Hit, bool) {
	// Propagate raw BM25 scores once. Using the strongest neighbor limits the
	// advantage that an interior turn would get solely from having two links.
	boost := make(map[string]float64)
	candidates := make(map[string]slice.Hit, len(primary))
	for _, h := range primary {
		candidates[h.Slice.ID] = h
	}
	for _, h := range primary {
		for _, neighbor := range g.neighbors[h.Slice.ID] {
			if increment := 0.5 * h.Score; increment > boost[neighbor.ID] {
				boost[neighbor.ID] = increment
				if _, ok := candidates[neighbor.ID]; !ok {
					candidates[neighbor.ID] = slice.Hit{Slice: neighbor}
				}
			}
		}
	}
	if len(boost) == 0 {
		return primary, false
	}
	out := make([]slice.Hit, 0, len(candidates))
	for id, h := range candidates {
		h.Score += boost[id]
		out = append(out, h)
	}
	sortHits(out)
	return out, true
}
