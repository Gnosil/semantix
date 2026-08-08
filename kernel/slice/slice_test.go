package slice

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

// U4 acceptance: extraction from a synthetic session (2 turns, tool calls)
// yields Prompt / ToolPattern / Result slices.
func TestExtractTurnSlices(t *testing.T) {
	transcript := []byte(
		`{"role":"user","content":"写一个 Go 冒烟测试模板"}
` +
			`{"role":"assistant","content":"","tool_calls":[{"id":"c1","name":"readFile"},{"id":"c2","name":"grep"}]}
` +
			`{"role":"tool","tool_call_id":"c1","content":"...result..."}
` +
			`{"role":"assistant","content":"模板已写好，放在 test/ 下。"}
` +
			`{"role":"user","content":"再写一个 Python 冒烟测试"}
` +
			`{"role":"assistant","content":"Python 模板如下：..."}`)
	got, err := NewExtractor().Extract(transcript, SliceMeta{ProjectSlug: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	var prompts, patterns, results int
	for _, s := range got {
		switch s.Type {
		case Prompt:
			prompts++
		case ToolPattern:
			patterns++
		case Result:
			results++
		}
		if s.Scope != Project || s.Weight != 1.0 {
			t.Fatalf("slice %s: scope/weight not set", s.ID)
		}
	}
	if prompts != 2 {
		t.Fatalf("expected 2 prompt slices, got %d", prompts)
	}
	if patterns != 1 {
		t.Fatalf("expected 1 tool-pattern slice, got %d", patterns)
	}
	if results != 1 {
		t.Fatalf("expected 1 result slice, got %d", results)
	}
}

// ToolPattern slices are space-joined so the BM25 splitter can tokenize them.
func TestExtractToolPatternSpaceJoined(t *testing.T) {
	transcript := []byte(`{"role":"user","content":"查一下"}` +
		`{"role":"assistant","content":"","tool_calls":[{"id":"c1","name":"readFile"},{"id":"c2","name":"editFile"}]}`)
	got, err := NewExtractor().Extract(transcript, SliceMeta{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range got {
		if s.Type == ToolPattern && string(s.Content) != "readFile editFile" {
			t.Fatalf("tool pattern content = %q, want space-joined", s.Content)
		}
	}
}

// Tolerant parsing: malformed lines are skipped without error.
func TestExtractTolerantBadLines(t *testing.T) {
	transcript := []byte("{not json\n{\"role\":\"user\",\"content\":\"hi\"}\n\n")
	got, err := NewExtractor().Extract(transcript, SliceMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Type != Prompt {
		t.Fatalf("expected 1 prompt slice, got %d", len(got))
	}
}

// Deterministic IDs: same content yields the same slice ID (dedup works).
func TestExtractDedup(t *testing.T) {
	transcript := []byte(`{"role":"user","content":"同一句话"}
{"role":"user","content":"同一句话"}`)
	got, err := NewExtractor().Extract(transcript, SliceMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 deduped slice, got %d", len(got))
	}
}

func TestFileStorePutGetList(t *testing.T) {
	st, err := NewFileStore(filepath.Join(t.TempDir(), "slices.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	a := &Slice{ID: "a", Type: Prompt, Scope: Project, Content: []byte("x")}
	b := &Slice{ID: "b", Type: Result, Scope: User, Content: []byte("y")}
	if err := st.Put(a); err != nil {
		t.Fatal(err)
	}
	if err := st.Put(b); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get("a")
	if err != nil || got == nil || got.ID != "a" {
		t.Fatalf("Get(a) = %v, %v", got, err)
	}
	if got, _ := st.Get("missing"); got != nil {
		t.Fatal("expected nil for missing id")
	}
	proj, err := st.List(Project)
	if err != nil || len(proj) != 1 || proj[0].ID != "a" {
		t.Fatalf("List(Project) = %v, %v", proj, err)
	}
	user, err := st.List(User)
	if err != nil || len(user) != 1 || user[0].ID != "b" {
		t.Fatalf("List(User) = %v, %v", user, err)
	}
}

// Put with the same ID replaces (no duplicates).
func TestFileStoreReplaceSameID(t *testing.T) {
	st, err := NewFileStore(filepath.Join(t.TempDir(), "slices.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Put(&Slice{ID: "a", Type: Prompt, Scope: Project, Content: []byte("v1")}); err != nil {
		t.Fatal(err)
	}
	if err := st.Put(&Slice{ID: "a", Type: Prompt, Scope: Project, Content: []byte("v2")}); err != nil {
		t.Fatal(err)
	}
	all, err := st.List(Project)
	if err != nil || len(all) != 1 {
		t.Fatalf("expected 1 slice after replace, got %d (%v)", len(all), err)
	}
	if string(all[0].Content) != "v2" {
		t.Fatalf("expected v2 content, got %q", all[0].Content)
	}
}

func TestFileStoreUpdateStats(t *testing.T) {
	st, err := NewFileStore(filepath.Join(t.TempDir(), "slices.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Put(&Slice{ID: "a", Type: Prompt, Scope: Project}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateStats("a", SliceStats{Hits: 3, Injected: 1}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get("a")
	if err != nil || got == nil {
		t.Fatalf("Get = %v, %v", got, err)
	}
	if got.Stats.Hits != 3 || got.Stats.Injected != 1 {
		t.Fatalf("stats mismatch: %+v", got.Stats)
	}
	if err := st.UpdateStats("nope", SliceStats{}); err == nil {
		t.Fatal("expected error for missing slice")
	}
}

// Concurrent Put/Get must be race-free and lossless (run with -race).
func TestFileStoreConcurrent(t *testing.T) {
	st, err := NewFileStore(filepath.Join(t.TempDir(), "slices.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	const goroutines = 10
	const perG = 20
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				id := fmt.Sprintf("g%d-%d", g, i)
				if err := st.Put(&Slice{ID: id, Type: Prompt, Scope: Project, Content: []byte(id)}); err != nil {
					t.Errorf("Put(%s): %v", id, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	all, err := st.List(Project)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != goroutines*perG {
		t.Fatalf("expected %d slices, got %d", goroutines*perG, len(all))
	}
}

// Oversized content is truncated to the cap, not dropped.
func TestExtractOversizeTruncated(t *testing.T) {
	long := make([]byte, maxPromptLen+100)
	for i := range long {
		long[i] = 'a'
	}
	transcript := []byte(`{"role":"user","content":"` + string(long) + `"}`)
	got, err := NewExtractor().Extract(transcript, SliceMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 slice, got %d", len(got))
	}
	if len(got[0].Content) != maxPromptLen {
		t.Fatalf("expected truncated content of %d bytes, got %d", maxPromptLen, len(got[0].Content))
	}
}

// Multi-byte runes crossing the truncation boundary must stay valid UTF-8.
func TestExtractOversizeTruncatedUTF8(t *testing.T) {
	long := "中" + strings.Repeat("文", maxPromptLen) // > maxPromptLen bytes
	transcript := []byte(`{"role":"user","content":"` + long + `"}`)
	got, err := NewExtractor().Extract(transcript, SliceMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 slice, got %d", len(got))
	}
	if !utf8.Valid(got[0].Content) {
		t.Fatalf("truncated content is invalid UTF-8: %q", got[0].Content)
	}
	if len(got[0].Content) > maxPromptLen {
		t.Fatalf("truncated content exceeds cap: %d > %d", len(got[0].Content), maxPromptLen)
	}
}

// Corrupt lines in the store file are skipped, not fatal.
func TestFileStoreCorruptLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "slices.jsonl")
	valid, err := json.Marshal(&Slice{ID: "ok", Type: Prompt, Scope: Project, Content: []byte("x")})
	if err != nil {
		t.Fatal(err)
	}
	content := append([]byte("{corrupt\n"), valid...)
	content = append(content, '\n')
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.Get("ok")
	if err != nil || got == nil {
		t.Fatalf("Get(ok) = %v, %v", got, err)
	}
}
