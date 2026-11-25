// Command locomo_ablation runs a 6-arm controlled ablation comparing baseline
// BM25 against deterministic graph expansion strategies on LoCoMo:
//   - Arm0: BM25 baseline
//   - ArmA: PR #586 naive query concatenation (support >= 2)
//   - ArmB: Non-destructive weighted fusion (0.85 primary BM25 + 0.15 expansion BM25)
//   - ArmC: Tighter graph (support >= 3, PMI >= 3.0, maxDocFrac <= 0.15) with concat
//   - ArmD: Confidence-gated expansion (only when baseline coverage < 0.6 or relMargin < 0.12)
//   - ArmE: Combined (tighter graph + weighted fusion + confidence gate)
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
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

type armReport struct {
	Name       string           `json:"name"`
	Expanded   int              `json:"expanded_queries"`
	GraphEdges int              `json:"graph_edges"`
	All        metrics          `json:"all"`
	HeldOut    metrics          `json:"held_out"`
	ByCategory map[int]*metrics `json:"by_category"`
}

func newArmReport(name string) *armReport {
	return &armReport{Name: name, ByCategory: map[int]*metrics{}}
}

func (a *armReport) record(cat int, hits []slice.Hit, gold, lateGold map[string]bool) {
	a.All.add(hits, gold)
	cm := a.ByCategory[cat]
	if cm == nil {
		cm = &metrics{}
		a.ByCategory[cat] = cm
	}
	cm.add(hits, gold)
	if len(lateGold) > 0 {
		a.HeldOut.add(hits, lateGold)
	}
}

func (a *armReport) finalize() {
	a.All.mean()
	a.HeldOut.mean()
	for _, m := range a.ByCategory {
		m.mean()
	}
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

type graphConfig struct {
	minSupport int
	minPMI     float64
	maxDocFrac float64
}

func buildNeighbors(sessions [][]turn, cfg graphConfig) (map[string][]string, int) {
	type candidate struct {
		term    string
		support int
		pmi     float64
	}
	termTurns := make(map[string]int)
	pairTurns := make(map[graph.Triple]int)
	turnCount := 0
	var train [][][]string
	for _, session := range sessions {
		var rows [][]string
		for _, t := range session {
			words := terms(t.Text)
			for _, word := range words {
				termTurns[word]++
			}
			rows = append(rows, words)
			turnCount++
		}
		train = append(train, rows)
	}
	if turnCount == 0 {
		return map[string][]string{}, 0
	}
	var g graph.Graph
	for i, session := range train {
		for _, row := range session {
			for j, from := range row {
				if float64(termTurns[from])/float64(turnCount) > cfg.maxDocFrac {
					continue
				}
				for _, to := range row[j+1:] {
					if float64(termTurns[to])/float64(turnCount) <= cfg.maxDocFrac {
						tr := graph.Triple{Relation: "co_mentioned", From: from, To: to}
						g.Observe(strconv.Itoa(i), tr)
						pairTurns[tr]++
					}
				}
			}
		}
	}
	edges := g.Edges()
	ranked := make(map[string][]candidate)
	keptEdges := 0
	n := float64(turnCount)
	for _, edge := range edges {
		sup := len(edge.Sessions)
		if sup < cfg.minSupport {
			continue
		}
		pab := float64(pairTurns[edge.Triple]) / n
		pa := float64(termTurns[edge.From]) / n
		pb := float64(termTurns[edge.To]) / n
		pmi := 0.0
		if pab > 0 && pa > 0 && pb > 0 {
			pmi = math.Log2(pab / (pa * pb))
		}
		if cfg.minPMI > 0 && pmi < cfg.minPMI {
			continue
		}
		keptEdges++
		ranked[edge.From] = append(ranked[edge.From], candidate{edge.To, sup, pmi})
		ranked[edge.To] = append(ranked[edge.To], candidate{edge.From, sup, pmi})
	}
	out := make(map[string][]string)
	for seed, items := range ranked {
		sort.Slice(items, func(i, j int) bool {
			a, b := items[i], items[j]
			if cfg.minPMI > 0 {
				if a.pmi != b.pmi {
					return a.pmi > b.pmi
				}
			} else {
				left, right := a.support*termTurns[b.term], b.support*termTurns[a.term]
				if left != right {
					return left > right
				}
			}
			return a.term < b.term
		})
		for _, item := range items {
			out[seed] = append(out[seed], item.term)
		}
	}
	return out, keptEdges
}

func expandTerms(query string, related map[string][]string, maxExtras int) []string {
	seen := make(map[string]bool)
	for _, word := range bm25.Tokenize(query) {
		seen[word] = true
	}
	var extras []string
	for _, word := range terms(query) {
		for _, other := range related[word] {
			if !seen[other] {
				extras = append(extras, other)
				seen[other] = true
				break
			}
		}
		if len(extras) >= maxExtras {
			break
		}
	}
	return extras
}

// fuseWeighted blends primary BM25 hits with secondary expansion hits, keeping
// primary BM25 dominant (alpha = primary weight, 1-alpha = secondary weight).
func fuseWeighted(primary, secondary []slice.Hit, alpha float64) []slice.Hit {
	norm := func(hs []slice.Hit) map[string]float64 {
		m := make(map[string]float64, len(hs))
		if len(hs) == 0 || hs[0].Score <= 0 {
			return m
		}
		top := hs[0].Score
		for _, h := range hs {
			if h.Score > 0 {
				m[h.Slice.ID] = h.Score / top
			}
		}
		return m
	}
	pN := norm(primary)
	sN := norm(secondary)
	byID := make(map[string]*slice.Slice, len(primary)+len(secondary))
	for _, h := range primary {
		byID[h.Slice.ID] = h.Slice
	}
	for _, h := range secondary {
		if _, ok := byID[h.Slice.ID]; !ok {
			byID[h.Slice.ID] = h.Slice
		}
	}
	out := make([]slice.Hit, 0, len(byID))
	for id, sl := range byID {
		sc := alpha*pN[id] + (1-alpha)*sN[id]
		if sc > 0 {
			out = append(out, slice.Hit{Slice: sl, Score: sc})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Slice.ID < out[j].Slice.ID
		}
		return out[i].Score > out[j].Score
	})
	return out
}

