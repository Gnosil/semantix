// Package inject implements the L2 semantic injection (U8, compose side):
// given a user turn and the slice store/index, it retrieves the most
// relevant previously-done slices and assembles a deterministic injection
// block for the harness compose step.
//
// Design invariants (see Agent-Infra-架构设计.md §4.2):
//   - canonical order: injection slices are sorted by ID so identical
//     retrievals produce byte-identical blocks (DeepSeek prefix-cache
//     friendly); never sorted by score.
//   - budget: only whole slices are dropped, in score order, until the block
//     fits the budget; the top slice is always kept when k >= 1.
//   - low-authority: the block is wrapped in markers so the harness can place
//     it after the system prefix / before the user message, and strip it on
//     user edit/rollback (SliceReject).
package inject

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"unicode"

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
	Slices  []*slice.Slice // canonical (ID-sorted) order
	Text    string         // marker-wrapped block to place in the compose step
	Bytes   int
	Dropped int // slices dropped by zone filter or budget (whole-slice truncation)
	// GreyIncluded counts grey-zone slices admitted under AllowGrey (audit
	// mode). Zero in the default drop mode; a persistent non-zero value is
	// the signal to recalibrate zone thresholds (W3 of the efficiency plan).
	GreyIncluded int
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
	budget := in.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}

	hits, err := in.Index.Search(query, k, in.Scope)
	if err != nil {
		return nil, fmt.Errorf("inject: search: %w", err)
	}
	// The relative-confidence denominator is the best ELIGIBLE candidate,
	// not the raw best hit: a candidate whose type can never inject (per-type
	// zone override) or that the task gate rejects must not depress every
	// other candidate's score/top1 ratio. Otherwise one lexically strong
	// tool_pattern slice empties the whole block — the exact denominator
	// artifact the W6 ablation measured on cold libraries.
	top1 := 0.0
	for _, h := range hits {
		if h.Slice == nil || !in.taskAdmits(h.Slice) {
			continue
		}
		if in.Zones != nil {
			z := in.Zones.ForType(h.Slice.Type.String())
			// Miss even as its own denominator → ineligible at any rank.
			if z.Classify(h.Score, h.Score) == zone.Miss {
				continue
			}
		}
		if h.Score > top1 {
			top1 = h.Score
		}
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
		sl      *slice.Slice
		content string // sanitized + marker-escaped, == the bytes written
		grey    bool   // audit-mode admission under the unverified header
	}
	var cands []candidate
	size := len(blockOpen)
	for _, h := range hits {
		if in.MinScore > 0 && h.Score < in.MinScore {
			continue
		}
		// Task-type admission for tagged Memory cards (see the TaskType
		// field doc): a card distilled from a different task type is
		// dropped before zone classification even sees it.
		if h.Slice != nil && !in.taskAdmits(h.Slice) {
			dropped++
			continue
		}
		isGrey := false
		if in.Zones != nil {
			z := *in.Zones
			if h.Slice != nil {
				z = z.ForType(h.Slice.Type.String()) // Issue #259 阶段 2
			}
			switch z.Classify(h.Score, top1) {
			case zone.Hit:
				// verified: admitted below
			case zone.Grey:
				if !in.AllowGrey {
					dropped++
					continue
				}
				isGrey = true // admitted below under the unverified header
			default: // miss
				dropped++
				continue
			}
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
			dropped++
			continue
		}
		// Issue #279: low-integrity origins never enter the injection
		// block — a trusted floor above the candidate's level excludes it
		// (import and legacy are level 1; session-auto/prefetch 2;
		// user-curated 3).
		if h.Slice != nil && in.MinOrigin.Level() > h.Slice.Meta.Origin.Level() {
			dropped++
			continue
		}
		content = escapeMarker(content)
		// Budget judged on the EXACT bytes that will be written (escaped
		// content, canonical header — including the grey audit variant) —
		// never on a pre-escape length.
		header := "--- slice %s ---\n%s\n"
		if isGrey {
			header = "--- slice %s (grey, unverified) ---\n%s\n"
		}
		item := len(fmt.Sprintf(header, h.Slice.ID, content))
		if size+item+len(blockClose)+64 > budget && len(cands) > 0 {
			dropped++
			continue
		}
		size += item
		cands = append(cands, candidate{sl: h.Slice, content: content, grey: isGrey})
	}

	// Canonical order: verified candidates first, then audit-mode grey,
	// each ID-sorted for byte-stable output (never score order).
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].grey != cands[j].grey {
			return !cands[i].grey
		}
		return cands[i].sl.ID < cands[j].sl.ID
	})

	var buf bytes.Buffer
	buf.Grow(size + len(blockClose))
	buf.WriteString(blockOpen)
	greyIncluded := 0
	for _, c := range cands {
		header := "--- slice %s ---\n%s\n"
		if c.grey {
			header = "--- slice %s (grey, unverified) ---\n%s\n"
			greyIncluded++
		}
		fmt.Fprintf(&buf, header, c.sl.ID, c.content)
		kept = append(kept, c.sl)
	}
	buf.WriteString(blockClose)

	return &Injection{
		Slices:       kept,
		Text:         buf.String(),
		Bytes:        buf.Len(),
		Dropped:      dropped,
		GreyIncluded: greyIncluded,
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
