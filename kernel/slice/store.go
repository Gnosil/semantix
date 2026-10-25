package slice

// Store persists slices. MVP implementation: zero-dependency JSONL file store
// (NewFileStore); a bbolt-backed store can replace it later if volume demands.
type Store interface {
	Put(s *Slice) error
	Get(id string) (*Slice, error)
	List(scope Scope) ([]*Slice, error)
	UpdateStats(id string, delta SliceStats) error
	// ListAll returns every slice in the store regardless of scope
	// (maintenance commands: export/gc).
	ListAll() ([]*Slice, error)
	// Delete removes the slice with id. Removing a missing id is a no-op.
	Delete(id string) error
}

// Index provides similarity retrieval over the slice corpus (BM25 lands in U5).
type Index interface {
	Search(query string, k int, scope Scope) ([]Hit, error)
	Insert(s *Slice) error
	Remove(id string) error
}

// StatsBatcher is an optional Store capability: apply many stats deltas in
// one call. The journaled file store implements it as k O(1) appends + one
// flush; wrappers should forward it (see gateway metaStore) or callers fall
// back to per-ID UpdateStats via ApplyStats.
type StatsBatcher interface {
	UpdateStatsBatch(deltas map[string]SliceStats) error
}

// ApplyStats records usage deltas best-effort: batch when the store supports
// it, per-ID otherwise. Unlike UpdateStats, a missing ID is skipped rather
// than an error — reuse accounting must never fail a read path over a slice
// that was deleted between retrieval and write-back.
func ApplyStats(s Store, deltas map[string]SliceStats) error {
	if len(deltas) == 0 {
		return nil
	}
	if b, ok := s.(StatsBatcher); ok {
		return b.UpdateStatsBatch(deltas)
	}
	for id, d := range deltas {
		if err := s.UpdateStats(id, d); err != nil && err != errNotFound {
			return err
		}
	}
	return nil
}

// EmbeddingCache is an optional lazy vector capability. Ordinary reads keep
// Embedding nil. Backfills are disposable content-addressed sidecars, never
// whole-slice writes, so concurrent readers cannot overwrite live slice state.
type EmbeddingCache interface {
	ReadEmbedding(id string) ([]byte, error)
	ReadCachedEmbedding(id, source, contentHash string) ([]byte, error)
	CacheEmbedding(id string, content []byte, vector []float32, model, source, contentHash string) error
}
