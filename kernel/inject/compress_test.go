package inject

import (
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
)

func newBenchIndex() *bm25.Index { return bm25.New() }

// compressSeed builds a store+index with the given contents and runs one
// BuildHits with the compression options (nil = off), returning the result.
func compressSeed(t *testing.T, contents []string, opts *CompressionOptions) *Injection {
	t.Helper()
	store, err := slice.NewFileStore(t.TempDir() + "/slices.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	idx := newBenchIndex()
	for i, c := range contents {
		s := &slice.Slice{
			ID:      "c-" + string(rune('a'+i)),
			Type:    slice.Context,
			Scope:   slice.Project,
			Content: []byte(c),
		}
		if err := store.Put(s); err != nil {
			t.Fatal(err)
		}
		if err := idx.Insert(s); err != nil {
			t.Fatal(err)
		}
	}
	inj := &Injector{
		Store:        store,
		Index:        idx,
		Scope:        slice.Project,
		K:            5,
		Budget:       8192,
		AllowedTypes: map[slice.SliceType]bool{slice.Context: true},
		Compress:     opts,
	}
	hits, err := idx.Search(sharedQuery, 5, slice.Project)
	if err != nil {
		t.Fatal(err)
	}
	res, err := inj.BuildHits(sharedQuery, hits)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

const sharedQuery = "部署 发布 go test"

// Fixtures sized like real slices (~6 units each, ~50% shared): short
// three-unit texts fall below the equivalence-gate floor on any compression
// because removing one of three units moves the hash-embedding too far —
// the gate was calibrated on real library slices (≥1.7KB blocks).
const (
	compressA = "本项目部署前必须运行 go test ./... 验证全部包通过，任何失败都要先修复再继续。部署脚本位于 deploy/release.sh，它负责构建镜像并推送。部署完成后必须运行冒烟检查确认服务健康。回滚用 rollback.sh，它会把上一个版本重新拉起。发布窗口只在工作日白天开放，避免无人值守时出问题。"
	compressB = "发布流程说明：本项目部署前必须运行 go test ./... 验证全部包通过，这一步不可跳过。然后打 tag 并推送镜像仓库。发布完成后在群里同步版本号和变更摘要。发布窗口只在工作日白天开放，夜间发布需要双人值守。"
)

func TestCompressCrossSliceDedup(t *testing.T) {
	res := compressSeed(t, []string{compressA, compressB}, &CompressionOptions{Mode: CompressionDedup})
	if res.UnitsDropped == 0 {
		t.Fatalf("expected cross-slice units dropped, got %d", res.UnitsDropped)
	}
	if res.CompressedContentBytes >= res.OriginalContentBytes {
		t.Fatalf("expected compression to shrink content: %d >= %d",
			res.CompressedContentBytes, res.OriginalContentBytes)
	}
	// The shared sentence survives exactly once in the block.
	if n := strings.Count(res.Text, "部署前必须运行 go test ./... 验证"); n != 1 {
		t.Fatalf("shared sentence appears %d times, want 1", n)
	}
	// Each slice's unique tail survives (delete-only never loses kept units).
	if !strings.Contains(res.Text, "rollback.sh") || !strings.Contains(res.Text, "群里同步版本号") {
		t.Fatal("unique units were dropped — compression lost information")
	}
	compressed := 0
	for _, d := range res.Decisions {
		if d.Compressed {
			compressed++
		}
	}
	if compressed == 0 {
		t.Fatal("expected at least one Compressed decision flag")
	}
}

func TestCompressDefaultOffMatchesLegacy(t *testing.T) {
	off := compressSeed(t, []string{compressA, compressB}, nil)
	if off.UnitsDropped != 0 || off.OriginalContentBytes != 0 || off.CompressedContentBytes != 0 {
		t.Fatal("nil Compress must leave compression stats zeroed")
	}
	if n := strings.Count(off.Text, "部署前必须运行 go test ./... 验证"); n != 2 {
		t.Fatalf("legacy behavior must keep both copies, got %d", n)
	}
	for _, d := range off.Decisions {
		if d.Compressed {
			t.Fatal("nil Compress must not set Compressed flags")
		}
	}
}

func TestCompressGateFallbackKeepsOriginal(t *testing.T) {
	// MinSimilarity=1.0 makes every compression fail the gate: the block must
	// be byte-identical to the no-compression build, with no Compressed flags.
	off := compressSeed(t, []string{compressA, compressB}, nil)
	gated := compressSeed(t, []string{compressA, compressB},
		&CompressionOptions{Mode: CompressionDedup, MinSimilarity: 1.0})
	if off.Text != gated.Text {
		t.Fatal("gate fallback must reproduce the uncompressed block byte-for-byte")
	}
	for _, d := range gated.Decisions {
		if d.Compressed {
			t.Fatal("gate-rejected slices must not be marked Compressed")
		}
	}
}

func TestCompressDeterministic(t *testing.T) {
	opts := &CompressionOptions{Mode: CompressionDedup}
	first := compressSeed(t, []string{compressA, compressB}, opts)
	for i := 0; i < 20; i++ {
		again := compressSeed(t, []string{compressA, compressB}, opts)
		if again.Text != first.Text {
			t.Fatalf("iteration %d: compression is not deterministic", i)
		}
	}
}

func TestCompressBudgetGain(t *testing.T) {
	// Size the budget so that only the compressed forms of both slices fit.
	// Render each slice via a real injector to measure exact item bytes.
	uncompressed := compressSeed(t, []string{compressA, compressB}, nil)
	compressed := compressSeed(t, []string{compressA, compressB}, &CompressionOptions{Mode: CompressionDedup})
	if compressed.Bytes >= uncompressed.Bytes {
		t.Fatalf("compressed block %d must be smaller than %d", compressed.Bytes, uncompressed.Bytes)
	}
	// A budget between the two totals admits both only under compression.
	budget := (uncompressed.Bytes + compressed.Bytes) / 2

	store, err := slice.NewFileStore(t.TempDir() + "/slices.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	idx := newBenchIndex()
	for i, c := range []string{compressA, compressB} {
		s := &slice.Slice{ID: "c-" + string(rune('a'+i)), Type: slice.Context,
			Scope: slice.Project, Content: []byte(c)}
		if err := store.Put(s); err != nil {
			t.Fatal(err)
		}
		if err := idx.Insert(s); err != nil {
			t.Fatal(err)
		}
	}
	runWith := func(opts *CompressionOptions) int {
		inj := &Injector{Store: store, Index: idx, Scope: slice.Project, K: 5,
			Budget: budget, AllowedTypes: map[slice.SliceType]bool{slice.Context: true},
			Compress: opts}
		hits, err := idx.Search(sharedQuery, 5, slice.Project)
		if err != nil {
			t.Fatal(err)
		}
		res, err := inj.BuildHits(sharedQuery, hits)
		if err != nil {
			t.Fatal(err)
		}
		return len(res.Slices)
	}
	if n := runWith(nil); n != 1 {
		t.Fatalf("uncompressed run under tight budget: admitted %d, want 1", n)
	}
	if n := runWith(&CompressionOptions{Mode: CompressionDedup}); n != 2 {
		t.Fatalf("compressed run under tight budget: admitted %d, want 2", n)
	}
}

func TestCompressDeleteOnlySubsequence(t *testing.T) {
	content := compressA + "\n" + compressB
	comp := newBlockCompressor(&CompressionOptions{Mode: CompressionDedup})
	out, _ := comp.compressSlice(content)
	if out == "" {
		t.Skip("nothing dropped for this input")
	}
	// Every unit of the output must appear, in order, in the input.
	pos := 0
	for _, u := range splitUnits(out) {
		idx := strings.Index(content[pos:], u)
		if idx < 0 {
			t.Fatalf("output unit %q is not a verbatim subsequence of the input", u)
		}
		pos += idx + len(u)
	}
}

func TestSplitUnitsBoundaries(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"第一句。第二句！", 2},
		{"line one\nline two", 2},
		{"版本 1.2.3 已发布。", 1},                       // decimals stay whole
		{"run go test. then build. then ship.", 3}, // English periods + space
		{"单句无终止符", 1},
	}
	for _, c := range cases {
		if got := len(splitUnits(c.in)); got != c.want {
			t.Errorf("splitUnits(%q) = %d units, want %d", c.in, got, c.want)
		}
	}
}

func TestCompressMarkerSafetyPreserved(t *testing.T) {
	// Content whose compression keeps a unit containing a fake marker must
	// still be escaped in the final block: only the injector's own open and
	// close markers survive verbatim.
	s1 := "正常内容第一句。说明见 [semantix-reuse] 文档，这是必须保留的上下文。独有补充信息用于凑足单元数量，避免整片被门控回退。第二条独有信息说明部署细节。"
	s2 := "另一条切片的独立内容。包含不同的说明文本与发布注意事项。第三句补充运维细节保证单元数量充足。"
	res := compressSeed(t, []string{s1, s2}, &CompressionOptions{Mode: CompressionDedup})
	if got := strings.Count(res.Text, "[semantix-reuse]"); got != 1 {
		t.Fatalf("expected exactly 1 verbatim open marker, got %d (escaping under compression broken)", got)
	}
	if !strings.HasSuffix(res.Text, "[/semantix-reuse]") {
		t.Fatal("close marker missing")
	}
	if !strings.Contains(res.Text, `[\semantix-reuse]`) {
		t.Fatal("in-content marker was not escaped")
	}
}
