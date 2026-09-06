// Package inject implements the L2 semantic injection (U8, compose side):
// given a user turn and the slice store/index, it retrieves the most
// relevant previously-done slices and assembles a deterministic injection
// block for the harness compose step.
//
// Design invariants (see Agent-Infra-架构设计.md §4.2):
//   - canonical order: injection slices are score-descending with an ID
//     tie-break so relevance order and byte stability agree.
//   - budget: only whole slices are dropped; no candidate, including top-1,
//     may make the final block exceed the configured hard byte limit.
//   - low-authority: the block is wrapped in markers so the harness can place
//     it after the system prefix / before the user message, and strip it on
//     user edit/rollback (SliceReject).
package inject

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"semantix/kernel/bm25"
	"semantix/kernel/fingerprint"
	"semantix/kernel/sanitize"
	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

// escapeMarker neutralizes block markers inside slice content so a stored
// slice cannot break out of (or fake) the [semantix-reuse] block — the
// injection stays a low-authority, structurally-closed region. Matching is
// case-insensitive so `[/SEMANTIX-REUSE]` variants cannot bypass either
// (HIGH fix, security review 2026-08-09; MEDIUM hardening 2026-08-10).
func escapeMarker(s string) string {
	s = replaceFold(s, "[/semantix-reuse]", "[\\/semantix-reuse]")
	return replaceFold(s, "[semantix-reuse]", "[\\semantix-reuse]")
}

// replaceFold replaces all case-insensitive occurrences of old with new.
// Rune-level folding: unicode.ToLower maps one rune to one rune, so byte
// offsets never drift (a byte-level ToLower of the whole string would
// misalign for fold-special runes like İ/ẞ and could corrupt output).
func replaceFold(s, old, new string) string {
	rs := []rune(s)
	ro := []rune(strings.ToLower(old))
	var b strings.Builder
	i := 0
	for i <= len(rs)-len(ro) {
		match := true
		for j := range ro {
			if unicode.ToLower(rs[i+j]) != ro[j] {
				match = false
				break
			}
		}
		if match {
			b.WriteString(new)
			i += len(ro)
		} else {
			b.WriteRune(rs[i])
			i++
		}
	}
	for ; i < len(rs); i++ {
		b.WriteRune(rs[i])
	}
	return b.String()
}

// DefaultBudget is the default maximum injection block size in bytes.
const DefaultBudget = 4096

// Injector retrieves and assembles reuse slices for one user turn.
type Injector struct {
	Index slice.Index
	Store slice.Store // optional; used to re-read full slices when the index
	// returns them anyway — kept for symmetry with future lazy indexes.
	Scope slice.Scope
	K     int // top-k slices to consider (default 5)
	// Budget caps the assembled block size in bytes (default DefaultBudget).
	Budget int
	// MinScore drops slices below this BM25 score (0 disables).
	MinScore float64
	// AllowedTypes, when non-nil, is a fail-closed injection allowlist. Search
	// results outside it remain in Decisions for shadow analysis.
	AllowedTypes map[slice.SliceType]bool
	// RootDir and CurrentCommit enable strict source-freshness admission.
	// Callers that leave both empty retain the generic injector behavior.
	RootDir       string
	CurrentCommit string
	// LibrarySize and MinLibrarySize gate immature Project libraries.
	LibrarySize    int
	MinLibrarySize int
	// SourceSessionsByType counts distinct non-empty source sessions in the
	// library. MinSourceSessions prevents one session from self-confirming.
	SourceSessionsByType map[slice.SliceType]int
	MinSourceSessions    int
	// MinCoverage is the cleaned-query token coverage floor.
	MinCoverage float64
	// MinTopMargin is the absolute score gap between the two best candidates
	// that pass AllowedTypes. RequireRunnerUp rejects a singleton eligible set.
	MinTopMargin    float64
	RequireRunnerUp bool
	// Zones, when non-nil, applies the grey-zone classifier: only clearly
	// reusable slices (zone.Hit) enter the block; grey/miss candidates are
	// skipped (Krites §3.1 — the grey zone must be verified, not injected).
	Zones *zone.Zones
	// MinOrigin is the lowest provenance level admitted to the injection
	// block (Issue #279). The zero value (empty origin, level 1) admits
	// everything — embedding callers opt in explicitly; production paths
	// configure session-auto so import/legacy slices never inject.
	MinOrigin slice.Origin
	// AllowGrey switches the grey policy from drop (default, fail-closed)
	// to audit: grey-zone slices enter the block under a distinct
	// "(grey, unverified)" header — clearly separated from verified hits so
	// the model treats them as hints, not ground truth. Rationale: GW4
	// measured 8 of 10 repeated tasks landing in grey, so a hard drop
	// silently forfeits most of the reuse value; the audit mode keeps the
	// grey signal measurable (Injection.GreyIncluded) and injectable while
	// preserving the verified/grey boundary inside the block. Grey slices
	// share the exact-byte budget bound — audit mode is not a bypass.
	AllowGrey bool
	// TaskType, when non-empty, gates task-tagged Memory slices (the
	// distilled plan-skeleton / outcome cards, which carry a "task=<type>"
	// marker) on matching the current turn's classified type. Cross-type
	// instance locators are exactly the "misleading reference" failure the
	// two-arm pilot measured — a bugfix outcome card must not steer a
	// feature task. Untagged Memory slices and every other slice type are
	// unaffected; empty TaskType keeps the historical behavior.
	TaskType string
}

