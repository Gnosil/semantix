package slice

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Extractor turns session transcripts into slices.
type Extractor interface {
	// Extract processes one session transcript (JSONL lines) and returns new
	// or updated slices ready for store.Put.
	Extract(sessionJSONL []byte, meta SliceMeta) ([]*Slice, error)
}

// transcriptLine is one line of a session JSONL transcript (Reasonix-style).
// Only the fields the extractor needs are decoded; unknown fields are ignored.
type transcriptLine struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolCalls []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"tool_calls,omitempty"`
}

// Limits for extracted slice content (kept modest for the MVP).
const (
	maxPromptLen = 4000 // bytes of a Prompt slice
	maxResultLen = 8000 // bytes of a Result slice
	minToolSeq   = 2    // minimum tool calls for a ToolPattern slice
)

type extractor struct{}

// NewExtractor returns the default extractor: turn-boundary segmentation,
// tool-sequence (T-Slice) extraction and final-result capture.
func NewExtractor() Extractor { return extractor{} }

// Extract parses one session JSONL transcript into slices:
//   - Prompt slice: first user message of each turn (bounded)
//   - ToolPattern slice: tool-name sequence of each turn (>= 2 calls)
//   - Result slice: final assistant answer of the session (bounded)
//
// Malformed lines are skipped (tolerant). Duplicate content (same ID) is
// merged. Slice IDs are content hashes (deterministic, dedup-friendly); a
// stable UUID strategy can replace this in M1 (see Slice.ID doc).
func (extractor) Extract(sessionJSONL []byte, meta SliceMeta) ([]*Slice, error) {
	lines, perr := parseTranscript(sessionJSONL)
	var out []*Slice
	var turn []transcriptLine
	flushTurn := func() {
		if s := promptSlice(turn, meta); s != nil {
			out = append(out, s)
		}
		if s := toolPatternSlice(turn, meta); s != nil {
			out = append(out, s)
		}
		turn = nil
	}
	for _, l := range lines {
		if l.Role == "user" && len(turn) > 0 {
			flushTurn()
		}
		turn = append(turn, l)
	}
	if len(turn) > 0 {
		flushTurn()
	}
	if s := finalResultSlice(lines, meta); s != nil {
		out = append(out, s)
	}
	// Partial results are still returned; perr reports a truncated scan
	// (oversized line) so callers can decide whether to retry/scale up.
	return dedup(out), perr
}

func parseTranscript(data []byte) ([]transcriptLine, error) {
	var out []transcriptLine
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var tl transcriptLine
		if err := json.Unmarshal(line, &tl); err != nil {
			continue // tolerant: skip malformed lines
		}
		out = append(out, tl)
	}
	return out, sc.Err()
}

func promptSlice(turn []transcriptLine, meta SliceMeta) *Slice {
	for _, l := range turn {
		if l.Role == "user" {
			content := strings.TrimSpace(l.Content)
			if content == "" {
				return nil
			}
			return newSlice(Prompt, Project, truncate([]byte(content), maxPromptLen), meta)
		}
	}
	return nil
}

func toolPatternSlice(turn []transcriptLine, meta SliceMeta) *Slice {
	var names []string
	for _, l := range turn {
		for _, tc := range l.ToolCalls {
			if tc.Name != "" {
				names = append(names, tc.Name)
			}
		}
	}
	if len(names) < minToolSeq {
		return nil
	}
	// Space-joined (not \x1f): keeps the sequence tokenizable by the BM25
	// whitespace/CJK splitter (kernel/bm25). Tool-name tokens are plain words.
	return newSlice(ToolPattern, Project, []byte(strings.Join(names, " ")), meta)
}

func finalResultSlice(lines []transcriptLine, meta SliceMeta) *Slice {
	for i := len(lines) - 1; i >= 0; i-- {
		l := lines[i]
		if l.Role == "assistant" && len(l.ToolCalls) == 0 {
			content := strings.TrimSpace(l.Content)
			if content == "" {
				return nil
			}
			return newSlice(Result, Project, truncate([]byte(content), maxResultLen), meta)
		}
	}
	return nil
}

func truncate(b []byte, max int) []byte {
	if len(b) <= max {
		return b
	}
	b = b[:max]
	// Avoid cutting mid-rune: back off to the last valid UTF-8 boundary.
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return b
}

func newSlice(t SliceType, sc Scope, content []byte, meta SliceMeta) *Slice {
	return &Slice{
		ID:      sliceID(content, t, sc),
		Type:    t,
		Scope:   sc,
		Content: content,
		Weight:  1.0,
		Meta:    meta,
	}
}

// sliceID is a deterministic content-derived ID. Type and scope are mixed in
// so identical content of different kinds cannot collide.
func sliceID(content []byte, t SliceType, sc Scope) string {
	h := sha256.New()
	h.Write([]byte{byte(t), byte(sc)})
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil)[:8])
}

func dedup(slices []*Slice) []*Slice {
	seen := make(map[string]bool, len(slices))
	var out []*Slice
	for _, s := range slices {
		if !seen[s.ID] {
			seen[s.ID] = true
			out = append(out, s)
		}
	}
	return out
}
