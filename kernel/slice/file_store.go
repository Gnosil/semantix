package slice

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"semantix/kernel/fingerprint"
)

// fileStore is a zero-dependency Store backed by a JSONL base file plus an
// append-only sidecar journal (<path>.journal). The base stays plain v1 JSONL
// (one Slice JSON per line, human-readable, old binaries can read it); every
// mutation is one O(1) journal append, folded back into the base by geometric
// compaction — n writes cost O(n) total instead of the old rewrite-per-Put
// O(n²). State is loaded once at open and served from memory; embedding
// vectors are kept as raw JSON bytes and never float-decoded on the read path
// (nothing in the repo reads them — retrieval is BM25 over Content).
//
// Design: docs/specs/slice-store-append-journal.md.
type fileStore struct {
	mu   sync.Mutex
	path string

	entries map[string]*storedEntry
	order   []*storedEntry // live+dead in arrival order; dead pruned at compaction

	baseSkipped    int // corrupt/oversized base lines (export surfaces these)
	journalSkipped int // corrupt/unknown journal records
	baseSize       int64
	baseMtime      int64 // UnixNano of the base generation the journal binds to
	baseSha        string

	jf             *os.File
	jw             *bufio.Writer
	jOps           int
	jBytes         int64
	opsSinceSync   int
	bytesSinceSync int64

	compactions int // white-box counter for the O(n) regression guard test
}

type storedEntry struct {
	s      Slice           // canonical state; s.Embedding is always nil
	embRaw json.RawMessage // raw "Embedding" bytes from disk/Put; nil = absent
	dead   bool
}

// sliceDTO mirrors Slice's wire format exactly, with Embedding held as raw
// bytes so loading never decodes 256 floats per line (71% of v1 open cost)
// and rewriting is byte-faithful for vectors we did not produce.
type sliceDTO struct {
	ID        string          `json:"ID"`
	Type      SliceType       `json:"Type"`
	Scope     Scope           `json:"Scope"`
	Content   []byte          `json:"Content"`
	Embedding json.RawMessage `json:"Embedding,omitempty"`
	Stats     SliceStats      `json:"Stats"`
	Weight    float64         `json:"Weight"`
	Meta      SliceMeta       `json:"Meta"`
	CreatedAt int64           `json:"created_at,omitempty"`
}

// journalVersion is the sidecar format version. A journal whose header
// declares a different version is stashed, never guessed at.
const journalVersion = 1

type journalHeader struct {
	J      int    `json:"j"`
	BSize  int64  `json:"bsize"`
	BMtime int64  `json:"bmtime"`
	BSha   string `json:"bsha"`
}

type journalRecord struct {
	Op string      `json:"op"`
	S  *sliceDTO   `json:"s,omitempty"`
	ID string      `json:"id,omitempty"`
	D  *SliceStats `json:"d,omitempty"`
}

// fsync cadence: flush (kernel handoff) happens on every public write, but
// F_FULLFSYNC-class syncs are batched — power loss can drop at most this many
// trailing records. The library is derived data (re-extractable, exportable),
// so the spec trades that window for not turning bulk import into minutes.
const (
	syncEveryOps   = 128
	syncEveryBytes = 1 << 20
)

var errNotFound = errors.New("slice: not found")