// Injection is the assembled, deterministic reuse block.
type Injection struct {
	Slices  []*slice.Slice // score-descending; ID tie-break
	Text    string         // marker-wrapped block to place in the compose step
	Bytes   int
	Dropped int // slices dropped by zone filter or budget (whole-slice truncation)
	// GreyIncluded counts grey-zone slices admitted under AllowGrey (audit
	// mode). Zero in the default drop mode; a persistent non-zero value is
	// the signal to recalibrate zone thresholds (W3 of the efficiency plan).
	GreyIncluded int
	// Decisions preserves the score-order admission trace for every retrieved
	// candidate. It is observation-only: replaying Admitted from Reason must
	// yield the same slice set that produced Text.
	Decisions []CandidateDecision
	// TopMargin is top1-top2 over type-eligible candidates. Zero means fewer
	// than two eligible candidates or equal scores.
	TopMargin float64
}

// CandidateDecision is the replayable admission outcome for one retrieved
// slice. Reason is a stable enum: admitted, below_min_score, zone_grey,
// zone_miss, sanitized_empty, origin_below_floor, budget, nil_slice,
// type_not_allowed, library_too_small, type_sources_too_few,
// runner_up_missing, top_margin_low, coverage_low, current_commit_unknown,
// commit_unknown, stale_commit, path_missing, dependency_path_invalid, or
// dependency_changed.
type CandidateDecision struct {
	ID       string
	Score    float64
	Coverage float64
	Zone     string
	Admitted bool
	Reason   string
}

const (
	blockOpen  = "[semantix-reuse]\n"
	blockClose = "[/semantix-reuse]"
)

// Build retrieves top-k slices for query and assembles the injection block.
// It never errors on empty results — an empty Injection is valid.
func (in *Injector) Build(query string) (*Injection, error) {
	k := in.K
	if k <= 0 {
		k = 5
	}
	hits, err := in.Index.Search(query, k, in.Scope)
	if err != nil {
		return nil, fmt.Errorf("inject: search: %w", err)
	}
	return in.BuildHits(query, hits)
}

