// Command locomo compares turn-level BM25 with deterministic graph expansion.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"semantix/kernel/bm25"
	"semantix/kernel/graph"
	"semantix/kernel/slice"
)

type dialogue struct {
	Conversation map[string]json.RawMessage `json:"conversation"`
	QA           []question                 `json:"qa"`
}

type question struct {
	Text     string   `json:"question"`
	Category int      `json:"category"`
	Evidence []string `json:"evidence"`
}

type turn struct {
	ID      string `json:"dia_id"`
	Speaker string `json:"speaker"`
	Text    string `json:"text"`
}

type metrics struct {
	Queries int     `json:"queries"`
	Hit5    float64 `json:"hit_at_5"`
	Recall5 float64 `json:"recall_at_5"`
	MRR     float64 `json:"mrr"`
}

func (m *metrics) add(hits []slice.Hit, gold map[string]bool) {
	m.Queries++
	correct5 := 0
	first := true
	for i, hit := range hits {
		if !gold[hit.Slice.ID] {
			continue
		}
		if first {
			m.MRR += 1 / float64(i+1)
			first = false
		}
		if i < 5 {
			correct5++
		}
	}
	if correct5 > 0 {
		m.Hit5++
	}
	m.Recall5 += float64(correct5) / float64(len(gold))
}

func (m *metrics) mean() {
	if m.Queries > 0 {
		n := float64(m.Queries)
		m.Hit5, m.Recall5, m.MRR = m.Hit5/n, m.Recall5/n, m.MRR/n
	}
}

type comparison struct {
	BM25  metrics `json:"bm25"`
	Graph metrics `json:"bm25_graph_expansion"`
}

type report struct {
	Dialogues       int                 `json:"dialogues"`
	SHA256          string              `json:"dataset_sha256"`
	Adversarial     int                 `json:"excluded_adversarial"`
	InvalidEvidence int                 `json:"excluded_missing_or_unknown_evidence"`
	Expanded        int                 `json:"expanded_queries"`
	GraphEdges      int                 `json:"graph_edges"`
	All             comparison          `json:"all"`
	HeldOut         comparison          `json:"held_out_evidence"`
	ByCategory      map[int]*comparison `json:"by_category"`
}

var (
	sessionKey  = regexp.MustCompile(`^session_([1-9][0-9]*)$`)
	evidenceKey = regexp.MustCompile(`D:?([0-9]+):([0-9]+)`)
	stopWords   = strings.Fields("about after again also been before being could does from have here into just like more most much only other over said same some such than that their them then there these they this those very want were what when where which while will with words would your")
)

func terms(text string) []string {
	seen := make(map[string]bool)
	for _, word := range stopWords {
		seen[word] = true
	}
	var out []string
	for _, word := range bm25.Tokenize(text) {
		if len(word) >= 4 && !seen[word] {
			seen[word] = true
			out = append(out, word)
		}
	}
	sort.Strings(out)
	return out
}

// Learn term pairs only from training turns. A pair must recur in different
// sessions. The labels and questions are never used to build this graph.
func neighbors(sessions [][]turn) (map[string][]string, int) {
	type candidate struct {
		term    string
		support int
	}
	counts := make(map[string]int)
	turnCount := 0
	var train [][][]string
	for _, session := range sessions {
		var rows [][]string
		for _, t := range session {
			words := terms(t.Text)
			for _, word := range words {
				counts[word]++
			}
			rows = append(rows, words)
			turnCount++
		}
		train = append(train, rows)
	}
	var g graph.Graph
	for i, session := range train {
		for _, row := range session {
			for j, from := range row {
				if counts[from]*4 > turnCount {
					continue
				}
				for _, to := range row[j+1:] {
					if counts[to]*4 <= turnCount {
						g.Observe(strconv.Itoa(i), graph.Triple{Relation: "co_mentioned", From: from, To: to})
					}
				}
			}
		}
	}
	edges := g.Edges()
	ranked := make(map[string][]candidate)
	for _, edge := range edges {
		ranked[edge.From] = append(ranked[edge.From], candidate{edge.To, len(edge.Sessions)})
		ranked[edge.To] = append(ranked[edge.To], candidate{edge.From, len(edge.Sessions)})
	}
	out := make(map[string][]string)
	for seed, items := range ranked {
		sort.Slice(items, func(i, j int) bool {
			a, b := items[i], items[j]
			// Compare support normalized by candidate frequency without floats.
			left, right := a.support*counts[b.term], b.support*counts[a.term]
			if left != right {
				return left > right
			}
			return a.term < b.term
		})
		for _, item := range items {
			out[seed] = append(out[seed], item.term)
		}
	}
	return out, len(edges)
}