// lowConfidence reports whether baseline BM25 is uncertain (empty, low top-1
// query coverage, or narrow top1-top2 relative margin).
func lowConfidence(baseline []slice.Hit) bool {
	if len(baseline) == 0 {
		return true
	}
	if baseline[0].QueryCoverageValid && baseline[0].QueryCoverage < 0.6 {
		return true
	}
	if len(baseline) >= 2 && baseline[0].Score > 0 {
		relMargin := (baseline[0].Score - baseline[1].Score) / baseline[0].Score
		if relMargin < 0.12 {
			return true
		}
	}
	return false
}

func evaluateAll(dialogues []dialogue) ([]*armReport, error) {
	armBase := newArmReport("Arm0_BM25_Baseline")
	armA := newArmReport("ArmA_PR586_NaiveConcat")
	armB := newArmReport("ArmB_WeightedFusion_85_15")
	armC := newArmReport("ArmC_TightGraph_Sup3_PMI3")
	armD := newArmReport("ArmD_ConfidenceGated")
	armE := newArmReport("ArmE_Combined_BCD")
	arms := []*armReport{armBase, armA, armB, armC, armD, armE}

	for idx, d := range dialogues {
		var order []int
		sessions := make(map[int][]turn)
		for key, value := range d.Conversation {
			match := sessionKey.FindStringSubmatch(key)
			if match == nil {
				continue
			}
			number, err := strconv.Atoi(match[1])
			if err != nil {
				return nil, fmt.Errorf("dialogue %d: %w", idx+1, err)
			}
			var turns []turn
			if err := json.Unmarshal(value, &turns); err != nil {
				return nil, fmt.Errorf("dialogue %d session %s: %w", idx+1, key, err)
			}
			order = append(order, number)
			sessions[number] = turns
		}
		if len(order) < 2 {
			return nil, fmt.Errorf("dialogue %d: fewer than two sessions", idx+1)
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
					return nil, fmt.Errorf("dialogue %d: empty turn ID", idx+1)
				}
				heldOut[t.ID] = i >= cutoff
				if err := index.Insert(&slice.Slice{ID: t.ID, Scope: slice.Project, Content: []byte(t.Speaker + " " + t.Text)}); err != nil {
					return nil, fmt.Errorf("dialogue %d insert %s: %w", idx+1, t.ID, err)
				}
			}
		}

		relDefault, edgesDefault := buildNeighbors(train, graphConfig{minSupport: 2, minPMI: 0, maxDocFrac: 0.25})
		relTight, edgesTight := buildNeighbors(train, graphConfig{minSupport: 3, minPMI: 3.0, maxDocFrac: 0.15})

		armA.GraphEdges += edgesDefault
		armB.GraphEdges += edgesDefault
		armD.GraphEdges += edgesDefault
		armC.GraphEdges += edgesTight
		armE.GraphEdges += edgesTight

		totalDocs := len(heldOut)
		for _, q := range d.QA {
			if q.Category == 5 {
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
				continue
			}

			baseline, err := index.Search(q.Text, totalDocs, slice.Project)
			if err != nil {
				return nil, err
			}
			armBase.record(q.Category, baseline, gold, lateGold)

			extrasA := expandTerms(q.Text, relDefault, 3)
			hitsA := baseline
			if len(extrasA) > 0 {
				armA.Expanded++
				hitsA, err = index.Search(q.Text+" "+strings.Join(extrasA, " "), totalDocs, slice.Project)
				if err != nil {
					return nil, err
				}
			}
			armA.record(q.Category, hitsA, gold, lateGold)

			hitsB := baseline
			if len(extrasA) > 0 {
				armB.Expanded++
				expOnly, err := index.Search(strings.Join(extrasA, " "), totalDocs, slice.Project)
				if err != nil {
					return nil, err
				}
				hitsB = fuseWeighted(baseline, expOnly, 0.85)
			}
			armB.record(q.Category, hitsB, gold, lateGold)

			extrasC := expandTerms(q.Text, relTight, 3)
			hitsC := baseline
			if len(extrasC) > 0 {
				armC.Expanded++
				hitsC, err = index.Search(q.Text+" "+strings.Join(extrasC, " "), totalDocs, slice.Project)
				if err != nil {
					return nil, err
				}
			}
			armC.record(q.Category, hitsC, gold, lateGold)

			hitsD := baseline
			if len(extrasA) > 0 && lowConfidence(baseline) {
				armD.Expanded++
				hitsD = hitsA
			}
			armD.record(q.Category, hitsD, gold, lateGold)

			hitsE := baseline
			if len(extrasC) > 0 && lowConfidence(baseline) {
				armE.Expanded++
				expOnlyC, err := index.Search(strings.Join(extrasC, " "), totalDocs, slice.Project)
				if err != nil {
					return nil, err
				}
				hitsE = fuseWeighted(baseline, expOnlyC, 0.85)
			}
			armE.record(q.Category, hitsE, gold, lateGold)
		}
	}

	for _, a := range arms {
		a.finalize()
	}
	return arms, nil
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: go run ./scripts/locomo_ablation <locomo10.json>")
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		return err
	}
	var dialogues []dialogue
	if err := json.Unmarshal(data, &dialogues); err != nil {
		return err
	}
	arms, err := evaluateAll(dialogues)
	if err != nil {
		return err
	}
	out := struct {
		SHA256 string       `json:"dataset_sha256"`
		Arms   []*armReport `json:"arms"`
	}{
		SHA256: fmt.Sprintf("%x", sha256.Sum256(data)),
		Arms:   arms,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