// BuildHits applies the exact production admission and assembly path to an
// already-retrieved score-ordered hit list. It lets callers record the same
// candidates that produced the block without running retrieval twice.
func (in *Injector) BuildHits(query string, hits []slice.Hit) (*Injection, error) {
	budget := in.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}
	// Eligibility feeds the relative-confidence denominator (top1) and the
	// runner-up margin: a candidate the type allowlist or the task gate
	// rejects must not depress every other candidate's score/top1 ratio —
	// the exact denominator artifact the W6 ablation measured on cold
	// libraries (one lexically strong ineligible slice emptying the block).
	eligibleScores := make([]float64, 0, len(hits))
	freshnessReasons := make(map[*slice.Slice]string, len(hits))
	for _, h := range hits {
		if h.Slice != nil {
			freshnessReasons[h.Slice] = in.freshnessReason(h.Slice)
		}
		if h.Slice != nil && in.admissionTypeEligible(h.Slice) && in.taskAdmits(h.Slice) &&
			freshnessReasons[h.Slice] == "" {
			eligibleScores = append(eligibleScores, h.Score)
		}
	}
	top1 := 0.0
	topMargin := 0.0
	if len(eligibleScores) > 0 {
		top1 = eligibleScores[0]
	}
	if len(eligibleScores) > 1 {
		topMargin = eligibleScores[0] - eligibleScores[1]
	}

	var kept []*slice.Slice
	var dropped int
	// Single-pass assembly (Issue #283): candidates are filtered and
	// sized with the EXACT on-disk format before any bytes are written,
	// so the budget judgment and the final block share one口径 — the
	// block can never exceed Budget. (The old two-pass shape carried a
	// redundant pre-escape budget check from the #279 merge plus a dead
	// "(score=%.2f)" header that never reached the output.)
	type candidate struct {
		sl    *slice.Slice
		item  string // exact header + provenance + sanitized content bytes written
		grey  bool   // audit-mode admission under the unverified header
		score float64
	}
	var cands []candidate
	decisions := make([]CandidateDecision, 0, len(hits))
	size := len(blockOpen)
	for _, h := range hits {
		d := CandidateDecision{Score: h.Score, Zone: zone.Miss.String(), Reason: "nil_slice"}
		if h.Slice == nil {
			decisions = append(decisions, d)
			dropped++
			continue
		}
		d.ID = h.Slice.ID
		d.Coverage = bm25.QueryCoverage(query, string(h.Slice.Content))
		if !in.typeAllowed(h.Slice.Type) {
			d.Reason = "type_not_allowed"
			decisions = append(decisions, d)
			dropped++
			continue
		}
		if in.AllowedTypes != nil && h.Slice.Type == slice.Result && h.Slice.Meta.EffectiveResultStatus() != slice.ResultStatusVerified {
			d.Reason = "result_probation"
			decisions = append(decisions, d)
			dropped++
			continue
		}
		if reason := freshnessReasons[h.Slice]; reason != "" {
			d.Reason = reason
			decisions = append(decisions, d)
			dropped++
			continue
		}
		if in.MinLibrarySize > 0 && in.LibrarySize < in.MinLibrarySize {
			d.Reason = "library_too_small"
			decisions = append(decisions, d)
			dropped++
			continue
		}
		if in.MinSourceSessions > 0 && in.SourceSessionsByType[h.Slice.Type] < in.MinSourceSessions {
			d.Reason = "type_sources_too_few"
			decisions = append(decisions, d)
			dropped++
			continue
		}
		if in.RequireRunnerUp && len(eligibleScores) < 2 {
			d.Reason = "runner_up_missing"
			decisions = append(decisions, d)
			dropped++
			continue
		}
		if in.MinTopMargin > 0 && topMargin < in.MinTopMargin {
			d.Reason = "top_margin_low"
			decisions = append(decisions, d)
			dropped++
			continue
		}
		if in.MinScore > 0 && h.Score < in.MinScore {
			d.Reason = "below_min_score"
			decisions = append(decisions, d)
			dropped++
			continue
		}
		if in.MinCoverage > 0 && d.Coverage < in.MinCoverage {
			d.Reason = "coverage_low"
			decisions = append(decisions, d)
			dropped++
			continue
		}
		// Task-type admission for tagged Memory cards (see the TaskType
		// field doc): a card distilled from a different task type is
		// dropped before zone classification even sees it.
		if !in.taskAdmits(h.Slice) {
			d.Reason = "task_type_mismatch"
			decisions = append(decisions, d)
			dropped++
			continue
		}
		isGrey := false
		if in.Zones != nil {
			z := *in.Zones
			z = z.ForType(h.Slice.Type.String()) // Issue #259 阶段 2
			classified := z.Classify(h.Score, top1)
			d.Zone = classified.String()
			switch classified {
			case zone.Hit:
				// verified: admitted below
			case zone.Grey:
				if !in.AllowGrey {
					d.Reason = "zone_grey"
					decisions = append(decisions, d)
					dropped++
					continue
				}
				isGrey = true // admitted below under the unverified header
			default: // miss
				d.Reason = "zone_miss"
				decisions = append(decisions, d)
				dropped++
				continue
			}
		} else {
			d.Zone = "unclassified"
		}
		// Inject-side sanitization (Issue #278, Security §3.1): the block
		// carries the deterministically cleaned content — escape stripping,
		// injection-feature removal, sensitive redaction — then the block
		// markers are escaped. Idempotent for write-side-sanitized slices
		// (zero change → L1 prefix cache unaffected); legacy unsanitized
		// slices are backstopped here. A payload that sanitizes to empty is
		// dropped entirely (nothing useful to inject).
		content := sanitize.Sanitize(string(h.Slice.Content))
		if content == "" {
			d.Reason = "sanitized_empty"
			decisions = append(decisions, d)
			dropped++
			continue
		}
		// Issue #279: low-integrity origins never enter the injection
		// block — a trusted floor above the candidate's level excludes it
		// (import and legacy are level 1; session-auto/prefetch 2;
		// user-curated 3).
		if h.Slice != nil && in.MinOrigin.Level() > h.Slice.Meta.Origin.Level() {
			d.Reason = "origin_below_floor"
			decisions = append(decisions, d)
			dropped++
			continue
		}
		content = escapeMarker(content)
		// Budget is judged on the exact bytes that will be written, including
		// provenance, escaped content, and the grey audit variant.
		item := formatSliceItem(h.Slice, h.Score, content, isGrey)
		if size+len(item)+len(blockClose) > budget {
			d.Reason = "budget"
			decisions = append(decisions, d)
			dropped++
			continue
		}
		size += len(item)
		d.Admitted = true
		d.Reason = "admitted"
		decisions = append(decisions, d)
		cands = append(cands, candidate{sl: h.Slice, item: item, grey: isGrey, score: h.Score})
	}

	if len(cands) == 0 {
		return &Injection{Dropped: dropped, Decisions: decisions}, nil
	}

	// Canonical order: verified candidates first, then audit-mode grey;
	// relevance is score-descending and equal scores use ID as the stable
	// tie-break. This preserves prefix determinism without hiding the best
	// evidence behind an arbitrary identifier.
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].grey != cands[j].grey {
			return !cands[i].grey
		}
		if cands[i].score != cands[j].score {
			return cands[i].score > cands[j].score
		}
		return cands[i].sl.ID < cands[j].sl.ID
	})

	var buf bytes.Buffer
	buf.Grow(size + len(blockClose))
	buf.WriteString(blockOpen)
	greyIncluded := 0
	for _, c := range cands {
		if c.grey {
			greyIncluded++
		}
		buf.WriteString(c.item)
		kept = append(kept, c.sl)
	}
	buf.WriteString(blockClose)

	return &Injection{
		Slices:       kept,
		Text:         buf.String(),
		Bytes:        buf.Len(),
		Dropped:      dropped,
		GreyIncluded: greyIncluded,
		Decisions:    decisions,
		TopMargin:    topMargin,
	}, nil
}