func expand(query string, related map[string][]string) string {
	seen := make(map[string]bool)
	for _, word := range bm25.Tokenize(query) {
		seen[word] = true
	}
	// Add at most one neighbor per seed and at most three overall.
	var extras []string
	for _, word := range terms(query) {
		for _, other := range related[word] {
			if !seen[other] {
				extras = append(extras, other)
				seen[other] = true
				break
			}
		}
		if len(extras) == 3 {
			break
		}
	}
	if len(extras) == 0 {
		return query
	}
	return query + " " + strings.Join(extras, " ")
}

func (r *report) evaluate(d dialogue) error {
	var order []int
	sessions := make(map[int][]turn)
	for key, value := range d.Conversation {
		match := sessionKey.FindStringSubmatch(key)
		if match == nil {
			continue
		}
		number, err := strconv.Atoi(match[1])
		if err != nil {
			return err
		}
		var turns []turn
		if err := json.Unmarshal(value, &turns); err != nil {
			return err
		}
		order = append(order, number)
		sessions[number] = turns
	}
	if len(order) < 2 {
		return fmt.Errorf("dialogue has fewer than two sessions")
	}
	sort.Ints(order)
	cutoff := len(order) * 4 / 5
	var train [][]turn
	index := bm25.New()
	heldOut := make(map[string]bool)
	for i, number := range order {
		ts := sessions[number]
		if i < cutoff {
			train = append(train, ts)
		}
		for _, t := range ts {
			if t.ID == "" {
				return fmt.Errorf("empty dialogue turn ID")
			}
			if _, exists := heldOut[t.ID]; exists {
				return fmt.Errorf("duplicate turn ID %q", t.ID)
			}
			heldOut[t.ID] = i >= cutoff
			if err := index.Insert(&slice.Slice{ID: t.ID, Scope: slice.Project, Content: []byte(t.Speaker + " " + t.Text)}); err != nil {
				return err
			}
		}
	}
	related, edgeCount := neighbors(train)
	r.GraphEdges += edgeCount
	for _, q := range d.QA {
		if q.Category == 5 {
			r.Adversarial++
			continue
		}
		gold := make(map[string]bool)
		lateGold := make(map[string]bool)
		valid := true
		for _, ref := range q.Evidence {
			matches := evidenceKey.FindAllStringSubmatch(ref, -1)
			if len(matches) == 0 {
				valid = false
			}
			for _, match := range matches {
				id := "D" + strings.TrimLeft(match[1], "0") + ":" + strings.TrimLeft(match[2], "0")
				late, exists := heldOut[id]
				valid = valid && exists
				if late {
					lateGold[id] = true
				}
				gold[id] = true
			}
		}
		if !valid || len(gold) == 0 {
			r.InvalidEvidence++
			continue
		}
		query := expand(q.Text, related)
		if query != q.Text {
			r.Expanded++
		}
		baseline, err := index.Search(q.Text, len(heldOut), slice.Project)
		if err != nil {
			return err
		}
		expanded := baseline
		if query != q.Text {
			expanded, err = index.Search(query, len(heldOut), slice.Project)
			if err != nil {
				return err
			}
		}
		category := r.ByCategory[q.Category]
		if category == nil {
			category = &comparison{}
			r.ByCategory[q.Category] = category
		}
		for _, bucket := range []*comparison{&r.All, category} {
			bucket.BM25.add(baseline, gold)
			bucket.Graph.add(expanded, gold)
		}
		if len(lateGold) > 0 {
			r.HeldOut.BM25.add(baseline, lateGold)
			r.HeldOut.Graph.add(expanded, lateGold)
		}
	}
	return nil
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: go run ./scripts/locomo <locomo10.json>")
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		return err
	}
	var dialogues []dialogue
	if err := json.Unmarshal(data, &dialogues); err != nil {
		return err
	}
	r := report{Dialogues: len(dialogues), SHA256: fmt.Sprintf("%x", sha256.Sum256(data)), ByCategory: make(map[int]*comparison)}
	for i, d := range dialogues {
		if err := r.evaluate(d); err != nil {
			return fmt.Errorf("dialogue %d: %w", i+1, err)
		}
	}
	if r.All.BM25.Queries == 0 {
		return fmt.Errorf("no questions with usable evidence")
	}
	for _, c := range []*comparison{&r.All, &r.HeldOut} {
		c.BM25.mean()
		c.Graph.mean()
	}
	for _, c := range r.ByCategory {
		c.BM25.mean()
		c.Graph.mean()
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
