package slice

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"sort"
	"time"
)

// rawExporter is implemented by fileStore: full-fidelity v1 JSONL lines (raw
// embedding bytes preserved even though reads return Embedding=nil) plus the
// corrupt-line count. Other Store implementations fall back to ListAll and
// report zero skipped.
type rawExporter interface {
	exportLines() ([][]byte, int, error)
}

// Export writes every slice in the store to w as JSONL — one full Slice JSON
// per line, including Meta, Stats, Weight and CreatedAt. The output is a
// complete backup and a valid Import input (export/import round-trip is
// lossless). Returns the number of slices written and how many store lines
// were corrupt and dropped (so a "lossless backup" is never silently
// incomplete).
func Export(store Store, w io.Writer) (count, skipped int, err error) {
	if re, ok := store.(rawExporter); ok {
		lines, skipped, err := re.exportLines()
		if err != nil {
			return 0, skipped, err
		}
		n := 0
		for _, line := range lines {
			if _, err := w.Write(append(line, '\n')); err != nil {
				return n, skipped, err
			}
			n++
		}
		return n, skipped, nil
	}
	all, err := store.ListAll()
	if err != nil {
		return 0, 0, err
	}
	n := 0
	for _, sl := range all {
		b, err := json.Marshal(sl)
		if err != nil {
			return n, 0, err
		}
		if _, err := w.Write(append(b, '\n')); err != nil {
			return n, 0, err
		}
		n++
	}
	return n, 0, nil
}

// maxImportLine bounds a single accepted JSONL line; longer lines are treated
// as corrupt input and skipped (counted), never fatal. Matches the store's
// own 8 MiB tolerance (fileStore.readAll).
const maxImportLine = 8 * 1024 * 1024

