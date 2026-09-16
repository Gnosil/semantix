package slice

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"semantix/kernel/sanitize"
)

// PruneOptions describes conservative, explicit library maintenance. Apply is
// opt-in; zero day windows select the documented defaults. Now is injectable.
type PruneOptions struct {
	Scope         Scope
	Apply         bool
	OlderThanDays int
	RecentDays    int
	Project       string
	ProjectRoot   string
	Now           int64
}

func (o PruneOptions) normalized() (PruneOptions, error) {
	if o.Scope != Project && o.Scope != User {
		return o, fmt.Errorf("prune: scope must be project or user")
	}
	if o.OlderThanDays == 0 {
		o.OlderThanDays = 90
	}
	if o.RecentDays == 0 {
		o.RecentDays = 30
	}
	if o.OlderThanDays < 1 || o.OlderThanDays > 3650000 || o.RecentDays < 1 || o.RecentDays > 3650000 {
		return o, fmt.Errorf("prune: day windows must be between 1 and 3650000")
	}
	if o.ProjectRoot != "" && o.Project == "" {
		return o, fmt.Errorf("prune: project-root requires an explicit project slug")
	}
	if o.Now == 0 {
		o.Now = time.Now().Unix()
	}
	if o.Now < 0 {
		return o, fmt.Errorf("prune: invalid clock")
	}
	return o, nil
}

// PruneCandidate contains metadata only; content and verification evidence
// must never enter the public maintenance report.
type PruneCandidate struct {
	ID                        string   `json:"id"`
	Type                      string   `json:"type"`
	Scope                     string   `json:"scope"`
	Reasons                   []string `json:"reasons"`
	SourceSession             string   `json:"source_session"`
	CreatedAt                 int64    `json:"created_at"`
	LastUsed                  int64    `json:"last_used"`
	EstimatedReclaimableBytes int64    `json:"estimated_reclaimable_bytes"`
	RetainedID                string   `json:"retained_id,omitempty"`
}

type PruneSkippedCheck struct {
	Check  string `json:"check"`
	Reason string `json:"reason"`
}

type PruneResult struct {
	Scope                     string              `json:"scope"`
	DryRun                    bool                `json:"dry_run"`
	Checked                   int                 `json:"checked"`
	Kept                      int                 `json:"kept"`
	Removed                   int                 `json:"removed"`
	Candidates                []PruneCandidate    `json:"candidates"`
	SkippedChecks             []PruneSkippedCheck `json:"skipped_checks"`
	EstimatedReclaimableBytes int64               `json:"estimated_reclaimable_bytes"`
	SpaceEstimate             string              `json:"space_estimate"`
	ArchivePath               string              `json:"archive_path,omitempty"`
}

func emptyPruneResult(o PruneOptions) PruneResult {
	r := PruneResult{Scope: o.Scope.String(), DryRun: !o.Apply, Candidates: []PruneCandidate{}, SkippedChecks: []PruneSkippedCheck{},
		SpaceEstimate: "logical live-record bytes; journal/base compaction is separate and archives consume disk space"}
	if o.ProjectRoot == "" {
		r.SkippedChecks = append(r.SkippedChecks, PruneSkippedCheck{"missing_dependency", "project_mapping_unavailable"})
	}
	return r
}

func pruneProtected(s *Slice, o PruneOptions) bool {
	cutoff := o.Now - int64(o.RecentDays)*86400
	return s.Type.String() == "unknown" || s.CreatedAt <= 0 || s.CreatedAt >= cutoff || s.Stats.LastUsed < 0 ||
		s.Stats.LastUsed >= cutoff || ((s.Stats.Hits > 0 || s.Stats.Injected > 0) && s.Stats.LastUsed == 0) ||
		s.Meta.Origin == OriginUserCurated || (s.Meta.Origin != "" && !s.Meta.Origin.Valid()) ||
		s.Stats.UserFeedback > 0 || math.IsNaN(s.Stats.UserFeedback) || math.IsInf(s.Stats.UserFeedback, 0) || s.Stats.Useful > 0 ||
		s.Meta.ResultStatus == ResultStatusVerified || (s.Meta.ResultStatus != "" && s.Meta.ResultStatus != ResultStatusProbation)
}

// duplicateKey uses existing types and byte equality, not another content
// hash. Provenance affecting reuse must agree; a session label and extraction
// compression sizes do not change the applicability of identical content.
func duplicateKey(s *Slice) string {
	m := s.Meta
	m.SourceSession = ""
	m.OriginalBytes, m.StoredBytes = 0, 0
	b, _ := json.Marshal(struct {
		Type    SliceType
		Scope   Scope
		Content []byte
		Meta    SliceMeta
	}{s.Type, s.Scope, s.Content, m})
	return string(b)
}