// taskAdmits applies the task-type gate: with a non-empty TaskType, a
// Memory slice carrying a different task tag is inadmissible (both for the
// block and for the relative-confidence denominator).
func (in *Injector) taskAdmits(sl *slice.Slice) bool {
	if in.TaskType == "" || sl.Type != slice.Memory {
		return true
	}
	tag := taskTagOf(string(sl.Content))
	return tag == "" || tag == in.TaskType
}

// taskTagOf extracts the "task=<type>" marker a distilled Memory card
// carries in its head line ("Plan skeleton (task=bugfix):", "Task outcome
// (task=bugfix): …"). Empty for untagged content — legacy Memory slices
// carry no tag and are never task-gated.
func taskTagOf(content string) string {
	head := content
	if i := strings.IndexByte(head, '\n'); i >= 0 {
		head = head[:i]
	}
	_, rest, ok := strings.Cut(head, "task=")
	if !ok {
		return ""
	}
	end := strings.IndexFunc(rest, func(r rune) bool {
		return !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z')
	})
	if end < 0 {
		return rest
	}
	return rest[:end]
}

func formatSliceItem(sl *slice.Slice, score float64, content string, grey bool) string {
	header := fmt.Sprintf("--- slice %s ---\n", sl.ID)
	if grey {
		header = fmt.Sprintf("--- slice %s (grey, unverified) ---\n", sl.ID)
	}
	verified := "unknown"
	if sl.Type == slice.Result {
		verified = string(sl.Meta.EffectiveResultStatus())
	}
	provenance := fmt.Sprintf(
		"type=%s project=%q source=%q commit=%q origin=%s verified=%s score=%.4f created_at=%d\n",
		sl.Type.String(), sl.Meta.ProjectSlug, sl.Meta.SourceSession, sl.Meta.BaseCommit, sl.Meta.Origin, verified, score, sl.CreatedAt,
	)
	return header + provenance + content + "\n"
}