// NewFileStore opens (or creates) the JSONL slice store at path and loads it
// into memory: one pass over the base, then the journal replayed on top. A
// journal that does not match the base generation (an old binary rewrote the
// base underneath it) is moved aside to <path>.journal.stash-<ts> — base
// wins, nothing is guessed, the stash keeps the bytes for forensics.
func NewFileStore(path string) (Store, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	s := &fileStore{path: path, entries: map[string]*storedEntry{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *fileStore) load() error {
	f, err := os.Open(s.path)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	h := sha256.New()
	br := bufio.NewReader(io.TeeReader(f, h))
	for {
		line, tooLong, err := readJSONLLine(br)
		if err == io.EOF {
			break
		}
		if err != nil {
			f.Close()
			return err
		}
		if tooLong {
			s.baseSkipped++
			continue
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var dto sliceDTO
		if err := json.Unmarshal(line, &dto); err != nil || dto.ID == "" {
			// Corrupt lines and ID-less "phantom" objects are both skipped:
			// v1 let phantoms through as empty-ID slices, which broke every
			// index insert downstream. Count them so Export stays honest.
			s.baseSkipped++
			continue
		}
		s.upsertLocked(entryFromDTO(&dto))
	}
	f.Close()
	s.baseSize = st.Size()
	s.baseMtime = st.ModTime().UnixNano()
	s.baseSha = hex.EncodeToString(h.Sum(nil))
	return s.loadJournal()
}

func (s *fileStore) loadJournal() error {
	jpath := s.journalPath()
	jf, err := os.Open(jpath)
	if os.IsNotExist(err) {
		return nil // lazily created on first write
	}
	if err != nil {
		return err
	}
	br := bufio.NewReader(jf)
	headerLine, tooLong, err := readJSONLLine(br)
	if err == io.EOF || tooLong {
		jf.Close()
		return s.stashJournal("missing header")
	}
	if err != nil {
		jf.Close()
		return err
	}
	var hdr journalHeader
	if json.Unmarshal(bytes.TrimSpace(headerLine), &hdr) != nil || hdr.J == 0 {
		jf.Close()
		return s.stashJournal("malformed header")
	}
	if hdr.J != journalVersion {
		jf.Close()
		return s.stashJournal(fmt.Sprintf("unsupported journal version %d", hdr.J))
	}
	if hdr.BSize != s.baseSize || hdr.BMtime != s.baseMtime {
		jf.Close()
		return s.stashJournal("base changed underneath the journal")
	}
	for {
		line, tooLong, err := readJSONLLine(br)
		if err == io.EOF {
			break
		}
		if err != nil {
			jf.Close()
			return err
		}
		if tooLong {
			s.journalSkipped++
			continue
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		s.jOps++
		s.jBytes += int64(len(line)) + 1
		var rec journalRecord
		if json.Unmarshal(line, &rec) != nil {
			s.journalSkipped++ // torn tail or bit rot: skip, never brick
			continue
		}
		switch rec.Op {
		case "put":
			if rec.S == nil || rec.S.ID == "" {
				s.journalSkipped++
				continue
			}
			s.upsertLocked(entryFromDTO(rec.S))
		case "del":
			if e := s.entries[rec.ID]; e != nil {
				e.dead = true
				delete(s.entries, rec.ID)
			}
		case "stat":
			if e := s.entries[rec.ID]; e != nil && rec.D != nil {
				mergeStats(&e.s.Stats, *rec.D)
			}
		default:
			s.journalSkipped++ // forward compatibility: unknown op, skip
		}
	}
	jf.Close()
	s.jf, err = os.OpenFile(jpath, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	s.jw = bufio.NewWriter(s.jf)
	return nil
}

func (s *fileStore) journalPath() string { return s.path + ".journal" }

// stashJournal moves an out-of-sync journal aside. Base wins: replaying
// records of unknown ordering against a newer base would let stale puts
// overwrite fresh data, which is real corruption — a stash is diagnosable
// and recoverable, a silent stale-wins merge is neither.
func (s *fileStore) stashJournal(reason string) error {
	stash := s.journalPath() + ".stash-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := os.Rename(s.journalPath(), stash); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "semantix: slice store %s: journal out of sync (%s); moved to %s, base wins\n",
		s.path, reason, stash)
	return nil
}

// upsertLocked installs e as the live entry for its ID, marking any previous
// generation dead (order keeps arrival order; dead entries are pruned at
// compaction, so per-op cost stays O(1)).
func (s *fileStore) upsertLocked(e *storedEntry) {
	if old := s.entries[e.s.ID]; old != nil {
		old.dead = true
	}
	s.entries[e.s.ID] = e
	s.order = append(s.order, e)
}

// ensureJournalLocked creates the sidecar on first write (lazily: read-only
// commands must leave no write side effects). The header binds the journal
// to the base generation loaded at open.
func (s *fileStore) ensureJournalLocked() error {
	if s.jw != nil {
		return nil
	}
	hdr, err := json.Marshal(journalHeader{J: journalVersion, BSize: s.baseSize, BMtime: s.baseMtime, BSha: s.baseSha})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".journal.tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(hdr, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, s.journalPath()); err != nil {
		return err
	}
	jf, err := os.OpenFile(s.journalPath(), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	s.jf, s.jw = jf, bufio.NewWriter(jf)
	s.jOps, s.jBytes = 0, 0
	return nil
}

func (s *fileStore) appendLocked(rec journalRecord) error {
	b, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if _, err := s.jw.Write(append(b, '\n')); err != nil {
		return err
	}
	n := int64(len(b)) + 1
	s.jOps++
	s.jBytes += n
	s.opsSinceSync++
	s.bytesSinceSync += n
	return nil
}

func (s *fileStore) flushLocked() error {
	if err := s.jw.Flush(); err != nil {
		return err
	}
	if s.opsSinceSync >= syncEveryOps || s.bytesSinceSync >= syncEveryBytes {
		if err := s.jf.Sync(); err != nil {
			return err
		}
		s.opsSinceSync, s.bytesSinceSync = 0, 0
	}
	return nil
}

// Put inserts s, replacing any existing slice with the same ID.
func (s *fileStore) Put(sl *Slice) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := entryFromSlice(sl)
	if err := s.ensureJournalLocked(); err != nil {
		return err
	}
	if err := s.appendLocked(journalRecord{Op: "put", S: dtoFromEntry(e)}); err != nil {
		return err
	}
	if err := s.flushLocked(); err != nil {
		return err
	}
	s.upsertLocked(e)
	return s.maybeCompactLocked()
}

// Get returns the slice with id, or (nil, nil) when not found.
func (s *fileStore) Get(id string) (*Slice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[id]
	if e == nil {
		return nil, nil
	}
	return cloneStored(e), nil
}

// List returns all slices of the given scope.
func (s *fileStore) List(scope Scope) ([]*Slice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Slice
	for _, e := range s.order {
		if !e.dead && e.s.Scope == scope {
			out = append(out, cloneStored(e))
		}
	}
	return out, nil
}

// ListAll returns every slice in the store, regardless of scope.
func (s *fileStore) ListAll() ([]*Slice, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Slice, 0, len(s.entries))
	for _, e := range s.order {
		if !e.dead {
			out = append(out, cloneStored(e))
		}
	}
	return out, nil
}

// Delete removes the slice with id; a missing id is a no-op (and writes
// nothing — a delete of nothing should not create a journal).
func (s *fileStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[id]
	if e == nil {
		return nil
	}
	if err := s.ensureJournalLocked(); err != nil {
		return err
	}
	if err := s.appendLocked(journalRecord{Op: "del", ID: id}); err != nil {
		return err
	}
	if err := s.flushLocked(); err != nil {
		return err
	}
	e.dead = true
	delete(s.entries, id)
	return s.maybeCompactLocked()
}

// UpdateStats applies delta to the slice's stats (counters accumulate,
// LastUsed max-merges — same rule journal replay uses).
func (s *fileStore) UpdateStats(id string, delta SliceStats) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[id]
	if e == nil {
		return errNotFound
	}
	if err := s.ensureJournalLocked(); err != nil {
		return err
	}
	if err := s.appendLocked(journalRecord{Op: "stat", ID: id, D: &delta}); err != nil {
		return err
	}
	if err := s.flushLocked(); err != nil {
		return err
	}
	mergeStats(&e.s.Stats, delta)
	return s.maybeCompactLocked()
}

// UpdateStatsBatch applies many deltas with one flush. Missing IDs are
// skipped (best-effort reuse accounting), unlike single UpdateStats.
func (s *fileStore) UpdateStatsBatch(deltas map[string]SliceStats) error {
	if len(deltas) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(deltas))
	for id := range deltas {
		if s.entries[id] != nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Strings(ids) // deterministic journal bytes
	if err := s.ensureJournalLocked(); err != nil {
		return err
	}
	for _, id := range ids {
		d := deltas[id]
		if err := s.appendLocked(journalRecord{Op: "stat", ID: id, D: &d}); err != nil {
			return err
		}
	}
	if err := s.flushLocked(); err != nil {
		return err
	}
	for _, id := range ids {
		mergeStats(&s.entries[id].s.Stats, deltas[id])
	}
	return s.maybeCompactLocked()
}

// Close flushes and fsyncs the journal. Idempotent; reads keep working.
func (s *fileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.jw == nil {
		return nil
	}
	if err := s.jw.Flush(); err != nil {
		return err
	}
	if err := s.jf.Sync(); err != nil {
		return err
	}
	err := s.jf.Close()
	s.jf, s.jw = nil, nil
	return err
}

// exportLines renders every live slice as a complete v1 JSONL line (raw
// embedding bytes included) plus the corrupt-line count — the maintenance
// Export contract: backups are full-fidelity and never silently incomplete.
func (s *fileStore) exportLines() ([][]byte, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lines := make([][]byte, 0, len(s.entries))
	for _, e := range s.order {
		if e.dead {
			continue
		}
		b, err := json.Marshal(dtoFromEntry(e))
		if err != nil {
			return nil, 0, err
		}
		lines = append(lines, b)
	}
	return lines, s.baseSkipped + s.journalSkipped, nil
}

// compaction trigger thresholds (geometric): fold when the journal has at
// least as many records as there are live entries (and a floor so tiny
// stores don't fold constantly), or when its bytes reach the base size.
// Between two folds at least liveCount appends happen, so each record pays
// O(1) amortized and n writes cost O(n) total. Hardcoded on purpose — a
// config knob here would be another retrieval.vector_dim-style dead key.
const (
	compactMinOps   = 1024
	compactMinBytes = 4 << 20
)

// maybeCompactLocked runs the geometric trigger inline on the write path
// (never from a background goroutine: the freeze-window contract wants the
// store's visible state to change only at op boundaries, and an identity
// fold does not change visible state at all).
func (s *fileStore) maybeCompactLocked() error {
	opsTrig := s.jOps >= compactMinOps && s.jOps >= len(s.entries)
	byteTrig := s.jBytes >= compactMinBytes && s.jBytes >= s.baseSize
	if !opsTrig && !byteTrig {
		return nil
	}
	return s.compactLocked(nil)
}

// Compact folds the journal into the base (identity: logical state is
// unchanged; corrupt base lines are dropped for good). No-op when there is
// nothing to fold, no corrupt lines to shed and no journal file to retire —
// so callers can run it unconditionally at process boundaries (gc, gateway
// startup) without write amplification on clean stores.
func (s *fileStore) Compact() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Nothing folded, nothing corrupt to shed → true no-op (a header-only
	// journal may linger; it stays correctly bound to the base generation).
	if s.jOps == 0 && s.baseSkipped == 0 && s.journalSkipped == 0 {
		return nil
	}
	return s.compactLocked(nil)
}

// CompactWith folds like Compact but passes the live slices through rescore
// first: the callback may mutate them (Weight) and drop entries (eviction)
// by returning the kept subset. It runs under the store lock — the callback
// must not call back into the store. Raw embedding bytes are preserved for
// kept IDs even though the callback sees Embedding=nil clones.
func (s *fileStore) CompactWith(rescore func([]*Slice) []*Slice) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.compactLocked(rescore)
}

