package inject

import (
	"sort"
	"strings"

	"semantix/kernel/embed"
)

// Optional delete-only compression for injection content (Issue #509):
// repeated and redundant semantic units are removed across the admitted
// slices of one block, and every compressed slice is gated by a semantic
// equivalence check against its original — a slice whose compression moves
// too far from the original falls back to the original text (fail-open).

// Compression modes.
const (
	// CompressionDedup enables the deterministic unit-level deduplicator.
	CompressionDedup = "dedup"
)

// Gate and dedup defaults; overridable per Injector.
const (
	// DefaultMinSimilarity is the equivalence-gate floor. Below it a slice
	// keeps its original text (validated: meaning-preserving compressions
	// scored ≥0.94 at the 5th percentile while meaning-damaging removals
	// stayed ≤0.85 on a real slice library).
	DefaultMinSimilarity = 0.85
	// DefaultDedupThreshold is the unit near-duplicate Jaccard threshold,
	// aligned with `gc --consolidate-threshold`.
	DefaultDedupThreshold = 0.6
)

// compressVersion marks the compression pipeline revision. Any change to the
// algorithm must bump it: compressed bytes feed provider prefix caches, so
// two revisions must never produce the same block from different rules.
const compressVersion = "v1"

// CompressionOptions configures the optional compression stage. Nil on
// Injector means fully off: zero behavioral change, zero cost.
type CompressionOptions struct {
	// Mode selects the strategy. Only CompressionDedup exists today.
	Mode string
	// MinSimilarity is the equivalence-gate floor (default
	// DefaultMinSimilarity).
	MinSimilarity float64
	// DedupThreshold is the unit near-duplicate Jaccard threshold (default
	// DefaultDedupThreshold).
	DedupThreshold float64
	// Embedder scores the equivalence gate. Nil defaults to a deterministic
	// local hash embedder; any kernel/embed.Embedder works.
	Embedder embed.Embedder
}

// blockCompressor carries per-block state: units already retained by earlier
// admitted slices are the dedup reference for later ones.
type blockCompressor struct {
	minSim    float64
	threshold float64
	embed     embed.Embedder
	seen      []map[string]struct{}
}

func newBlockCompressor(opts *CompressionOptions) *blockCompressor {
	bc := &blockCompressor{
		minSim:    opts.MinSimilarity,
		threshold: opts.DedupThreshold,
		embed:     opts.Embedder,
	}
	if bc.minSim <= 0 {
		bc.minSim = DefaultMinSimilarity
	}
	if bc.threshold <= 0 {
		bc.threshold = DefaultDedupThreshold
	}
	if bc.embed == nil {
		bc.embed = embed.HashEmbedder{}
	}
	return bc
}

// compressSlice returns the replacement text for one admitted slice and
// whether compression was applied. The second result is false when there is
// nothing to compress or the equivalence gate rejected the candidate — the
// caller then injects the original text. Units retained here join the
// block-level reference set either way, so later slices dedupe against what
// is actually being injected.
func (bc *blockCompressor) compressSlice(content string) (string, bool) {
	units := splitUnits(content)
	if len(units) <= 1 {
		bc.retain(units)
		return "", false
	}
	kept := make([]int, 0, len(units))
	for i, u := range units {
		toks := unitTokens(u)
		if bc.dupOf(toks) >= 0 {
			continue
		}
		kept = append(kept, i)
	}
	if len(kept) == len(units) {
		bc.retain(units)
		return "", false
	}
	keptText := joinUnits(units, kept)
	if keptText == "" || keptText == content {
		bc.retain(units)
		return "", false
	}
	if !bc.equivalent(content, keptText) {
		// Gate rejected: keep the original verbatim, but its units still
		// enter the reference set — duplicates of it add nothing later.
		bc.retain(units)
		return "", false
	}
	bc.retainByIndices(units, kept)
	return keptText, true
}

// dupOf returns the index of the first retained unit that makes tok
// redundant (near-duplicate above threshold, or strict subset), or -1.
func (bc *blockCompressor) dupOf(tok map[string]struct{}) int {
	if len(tok) == 0 {
		return -1
	}
	for i, seen := range bc.seen {
		inter := 0
		subset := true
		for t := range tok {
			if _, ok := seen[t]; ok {
				inter++
			} else {
				subset = false
			}
		}
		if subset {
			return i
		}
		union := len(seen) + len(tok) - inter
		if union > 0 && float64(inter)/float64(union) >= bc.threshold {
			return i
		}
	}
	return -1
}

// equivalent reports whether the compressed text stayed semantically
// equivalent to the original per the embedding gate. Any embedder failure is
// fail-open: the caller keeps the original.
func (bc *blockCompressor) equivalent(orig, comp string) bool {
	vecs, err := bc.embed.Embed([]string{orig, comp})
	if err != nil || len(vecs) != 2 {
		return false
	}
	return embed.Cosine(vecs[0], vecs[1]) >= bc.minSim
}

func (bc *blockCompressor) retain(units []string) {
	for _, u := range units {
		bc.seen = append(bc.seen, unitTokens(u))
	}
}

func (bc *blockCompressor) retainByIndices(units []string, kept []int) {
	for _, i := range kept {
		bc.seen = append(bc.seen, unitTokens(units[i]))
	}
}

// splitUnits cuts content into semantic units: newline-separated lines and
// sentence-final punctuation boundaries. A period only ends a unit when
// followed by whitespace and not preceded by a digit (decimals, versions,
// IPs stay whole). Deterministic by construction.
func splitUnits(content string) []string {
	var units []string
	var cur strings.Builder
	runes := []rune(content)
	flush := func() {
		if u := strings.TrimSpace(cur.String()); u != "" {
			units = append(units, u)
		}
		cur.Reset()
	}
	for i, r := range runes {
		cur.WriteRune(r)
		switch {
		case r == '\n' || r == '。' || r == '！' || r == '？' || r == '；' || r == '!' || r == '?':
			flush()
		case r == '.':
			if i+1 < len(runes) && (runes[i+1] == ' ' || runes[i+1] == '\n') &&
				!(i > 0 && runes[i-1] >= '0' && runes[i-1] <= '9') {
				flush()
			}
		}
	}
	flush()
	return units
}

// unitTokens lowercases and splits a unit into a token set: CJK runes are
// individual tokens, ASCII alphanumeric runs are word tokens (mirrors the
// kernel/embed tokenizer conventions).
func unitTokens(unit string) map[string]struct{} {
	toks := make(map[string]struct{})
	var word strings.Builder
	for _, r := range strings.ToLower(unit) {
		switch {
		case r >= 0x4e00 && r <= 0x9fff:
			if word.Len() > 0 {
				toks[word.String()] = struct{}{}
				word.Reset()
			}
			toks[string(r)] = struct{}{}
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			word.WriteRune(r)
		default:
			if word.Len() > 0 {
				toks[word.String()] = struct{}{}
				word.Reset()
			}
		}
	}
	if word.Len() > 0 {
		toks[word.String()] = struct{}{}
	}
	return toks
}

// joinUnits reassembles kept units in original order, verbatim — the output
// is always a subsequence of the input's units, never a rewrite. Newline
// separators keep each retained unit byte-verbatim (sentence terminators are
// part of the unit; no punctuation is added).
func joinUnits(units []string, kept []int) string {
	idx := append([]int(nil), kept...)
	sort.Ints(idx)
	parts := make([]string, 0, len(idx))
	for _, i := range idx {
		parts = append(parts, units[i])
	}
	return strings.Join(parts, "\n")
}
