package slice

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"sync"
)

// fileStore is a zero-dependency Store backed by a JSONL file (one Slice JSON
// per line). Chosen over bbolt for the MVP because it keeps the kernel
// dependency-free and the store human-readable; swap to bbolt if volume
// demands it later. Writes rewrite the file (slice volumes are small).
type fileStore struct {
	mu   sync.Mutex
	path string
}

// NewFileStore opens (or creates) the JSONL slice store at path.
func NewFileStore(path string) (Store, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	return &fileStore{path: path}, nil
}

// Put inserts s, replacing any existing slice with the same ID.
func (s *fileStore) Put(sl *Slice) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return err
	}
	kept := make([]*Slice, 0, len(all)+1)
	for _, e := range all {
		if e.ID != sl.ID {
			kept = append(kept, e)
		}
	}
	kept = append(kept, sl)
	return s.writeAll(kept)
}

// Get returns the slice with id, or (nil, nil) when not found.
func (s *fileStore) Get(id string) (*Slice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return nil, err
	}
	for _, e := range all {
		if e.ID == id {
			return e, nil
		}
	}
	return nil, nil
}

// List returns all slices of the given scope.
func (s *fileStore) List(scope Scope) ([]*Slice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return nil, err
	}
	var out []*Slice
	for _, e := range all {
		if e.Scope == scope {
			out = append(out, e)
		}
	}
	return out, nil
}

// UpdateStats applies delta to the slice's stats.
func (s *fileStore) UpdateStats(id string, delta SliceStats) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.readAll()
	if err != nil {
		return err
	}
	for i := range all {
		if all[i].ID == id {
			all[i].Stats.Hits += delta.Hits
			all[i].Stats.Misses += delta.Misses
			all[i].Stats.Injected += delta.Injected
			all[i].Stats.Rejected += delta.Rejected
			all[i].Stats.UserFeedback += delta.UserFeedback
			return s.writeAll(all)
		}
	}
	return errors.New("slice: not found")
}

func (s *fileStore) readAll() ([]*Slice, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []*Slice
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // tolerate slices up to 8 MiB
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var sl Slice
		if err := json.Unmarshal(line, &sl); err != nil {
			continue // tolerant: skip corrupt lines
		}
		out = append(out, &sl)
	}
	return out, sc.Err()
}

func (s *fileStore) writeAll(slices []*Slice) error {
	var buf bytes.Buffer
	for _, sl := range slices {
		b, err := json.Marshal(sl)
		if err != nil {
			return err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	tmp := s.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil { // durability: flush before rename
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
