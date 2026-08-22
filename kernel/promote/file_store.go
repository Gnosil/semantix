package promote

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// fileStore is a zero-dependency disk Store backed by a JSONL file (one Entry
// JSON per line), mirroring kernel/slice/file_store.go: 0600 permissions,
// atomic write via temp-file + rename, and tolerant reads that skip corrupt
// lines. Entry volumes are small, so writes rewrite the file in place under a
// mutex (same trade-off as the slice store).
type fileStore struct {
	mu   sync.Mutex
	path string
}

// NewFileStore opens (or creates) the JSONL entry store at path.
func NewFileStore(path string) (Store, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()
	return &fileStore{path: path}, nil
}

// Put appends an entry, deduplicating by the exact (source, version, query)
// triple — same semantics as MemStore.Put.
func (s *fileStore) Put(e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return err
	}
	for _, old := range all {
		if old.SourceSliceID == e.SourceSliceID && old.ContentVersion == e.ContentVersion && old.Query == e.Query {
			return nil // duplicate
		}
	}
	all = append(all, e)
	return s.writeAll(all)
}

// List returns entries for a source slice, newest last (by PromotedAt, then
// insertion order — stable for tests, matching MemStore.List).
func (s *fileStore) List(sourceSliceID string) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range all {
		if e.SourceSliceID == sourceSliceID {
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].PromotedAt < out[j].PromotedAt })
	return out, nil
}

// Delete removes one exact entry (no-op when absent).
func (s *fileStore) Delete(sourceSliceID, contentVersion, query string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return err
	}
	kept := all[:0]
	for _, e := range all {
		if e.SourceSliceID == sourceSliceID && e.ContentVersion == contentVersion && e.Query == query {
			continue
		}
		kept = append(kept, e)
	}
	return s.writeAll(kept)
}

// readAll decodes every entry line, skipping blank and corrupt lines
// (tolerant reads — a partial write must not brick the store).
func (s *fileStore) readAll() ([]Entry, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // tolerate entries up to 8 MiB
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue // tolerant: skip corrupt lines
		}
		out = append(out, e)
	}
	return out, sc.Err()
}

// writeAll rewrites the whole store via a uniquely-named temp file (0600)
// then an atomic rename: avoids symlink pre-placement attacks and never
// exposes the store world-readable (os.Create would use 0666&umask).
func (s *fileStore) writeAll(entries []Entry) error {
	var buf bytes.Buffer
	for _, e := range entries {
		b, err := json.Marshal(e)
		if err != nil {
			return err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil { // durability: flush before rename
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return err
	}
	return nil
}