// Import reads JSONL produced by Export (or any JSONL store file) and stores
// each slice. Idempotent: Put replaces by ID, so re-importing the same backup
// converges to the same store state without duplicates. Corrupt, ID-less and
// oversized (> 8 MiB) lines are skipped and counted, so one bad line never
// aborts a restore. Returns imported and skipped counts.
func Import(store Store, r io.Reader) (imported, skipped int, err error) {
	br := bufio.NewReader(r)
	for {
		line, tooLong, err := readJSONLLine(br)
		if err == io.EOF {
			return imported, skipped, nil // clean end of stream
		}
		if err != nil {
			return imported, skipped, err
		}
		if tooLong {
			// Oversized lines are corrupt input: skip and keep going.
			skipped++
			continue
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var sl Slice
		if err := json.Unmarshal(line, &sl); err != nil {
			skipped++
			continue
		}
		if sl.ID == "" {
			// An unkeyed slice would clobber every other empty-ID slice
			// in the store; refuse it rather than corrupt data silently.
			skipped++
			continue
		}
		if err := store.Put(&sl); err != nil {
			return imported, skipped, err
		}
		imported++
	}
}

// readJSONLLine reads one logical line (up to and including '\n') from br.
// A line longer than maxImportLine is drained (skipped) and reported via
// tooLong=true so the import continues with the following lines. Returns
// io.EOF when the stream ends cleanly at a line boundary.
func readJSONLLine(br *bufio.Reader) (line []byte, tooLong bool, err error) {
	var full []byte
	for {
		frag, ferr := br.ReadSlice('\n')
		full = append(full, frag...)
		if len(full) > maxImportLine {
			// Drain the remainder of the oversized line, then report skip.
			for ferr == bufio.ErrBufferFull {
				_, ferr = br.ReadSlice('\n')
			}
			return nil, true, nil
		}
		if ferr == bufio.ErrBufferFull {
			continue
		}
		if ferr == io.EOF && len(full) == 0 {
			return nil, false, io.EOF
		}
		// ferr is nil (line ended with '\n') or io.EOF with trailing data.
		return full, false, nil
	}
}

// GCOptions configures a maintenance GC pass. Zero values disable the
// corresponding criterion, so a bare `GC(store, GCOptions{})` removes nothing.
type GCOptions struct {
	// RetentionDays: slices older than this many days are expired. Slices
	// with unknown (zero) CreatedAt are never expired by retention.
	RetentionDays int
	// MinWeight: slices whose Weight is strictly below this are low-score.
	// Slices with zero Weight are treated as unknown (legacy, pre-U26 data
	// never went through the evolution engine) and are never removed by the
	// score criterion — same protection as unknown CreatedAt.
	MinWeight float64
	// DryRun reports candidates without deleting, archiving, or persisting
	// rescored weights.
	DryRun bool
	// MaxSlices caps the library size: after the threshold criteria, the
	// worst-scored survivors are evicted down to this count. Unlike the
	// threshold criteria, the cap exempts nothing — a cap that legacy
	// entries can overflow is not a cap (they compete on the neutral
	// rescore prior and lose ties by age). 0 disables.
	MaxSlices int
	// Rescore recomputes every Weight (ComputeWeight) before applying the
	// criteria, turning gc into the offline scoring pass of 架构 §3.2 ④⑤.
	Rescore bool
	// Params tunes scoring and the eviction grace window (zero → defaults;
	// Params.GraceDays 0 disables grace).
	Params ScoreParams
	// Now is the scoring clock (unix seconds); 0 → time.Now(). Injectable so
	// the same library + same Now reproduce byte-identical decisions.
	Now int64
	// ArchivePath: when non-empty, evicted slices are appended there (Export
	// line format — `semantix import` restores them) before removal. Written
	// before deletion, so a crash can only leave duplicates, which Import
	// resolves idempotently.
	ArchivePath string
}

// GCResult summarizes one GC pass.
type GCResult struct {
	Checked         int      // slices inspected
	Removed         int      // slices removed (or that would be, in dry-run)
	Expired         []string // ids expired by retention
	LowScore        []string // ids below the weight threshold
	OverCap         []string // ids evicted by the capacity cap
	Capacity        int      // effective MaxSlices (0 = unlimited)
	Archived        int      // slices appended to ArchivePath
	RescoredWeights int      // weights that changed in the rescore pass
}

// GC is the scoring + eviction pass over the store. Pipeline: optional
// rescore → threshold criteria (retention OR min-weight; legacy exemptions
// preserved) → capacity cap (deterministic order: unprotected-by-grace
// first, then Weight asc, CreatedAt asc with 0 oldest, ID asc) → archive →
// apply. On stores supporting CompactWith the apply is a single fold that
// also persists new weights and keeps raw embedding bytes intact; otherwise
// it falls back to per-ID Delete/Put. With DryRun nothing is persisted.
func GC(store Store, opts GCOptions) (GCResult, error) {
	res := GCResult{Capacity: opts.MaxSlices}
	all, err := store.ListAll()
	if err != nil {
		return res, err
	}
	res.Checked = len(all)
	now := opts.Now
	if now == 0 {
		now = time.Now().Unix()
	}
	if opts.Rescore {
		res.RescoredWeights = Rescore(all, now, opts.Params)
	}

	evict := make(map[string]bool)
	cutoff := now - int64(opts.RetentionDays)*86400
	for _, sl := range all {
		expired := opts.RetentionDays > 0 && sl.CreatedAt > 0 && sl.CreatedAt < cutoff
		low := opts.MinWeight > 0 && sl.Weight > 0 && sl.Weight < opts.MinWeight
		if expired {
			res.Expired = append(res.Expired, sl.ID)
		}
		if low {
			res.LowScore = append(res.LowScore, sl.ID)
		}
		if expired || low {
			evict[sl.ID] = true
		}
	}

	if opts.MaxSlices > 0 {
		live := make([]*Slice, 0, len(all))
		for _, sl := range all {
			if !evict[sl.ID] {
				live = append(live, sl)
			}
		}
		if over := len(live) - opts.MaxSlices; over > 0 {
			graceSecs := int64(opts.Params.withDefaults().GraceDays * 86400)
			protected := func(sl *Slice) bool {
				return graceSecs > 0 && sl.CreatedAt > 0 && now-sl.CreatedAt < graceSecs
			}
			sort.SliceStable(live, func(i, j int) bool {
				a, b := live[i], live[j]
				if pa, pb := protected(a), protected(b); pa != pb {
					return !pa // grace-protected sort last (evicted last)
				}
				if a.Weight != b.Weight {
					return a.Weight < b.Weight
				}
				if a.CreatedAt != b.CreatedAt {
					return a.CreatedAt < b.CreatedAt // 0 = unknown = oldest
				}
				return a.ID < b.ID
			})
			for _, sl := range live[:over] {
				evict[sl.ID] = true
				res.OverCap = append(res.OverCap, sl.ID)
			}
		}
	}
	res.Removed = len(evict)
	if opts.DryRun {
		return res, nil
	}

	if opts.ArchivePath != "" && len(evict) > 0 {
		n, err := archiveEvicted(store, all, evict, opts.ArchivePath)
		if err != nil {
			return res, err
		}
		res.Archived = n
	}

	changed := len(evict) > 0 || res.RescoredWeights > 0
	if cw, ok := store.(interface {
		CompactWith(func([]*Slice) []*Slice) error
	}); ok && changed {
		weights := make(map[string]float64, len(all))
		for _, sl := range all {
			weights[sl.ID] = sl.Weight
		}
		if err := cw.CompactWith(func(live []*Slice) []*Slice {
			kept := live[:0]
			for _, sl := range live {
				if evict[sl.ID] {
					continue
				}
				if w, ok := weights[sl.ID]; ok {
					sl.Weight = w
				}
				kept = append(kept, sl)
			}
			return kept
		}); err != nil {
			return res, err
		}
		return res, nil
	}
	if changed {
		// Fallback for stores without CompactWith (fakes, future backends):
		// per-ID ops, deterministic order.
		ids := make([]string, 0, len(evict))
		for id := range evict {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			if err := store.Delete(id); err != nil {
				return res, err
			}
		}
		if opts.Rescore {
			for _, sl := range all {
				if !evict[sl.ID] {
					if err := store.Put(sl); err != nil {
						return res, err
					}
				}
			}
		}
		return res, nil
	}
	// Nothing to apply: still fold any pending journal so gc keeps doubling
	// as the downgrade path (no-op on a clean store).
	if c, ok := store.(interface{ Compact() error }); ok {
		return res, c.Compact()
	}
	return res, nil
}

// archiveEvicted appends the evicted slices to path in Export line format.
// It prefers the store's raw lines (embedding bytes preserved verbatim);
// weights in the archive are the last persisted ones — the archive records
// what the store held, the eviction decision used the rescored values.
func archiveEvicted(store Store, all []*Slice, evict map[string]bool, path string) (int, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n := 0
	if re, ok := store.(rawExporter); ok {
		lines, _, err := re.exportLines()
		if err != nil {
			return 0, err
		}
		for _, line := range lines {
			var id struct {
				ID string `json:"ID"`
			}
			if json.Unmarshal(line, &id) != nil || !evict[id.ID] {
				continue
			}
			if _, err := f.Write(append(line, '\n')); err != nil {
				return n, err
			}
			n++
		}
		return n, f.Sync()
	}
	for _, sl := range all {
		if !evict[sl.ID] {
			continue
		}
		b, err := json.Marshal(sl)
		if err != nil {
			return n, err
		}
		if _, err := f.Write(append(b, '\n')); err != nil {
			return n, err
		}
		n++
	}
	return n, f.Sync()
}
