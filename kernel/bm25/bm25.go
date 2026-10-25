package bm25

import (
	"errors"
	"math"
	"sort"
	"sync"

	"semantix/kernel/slice"
)

const (
	defaultK1 = 1.2
	defaultB  = 0.75
)

type document struct {
	value  *slice.Slice
	terms  map[string]int
	length int
}

// Index is an in-memory BM25 retrieval index over slices.
type Index struct {
	mu sync.RWMutex

	k1 float64
	b  float64

	docs             map[string]*document
	postings         map[string]map[string]int
	scopeDocs        map[slice.Scope]map[string]struct{}
	scopeDF          map[slice.Scope]map[string]int
	scopeTotalLength map[slice.Scope]int
}

// New returns an Index with the reference parameters.
func New() *Index {
	return &Index{
		k1:               defaultK1,
		b:                defaultB,
		docs:             make(map[string]*document),
		postings:         make(map[string]map[string]int),
		scopeDocs:        make(map[slice.Scope]map[string]struct{}),
		scopeDF:          make(map[slice.Scope]map[string]int),
		scopeTotalLength: make(map[slice.Scope]int),
	}
}

// Search returns the top-k matches for query within scope.
func (ix *Index) Search(query string, k int, scope slice.Scope) ([]slice.Hit, error) {
	if ix == nil {
		return nil, errors.New("bm25: nil index")
	}
	if k <= 0 {
		return []slice.Hit{}, nil
	}

	queryTerms := uniqueTerms(Tokenize(query))
	if len(queryTerms) == 0 {
		return nil, errors.New("bm25: query must contain at least one letter or number")
	}

	ix.mu.RLock()
	defer ix.mu.RUnlock()

	docIDs := ix.scopeDocs[scope]
	totalDocs := len(docIDs)
	if totalDocs == 0 {
		return []slice.Hit{}, nil
	}

	avgLength := float64(ix.scopeTotalLength[scope]) / float64(totalDocs)
	if avgLength <= 0 {
		return []slice.Hit{}, nil
	}

	candidates := make(map[string]struct{})
	for _, term := range queryTerms {
		for id := range ix.postings[term] {
			if _, ok := docIDs[id]; ok {
				candidates[id] = struct{}{}
			}
		}
	}

	hits := make([]slice.Hit, 0, len(candidates))
	for id := range candidates {
		doc := ix.docs[id]
		score := ix.score(doc, queryTerms, scope, totalDocs, avgLength)
		if score <= 0 {
			continue
		}
		// BM25 is lexical retrieval: a hit is lexical support by definition
		// (Issue #260 lexical support gate).
		hits = append(hits, slice.Hit{Slice: cloneSlice(doc.value), Score: score, Lexical: 1, LexicalValid: true})
	}

	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Score == hits[j].Score {
			return hits[i].Slice.ID < hits[j].Slice.ID
		}
		return hits[i].Score > hits[j].Score
	})
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits, nil
}

func (ix *Index) score(doc *document, queryTerms []string, scope slice.Scope, totalDocs int, avgLength float64) float64 {
	if doc == nil || doc.length == 0 {
		return 0
	}

	var score float64
	docLength := float64(doc.length)
	for _, term := range queryTerms {
		tf := doc.terms[term]
		df := ix.scopeDF[scope][term]
		if tf == 0 || df == 0 {
			continue
		}

		idf := math.Log(1 + (float64(totalDocs)-float64(df)+0.5)/(float64(df)+0.5))
		frequency := float64(tf)
		numerator := frequency * (ix.k1 + 1)
		denominator := frequency + ix.k1*(1-ix.b+ix.b*docLength/avgLength)
		score += idf * numerator / denominator
	}
	return score
}

// Insert adds s to the index. An existing slice with the same ID is replaced.
// Tokenless content has no lexical representation: omit it (and remove any old
// version) rather than letting one legacy row abort an entire index rebuild.
func (ix *Index) Insert(s *slice.Slice) error {
	if ix == nil {
		return errors.New("bm25: nil index")
	}
	if s == nil {
		return errors.New("bm25: nil slice")
	}
	if s.ID == "" {
		return errors.New("bm25: empty slice ID")
	}

	terms := Tokenize(string(s.Content))
	if len(terms) == 0 {
		return ix.Remove(s.ID)
	}
	doc := &document{
		value:  cloneSlice(s),
		terms:  countTerms(terms),
		length: len(terms),
	}

	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.initLocked()

	if old := ix.docs[s.ID]; old != nil {
		ix.removeLocked(s.ID, old)
	}

	ix.docs[s.ID] = doc
	scope := doc.value.Scope
	if ix.scopeDocs[scope] == nil {
		ix.scopeDocs[scope] = make(map[string]struct{})
	}
	if ix.scopeDF[scope] == nil {
		ix.scopeDF[scope] = make(map[string]int)
	}
	ix.scopeDocs[scope][s.ID] = struct{}{}
	ix.scopeTotalLength[scope] += doc.length

	for term, frequency := range doc.terms {
		if ix.postings[term] == nil {
			ix.postings[term] = make(map[string]int)
		}
		ix.postings[term][s.ID] = frequency
		ix.scopeDF[scope][term]++
	}
	return nil
}

// Remove deletes s from the index. Removing an unknown ID is a no-op.
func (ix *Index) Remove(id string) error {
	if ix == nil {
		return errors.New("bm25: nil index")
	}
	if id == "" {
		return errors.New("bm25: empty slice ID")
	}

	ix.mu.Lock()
	defer ix.mu.Unlock()
	if doc := ix.docs[id]; doc != nil {
		ix.removeLocked(id, doc)
	}
	return nil
}

func (ix *Index) initLocked() {
	if ix.k1 == 0 {
		ix.k1 = defaultK1
	}
	if ix.b == 0 {
		ix.b = defaultB
	}
	if ix.docs == nil {
		ix.docs = make(map[string]*document)
	}
	if ix.postings == nil {
		ix.postings = make(map[string]map[string]int)
	}
	if ix.scopeDocs == nil {
		ix.scopeDocs = make(map[slice.Scope]map[string]struct{})
	}
	if ix.scopeDF == nil {
		ix.scopeDF = make(map[slice.Scope]map[string]int)
	}
	if ix.scopeTotalLength == nil {
		ix.scopeTotalLength = make(map[slice.Scope]int)
	}
}

func (ix *Index) removeLocked(id string, doc *document) {
	delete(ix.docs, id)
	scope := doc.value.Scope
	delete(ix.scopeDocs[scope], id)
	ix.scopeTotalLength[scope] -= doc.length

	for term := range doc.terms {
		delete(ix.postings[term], id)
		if len(ix.postings[term]) == 0 {
			delete(ix.postings, term)
		}

		ix.scopeDF[scope][term]--
		if ix.scopeDF[scope][term] == 0 {
			delete(ix.scopeDF[scope], term)
		}
	}
	if len(ix.scopeDocs[scope]) == 0 {
		delete(ix.scopeDocs, scope)
		delete(ix.scopeDF, scope)
		delete(ix.scopeTotalLength, scope)
	}
}

func cloneSlice(s *slice.Slice) *slice.Slice {
	if s == nil {
		return nil
	}
	cloned := *s
	cloned.Content = append([]byte(nil), s.Content...)
	cloned.Embedding = append([]float32(nil), s.Embedding...)
	return &cloned
}
