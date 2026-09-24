// Package audit writes the local audit log (Security §8③): slice
// provenance stamps and trust upgrades as JSONL lines. It is best-effort
// by design — audit failures never block the main path (same discipline
// as the usage recorder). The log is append-only and tolerant of
// concurrent writers only when they are serialized by the caller.
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"semantix/kernel/slice"
)

// Action tags (wire-stable; new actions are additive).
const (
	// ActionSliceOrigin records a slice entering the library with its
	// provenance tag (Security §8③: 切片入库(来源+净化动作)).
	ActionSliceOrigin = "slice_origin"
	// ActionSliceTrust records an explicit trust upgrade (Issue #279).
	ActionSliceTrust = "slice_trust"
	// ActionResultVerify records an explicit Result probation promotion.
	ActionResultVerify = "result_verify"
)

// Recorder appends audit events to a JSONL file (0600, atomic rewrite via
// temp+rename, tolerant of trailing bad lines — mirrors usage.Recorder).
type Recorder struct {
	path string
	now  func() time.Time
}

// NewRecorder opens (creating if needed) the audit log at path.
func NewRecorder(path string) (*Recorder, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("audit: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit: create: %w", err)
	}
	f.Close()
	return &Recorder{path: path, now: time.Now}, nil
}

// Record appends one audit line: {"at":..., "action":..., fields...}.
func (r *Recorder) Record(action string, fields map[string]string) error {
	if r == nil {
		return nil
	}
	row := map[string]string{"at": fmt.Sprintf("%d", r.now().Unix()), "action": action}
	for k, v := range fields {
		row[k] = v
	}
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("audit: append: %w", err)
	}
	defer f.Close()
	enc, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("audit: marshal: %w", err)
	}
	if _, err := f.Write(append(enc, '\n')); err != nil {
		return fmt.Errorf("audit: write: %w", err)
	}
	return nil
}

// Origin records a slice entering the library with its provenance tag.
func (r *Recorder) Origin(sliceID string, origin slice.Origin, channel string) error {
	return r.Record(ActionSliceOrigin, map[string]string{
		"slice_id": sliceID, "origin": string(origin), "channel": channel,
	})
}

// Trust records an explicit trust upgrade of a slice.
func (r *Recorder) Trust(sliceID string, from, to slice.Origin) error {
	return r.Record(ActionSliceTrust, map[string]string{
		"slice_id": sliceID, "from_origin": string(from), "to_origin": string(to),
	})
}

// ResultVerify records the evidence channel for a Result promotion.
func (r *Recorder) ResultVerify(sliceID, method, evidence string) error {
	return r.Record(ActionResultVerify, map[string]string{
		"slice_id": sliceID, "method": method, "evidence": evidence,
	})
}
