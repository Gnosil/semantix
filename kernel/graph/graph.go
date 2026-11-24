// Package graph aggregates repeat observations from distinct source sessions.
package graph

import "sort"

// Triple identifies a directed relation. Callers canonicalize symmetric pairs.
type Triple struct {
	Relation string `json:"relation"`
	From     string `json:"from"`
	To       string `json:"to"`
}

type Edge struct {
	Triple
	Sessions []string `json:"sessions"`
}

// Graph is a replay-time accumulator. Its zero value is ready to use.
type Graph struct {
	evidence map[Triple]map[string]struct{}
}

func (g *Graph) Observe(session string, triple Triple) {
	if session == "" || triple.Relation == "" || triple.From == "" || triple.To == "" {
		return
	}
	if g.evidence == nil {
		g.evidence = make(map[Triple]map[string]struct{})
	}
	sessions := g.evidence[triple]
	if sessions == nil {
		sessions = make(map[string]struct{})
		g.evidence[triple] = sessions
	}
	sessions[session] = struct{}{}
}

// Edges returns a deterministic snapshot. A single session cannot confirm an edge.
func (g *Graph) Edges() []Edge {
	edges := make([]Edge, 0)
	for triple, evidence := range g.evidence {
		if len(evidence) < 2 {
			continue
		}
		edge := Edge{Triple: triple, Sessions: make([]string, 0, len(evidence))}
		for session := range evidence {
			edge.Sessions = append(edge.Sessions, session)
		}
		sort.Strings(edge.Sessions)
		edges = append(edges, edge)
	}
	sort.Slice(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.Relation != b.Relation {
			return a.Relation < b.Relation
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
	return edges
}