func (in *Injector) typeAllowed(t slice.SliceType) bool {
	return in.AllowedTypes == nil || in.AllowedTypes[t]
}

func (in *Injector) admissionTypeEligible(sl *slice.Slice) bool {
	if sl == nil || !in.typeAllowed(sl.Type) {
		return false
	}
	return in.AllowedTypes == nil || sl.Type != slice.Result || sl.Meta.EffectiveResultStatus() == slice.ResultStatusVerified
}

func (in *Injector) freshnessReason(sl *slice.Slice) string {
	if sl == nil || (in.RootDir == "" && in.CurrentCommit == "") {
		return ""
	}
	if in.CurrentCommit == "" {
		return "current_commit_unknown"
	}
	if sl.Meta.BaseCommit == "" {
		return "commit_unknown"
	}
	if sl.Meta.BaseCommit != in.CurrentCommit && len(sl.Meta.Deps) == 0 {
		return "stale_commit"
	}
	if len(sl.Meta.Deps) == 0 {
		return ""
	}

	paths := make([]string, 0, len(sl.Meta.Deps))
	for path := range sl.Meta.Deps {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	root, err := filepath.Abs(in.RootDir)
	if err != nil {
		return "dependency_path_invalid"
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "dependency_path_invalid"
	}
	for _, path := range paths {
		if !filepath.IsLocal(path) {
			return "dependency_path_invalid"
		}
		parts := strings.Split(filepath.Clean(filepath.FromSlash(path)), string(filepath.Separator))
		current := root
		for i, part := range parts {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if os.IsNotExist(err) {
				return "path_missing"
			}
			if err != nil || info.Mode()&os.ModeSymlink != 0 {
				return "dependency_path_invalid"
			}
			if i < len(parts)-1 && !info.IsDir() {
				return "dependency_path_invalid"
			}
			if i == len(parts)-1 && !info.Mode().IsRegular() {
				return "dependency_path_invalid"
			}
		}
	}
	changed, err := fingerprint.Verify(root, sl.Meta.Deps)
	if err != nil || len(changed) > 0 {
		return "dependency_changed"
	}
	return ""
}