func planPrune(entries map[string]*storedEntry, o PruneOptions) (PruneResult, error) {
	r := emptyPruneResult(o)
	var root *os.Root
	if o.ProjectRoot != "" {
		abs, err := filepath.Abs(o.ProjectRoot)
		if err != nil {
			return r, err
		}
		// Reject symlinked roots as well as symlinked dependency components.
		if resolved, err := filepath.EvalSymlinks(abs); err != nil || resolved != abs {
			return r, fmt.Errorf("prune: project root must be an existing directory without symlinks")
		}
		root, err = os.OpenRoot(abs)
		if err != nil {
			return r, fmt.Errorf("prune: cannot open project root")
		}
		defer root.Close()
	}
	var all []*Slice
	for _, e := range entries {
		if e.s.Scope == o.Scope && (o.Project == "" || e.s.Meta.ProjectSlug == o.Project) {
			all = append(all, &e.s)
		}
	}
	r.Checked = len(all)
	protected := map[string]bool{}
	reasons := map[string][]string{}
	groups := map[string][]*Slice{}
	uncertainPaths := false
	cutoff := o.Now - int64(o.OlderThanDays)*86400
	for _, s := range all {
		protected[s.ID] = pruneProtected(s, o)
		if !protected[s.ID] && root != nil && len(s.Meta.Deps) > 0 {
			missing, uncertain := missingPruneDependency(root, s)
			if uncertain {
				protected[s.ID] = true
				uncertainPaths = true
			} else if missing {
				reasons[s.ID] = append(reasons[s.ID], "missing_dependency")
			}
		}
		if !protected[s.ID] && s.CreatedAt < cutoff && s.Stats.LastUsed < cutoff {
			reasons[s.ID] = append(reasons[s.ID], "stale_unused")
			if s.Type == Result && s.Meta.ResultStatus == ResultStatusProbation {
				reasons[s.ID] = append(reasons[s.ID], "stale_probation")
			}
			if s.Stats.Rejected > 0 || s.Stats.UserFeedback < 0 {
				reasons[s.ID] = append(reasons[s.ID], "stale_rejected")
			}
		}
		if len(s.Content) > 0 {
			key := duplicateKey(s)
			groups[key] = append(groups[key], s)
		}
	}
	if uncertainPaths {
		r.SkippedChecks = append(r.SkippedChecks, PruneSkippedCheck{"missing_dependency", "ambiguous_dependencies_retained"})
	}
	retained := map[string]string{}
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		sort.Slice(group, func(i, j int) bool {
			a, b := group[i], group[j]
			if protected[a.ID] != protected[b.ID] {
				return protected[a.ID]
			}
			if a.Stats.LastUsed != b.Stats.LastUsed {
				return a.Stats.LastUsed > b.Stats.LastUsed
			}
			if a.CreatedAt != b.CreatedAt {
				return a.CreatedAt > b.CreatedAt
			}
			return a.ID < b.ID
		})
		// Only choose a representative that independently survives. If every
		// member is stale, remove them for staleness with no dangling relation.
		var rep *Slice
		for _, s := range group {
			if protected[s.ID] || len(reasons[s.ID]) == 0 {
				rep = s
				break
			}
		}
		if rep == nil {
			continue
		}
		for _, s := range group {
			if s.ID == rep.ID || protected[s.ID] {
				continue
			}
			retained[s.ID] = rep.ID
			reasons[s.ID] = append([]string{"exact_duplicate"}, reasons[s.ID]...)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })
	for _, s := range all {
		if protected[s.ID] || len(reasons[s.ID]) == 0 {
			continue
		}
		b, err := json.Marshal(dtoFromEntry(entries[s.ID]))
		if err != nil {
			return r, fmt.Errorf("prune: cannot serialize candidate")
		}
		source := sanitize.Sanitize(s.Meta.SourceSession)
		if source == "" {
			source = "unknown"
		}
		c := PruneCandidate{ID: s.ID, Type: s.Type.String(), Scope: s.Scope.String(), Reasons: reasons[s.ID], SourceSession: source,
			CreatedAt: s.CreatedAt, LastUsed: s.Stats.LastUsed, EstimatedReclaimableBytes: int64(len(b) + 1), RetainedID: retained[s.ID]}
		r.Candidates = append(r.Candidates, c)
		r.EstimatedReclaimableBytes += c.EstimatedReclaimableBytes
	}
	r.Kept = r.Checked - len(r.Candidates)
	return r, nil
}

// A failed stat is not automatically evidence of deletion. An os.Root confines
// every lookup even if components are replaced concurrently; Lstat additionally
// rejects symlinks at every existing level. Any ambiguous dependency retains
// the entire slice (even when another dependency is missing).
func missingPruneDependency(root *os.Root, s *Slice) (missing, uncertain bool) {
	for path := range s.Meta.Deps {
		// A Windows path in an imported Unix snapshot is not a missing Unix
		// file. Ambiguous foreign path syntax must never justify deletion.
		if !filepath.IsLocal(path) || strings.Contains(path, ":") || (filepath.Separator != '\\' && strings.Contains(path, "\\")) {
			return false, true
		}
		parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
		prefix := ""
		for i, part := range parts {
			prefix = filepath.Join(prefix, part)
			fi, err := root.Lstat(prefix)
			if os.IsNotExist(err) {
				missing = true
				break
			}
			if err != nil || fi.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !fi.IsDir()) {
				return false, true
			}
		}
	}
	return missing, false
}