func (s *fileStore) compactLocked(rescore func([]*Slice) []*Slice) error {
	lives := make([]*storedEntry, 0, len(s.entries))
	for _, e := range s.order {
		if !e.dead {
			lives = append(lives, e)
		}
	}
	kept := lives
	if rescore != nil {
		clones := make([]*Slice, len(lives))
		for i, e := range lives {
			clones[i] = cloneStored(e)
		}
		rawByID := make(map[string]json.RawMessage, len(lives))
		for _, e := range lives {
			rawByID[e.s.ID] = e.embRaw
		}
		kept = make([]*storedEntry, 0, len(lives))
		for _, sl := range rescore(clones) {
			if sl == nil || sl.ID == "" {
				continue
			}
			e := entryFromSlice(sl)
			e.embRaw = rawByID[sl.ID]
			kept = append(kept, e)
		}
	}

	// New base bytes; hash while building so the fresh header binds to the
	// exact bytes on disk.
	var buf bytes.Buffer
	h := sha256.New()
	w := io.MultiWriter(&buf, h)
	for _, e := range kept {
		b, err := json.Marshal(dtoFromEntry(e))
		if err != nil {
			return err
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	// Retire the current journal fd before swapping files. Crash between the
	// base rename and the fresh-header rename leaves new base + old journal:
	// the next open sees the generation mismatch and stashes a journal whose
	// content is already folded in — zero loss by construction.
	if s.jw != nil {
		if err := s.jw.Flush(); err != nil {
			return err
		}
		if err := s.jf.Close(); err != nil {
			return err
		}
		s.jf, s.jw = nil, nil
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return err
	}
	st, err := os.Stat(s.path)
	if err != nil {
		return err
	}
	s.baseSize = st.Size()
	s.baseMtime = st.ModTime().UnixNano()
	s.baseSha = hex.EncodeToString(h.Sum(nil))
	if err := os.Remove(s.journalPath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := s.ensureJournalLocked(); err != nil {
		return err
	}

	s.opsSinceSync, s.bytesSinceSync = 0, 0
	s.baseSkipped, s.journalSkipped = 0, 0
	s.entries = make(map[string]*storedEntry, len(kept))
	s.order = append([]*storedEntry(nil), kept...)
	for _, e := range kept {
		s.entries[e.s.ID] = e
	}
	s.compactions++
	return nil
}

// ---------------------------------------------------------------------------
// entry/DTO conversion

// entryFromSlice deep-copies the caller's slice (callers may mutate their
// value after Put; v1's immediate rewrite gave the same isolation) and
// captures the embedding as raw bytes.
func entryFromSlice(sl *Slice) *storedEntry {
	e := &storedEntry{s: *sl}
	e.s.Content = append([]byte(nil), sl.Content...)
	e.s.Meta.Deps = cloneDeps(sl.Meta.Deps)
	e.s.Meta.Mtimes = cloneMtimes(sl.Meta.Mtimes)
	e.s.Embedding = nil
	if sl.Embedding != nil {
		b, err := json.Marshal(sl.Embedding)
		if err == nil {
			e.embRaw = b
		}
	}
	return e
}

func entryFromDTO(dto *sliceDTO) *storedEntry {
	raw := dto.Embedding
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		raw = nil // v1 wrote "Embedding":null for nil; normalize away
	}
	return &storedEntry{
		s: Slice{
			ID: dto.ID, Type: dto.Type, Scope: dto.Scope, Content: dto.Content,
			Stats: dto.Stats, Weight: dto.Weight, Meta: dto.Meta, CreatedAt: dto.CreatedAt,
		},
		embRaw: raw,
	}
}

func dtoFromEntry(e *storedEntry) *sliceDTO {
	return &sliceDTO{
		ID: e.s.ID, Type: e.s.Type, Scope: e.s.Scope, Content: e.s.Content,
		Embedding: e.embRaw, Stats: e.s.Stats, Weight: e.s.Weight,
		Meta: e.s.Meta, CreatedAt: e.s.CreatedAt,
	}
}

// cloneStored returns an independent copy: callers can mutate results without
// corrupting the in-memory state (v1 re-parsed the file per call, so every
// caller historically got fresh objects). Embedding stays nil by contract —
// no code path in the repo reads persisted vectors.
func cloneStored(e *storedEntry) *Slice {
	c := e.s
	c.Content = append([]byte(nil), e.s.Content...)
	c.Meta.Deps = cloneDeps(e.s.Meta.Deps)
	c.Meta.Mtimes = cloneMtimes(e.s.Meta.Mtimes)
	c.Embedding = nil
	return &c
}

func cloneDeps(d fingerprint.Deps) fingerprint.Deps {
	if d == nil {
		return nil
	}
	out := make(fingerprint.Deps, len(d))
	for k, v := range d {
		out[k] = v
	}
	return out
}

func cloneMtimes(m map[string]int64) map[string]int64 {
	if m == nil {
		return nil
	}
	out := make(map[string]int64, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
