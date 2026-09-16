package slice

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func strictStoreJSON(data []byte, dst any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return errors.New("slice: malformed or unsupported maintenance data")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("slice: trailing maintenance data")
	}
	return nil
}

// PruneFile previews by default. Apply requires an exclusive maintenance lease
// and atomically replaces the journal after archiving complete candidate rows.
// It never calls NewFileStore: even a preview of a missing/corrupt store must
// neither create files nor repair the journal.
func PruneFile(path string, opts PruneOptions) (PruneResult, error) {
	return pruneFile(path, opts, defaultPruneIO())
}

// Per-invocation IO operations let failure tests cover every pre-commit step
// without mutable global hooks or changing the store's persistence format.
type pruneIO struct {
	createTemp func(string, string) (*os.File, error)
	write      func(*os.File, []byte) (int, error)
	sync       func(*os.File) error
	close      func(*os.File) error
	rename     func(string, string) error
}

func defaultPruneIO() pruneIO {
	return pruneIO{os.CreateTemp, (*os.File).Write, (*os.File).Sync, (*os.File).Close, os.Rename}
}

func pruneFile(path string, opts PruneOptions, ops pruneIO) (PruneResult, error) {
	o, err := opts.normalized()
	r := emptyPruneResult(o)
	if err != nil {
		return r, err
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		if _, jerr := os.Stat(path + ".journal"); !os.IsNotExist(jerr) {
			return r, errors.New("prune: missing base with an unreadable or orphan journal")
		}
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if !info.Mode().IsRegular() {
		return r, errors.New("prune: database must be a regular file")
	}
	path, err = canonicalStorePath(path)
	if err != nil {
		return r, err
	}
	lease, err := lockStore(path, true, o.Apply)
	if err != nil && !(os.IsNotExist(err) && !o.Apply) {
		return r, err
	}
	if lease != nil {
		defer lease.Close()
	}
	s := &fileStore{path: path, readOnly: true, entries: map[string]*storedEntry{}}
	if err = s.load(); err != nil {
		return r, err
	}
	if s.baseSkipped+s.journalSkipped > 0 {
		return r, errors.New("prune: corrupt or unsupported records; no changes made")
	}
	if lease == nil {
		// A new writer appearing during an unleased legacy preview makes the
		// snapshot uncertain. Refuse to present it as a consistent plan.
		if _, err = os.Stat(path + ".maintenance.lock"); !os.IsNotExist(err) {
			return r, ErrStoreBusy
		}
	}
	r, err = planPrune(s.entries, o)
	if err != nil || !o.Apply || len(r.Candidates) == 0 {
		return r, err
	}
	// Serialize the original journal plus the entire batch before creating
	// any output files. No active record is changed until the final rename.
	journal, err := os.ReadFile(s.journalPath())
	if os.IsNotExist(err) {
		journal, err = json.Marshal(journalHeader{J: journalVersion, BSize: s.baseSize, BMtime: s.baseMtime, BSha: s.baseSha})
	}
	if err != nil {
		return r, err
	}
	if len(journal) > 0 && journal[len(journal)-1] != '\n' {
		journal = append(journal, '\n')
	}
	var archive bytes.Buffer
	for _, c := range r.Candidates {
		line, err := json.Marshal(dtoFromEntry(s.entries[c.ID]))
		if err != nil {
			return r, errors.New("prune: cannot serialize archive")
		}
		archive.Write(line)
		archive.WriteByte('\n')
		line, err = json.Marshal(journalRecord{Op: "del", ID: c.ID})
		if err != nil {
			return r, err
		}
		journal = append(journal, line...)
		journal = append(journal, '\n')
	}
	archivePath, err := writePruneFile(filepath.Dir(path), filepath.Base(path)+".prune-archive-*.jsonl", archive.Bytes(), ops)
	if err != nil {
		return r, fmt.Errorf("prune: archive failed; no deletion committed: %w", err)
	}
	r.ArchivePath = archivePath
	tmp, err := writePruneFile(filepath.Dir(path), filepath.Base(path)+".prune-journal-*", journal, ops)
	if err != nil {
		return r, fmt.Errorf("prune: journal preparation failed; no deletion committed (archive %q): %w", archivePath, err)
	}
	defer os.Remove(tmp)
	if err = ops.rename(tmp, s.journalPath()); err != nil {
		return r, fmt.Errorf("prune: commit failed; no deletion committed (archive %q): %w", archivePath, err)
	}
	// Commit point. No fallible persistence or compaction follows it.
	r.Removed = len(r.Candidates)
	return r, nil
}

func writePruneFile(dir, pattern string, data []byte, ops pruneIO) (path string, err error) {
	f, err := ops.createTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	path = f.Name()
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(path)
		}
	}()
	if err = f.Chmod(0o600); err != nil {
		return path, err
	}
	n, err := ops.write(f, data)
	if err != nil {
		return path, err
	}
	if n != len(data) {
		return path, io.ErrShortWrite
	}
	if err = ops.sync(f); err != nil {
		return path, err
	}
	if err = ops.close(f); err != nil {
		return path, err
	}
	return path, nil
}
