package semantix

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// auditEntry is one line of the injection audit journal: every admitted L2
// injection exactly as it becomes provider-visible (the full [semantix-reuse]
// block), plus the query it was assembled for and the admitted slice targets.
// The runner copies the journal into run_dir/audit/<instance>.jsonl and the
// SWE-bench Track B leakage scan (Issue #326 §六 #4) checks each block against
// the instance being solved's gold patch and FAIL_TO_PASS test names — if a
// slice library distilled from earlier same-repo instances ever carried the
// answer, the scan flags it.
type auditEntry struct {
	Seq       int64    `json:"seq"`
	At        int64    `json:"at"` // unix ms (UTC)
	Session   string   `json:"session,omitempty"`
	Query     string   `json:"query"`        // truncated to maxAuditQueryRunes
	QueryHash string   `json:"query_sha256"` // full query hash (untruncated)
	Degraded  bool     `json:"degraded"`
	Budget    int      `json:"budget"`
	Bytes     int      `json:"bytes"`
	Targets   []string `json:"targets"`
	Text      string   `json:"text"`
}

// maxAuditQueryRunes bounds the stored query copy; the injected block itself
// is already budget-capped by the kernel injector.
const maxAuditQueryRunes = 4096

// auditInjection appends one journal line for an admitted injection. It is a
// no-op when AuditDir is unset (the common case: zero file I/O on the hot
// path) and degrades fail-open — a journal write error drops the entry and
// closes the journal rather than ever blocking or failing the agent loop.
func (b *Bridge) auditInjection(query string, budget int, degraded bool,
	targets []string, bytes int, text string) {
	if b == nil || b.cfg.AuditDir == "" || text == "" {
		return
	}
	targets = append([]string(nil), targets...)
	// Capture the label before taking auditMu: sessionLabel locks b.mu, and
	// Close locks b.mu then auditMu — taking auditMu first would invert that
	// order. Label assignment happens once per process, so this read is
	// stable for the lifetime of a run.
	session := b.sessionLabel()
	q := []rune(query)
	if len(q) > maxAuditQueryRunes {
		q = q[:maxAuditQueryRunes]
	}
	sum := sha256.Sum256([]byte(query))
	entry := auditEntry{
		Seq:       0, // assigned under auditMu below
		At:        time.Now().UTC().UnixMilli(),
		Session:   session,
		Query:     string(q),
		QueryHash: hex.EncodeToString(sum[:]),
		Degraded:  degraded,
		Budget:    budget,
		Bytes:     bytes,
		Targets:   targets,
		Text:      text,
	}
	b.auditMu.Lock()
	defer b.auditMu.Unlock()
	if b.auditF == nil {
		f, err := openAuditJournal(b.cfg.AuditDir)
		if err != nil {
			return
		}
		b.auditF = f
	}
	b.auditSeq++
	entry.Seq = b.auditSeq
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	if _, err := b.auditF.Write(append(data, '\n')); err != nil {
		_ = b.auditF.Close()
		b.auditF = nil
		return
	}
	_ = b.auditF.Sync()
}

// closeAudit closes the journal, if open, and returns its error. Callers hold
// auditMu.
func (b *Bridge) closeAudit() error {
	if b.auditF == nil {
		return nil
	}
	err := b.auditF.Close()
	b.auditF = nil
	return err
}

func (b *Bridge) sessionLabel() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.label
}

// openAuditJournal opens <AuditDir>/inject-audit.jsonl for append, creating
// the directory 0700 and refusing a pre-placed symlink (same tamper guard as
// the session sink).
func openAuditJournal(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "inject-audit.jsonl")
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrPermission}
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}
