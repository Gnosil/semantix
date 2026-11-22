package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/slice"
)

func TestSearchVectorRetriever(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lib.db")
	store := openTestStore(t, db)
	for _, c := range []string{"修复 go 测试失败", "配置 CI 流水线", "部署到服务器"} {
		sl := &slice.Slice{ID: "v-" + c[:4], Type: slice.Prompt, Scope: slice.Project, Content: []byte(c)}
		if err := store.Put(sl); err != nil {
			t.Fatal(err)
		}
	}

	var out bytes.Buffer
	code := run([]string{"search", "--query", "修复测试失败", "--db", db, "--retriever", "vector", "--limit", "2"}, &out, &emptyStderr{}, productionDependencies())
	if code != 0 {
		t.Fatalf("vector search code = %d, out:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "修复 go 测试失败") {
		t.Fatalf("vector search missed the repair slice:\n%s", out.String())
	}
	if strings.HasPrefix(out.String(), "1. score=0.000000") {
		t.Fatalf("vector scores not populated:\n%s", out.String())
	}
}

func TestSearchHybridRetriever(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lib.db")
	store := openTestStore(t, db)
	for _, c := range []string{"修复 go 测试失败", "配置 CI 流水线"} {
		sl := &slice.Slice{ID: "h-" + c[:4], Type: slice.Prompt, Scope: slice.Project, Content: []byte(c)}
		if err := store.Put(sl); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	code := run([]string{"search", "--query", "修复测试失败", "--db", db, "--retriever", "hybrid", "--limit", "2"}, &out, &emptyStderr{}, productionDependencies())
	if code != 0 {
		t.Fatalf("hybrid search code = %d, out:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "修复 go 测试失败") {
		t.Fatalf("hybrid search missed the repair slice:\n%s", out.String())
	}
}


// U30: search 文本输出带 zone 图标 + 来源会话 + 命中摘要行（vibe-coder 可读）。
// s1 与 query 全匹配（必为 🟢 hit）；s2/s3 部分匹配、分数非零，保证三行都进
// 结果，跨会话复用（3 个来源会话）可感知。
func TestSearchHitVisualization(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lib.db")
	store := openTestStore(t, db)
	for _, s := range []struct {
		id, sess, content string
	}{
		{"s1", "sess-a", "修复 go 测试失败"},
		{"s2", "sess-b", "修复 go 测试超时"},
		{"s3", "sess-c", "修复 go 测试崩溃"},
	} {
		sl := &slice.Slice{ID: s.id, Type: slice.Prompt, Scope: slice.Project, Content: []byte(s.content),
			Meta: slice.SliceMeta{SourceSession: s.sess}}
		if err := store.Put(sl); err != nil {
			t.Fatal(err)
		}
	}

	var out bytes.Buffer
	code := run([]string{"search", "--query", "修复 go 测试失败", "--db", db, "--limit", "3", "--retriever", "bm25"},
		&out, &emptyStderr{}, productionDependencies())
	if code != 0 {
		t.Fatalf("search code = %d, out:\n%s", code, out.String())
	}
	got := out.String()
	for _, want := range []string{"🟢", "zone=hit", "from:sess-a", "from:sess-b", "from:sess-c", "🎯 ", "hits in 3 sessions"} {
		if !strings.Contains(got, want) {
			t.Errorf("search output missing %q:\n%s", want, got)
		}
	}
}

type emptyStderr struct{}

func (emptyStderr) Write(p []byte) (int, error) { return len(p), nil }

var _ = bm25.New // keep import used if search tests reference it later

// TestSearchHybridFusionRRFZoneTriState covers the Issue #274 search
// wiring: hybrid with --fusion rrf emits scaled scores in [0,1] so the
// zone classifier sees hit/grey/miss (the historical RRF path classified
// every hit as miss — spec §1 defect), and the flags are validated.
func TestSearchHybridFusionRRFZoneTriState(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lib.db")
	store, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := store.(interface{ Close() error }); ok {
		defer c.Close() // release the journal before TempDir cleanup (Windows)
	}
	for _, c := range []string{"修复 go 测试失败", "配置 CI 流水线", "部署到服务器"} {
		sl := &slice.Slice{ID: "f-" + c[:4], Type: slice.Prompt, Scope: slice.Project, Content: []byte(c)}
		if err := store.Put(sl); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	code := run([]string{"search", "--query", "修复测试失败", "--db", db, "--retriever", "hybrid",
		"--fusion", "rrf", "--rrf-k", "60", "--limit", "5"}, &out, &emptyStderr{}, productionDependencies())
	if code != 0 {
		t.Fatalf("hybrid rrf search code = %d, out:\n%s", code, out.String())
	}
	// The repair slice matches both routes → top hit, zone must be a
	// non-miss classification (the historical RRF path reported miss for
	// every hit because raw RRF scores sit below AbsLow).
	if !strings.Contains(out.String(), "修复 go 测试失败") {
		t.Fatalf("hybrid rrf search missed the repair slice:\n%s", out.String())
	}
	if strings.Contains(out.String(), "❌ miss") && !strings.Contains(out.String(), "🟢 hit") {
		t.Fatalf("hybrid rrf must not classify every hit as miss:\n%s", out.String())
	}
	// Invalid fusion strategy is a usage error.
	code = run([]string{"search", "--query", "修复测试失败", "--db", db, "--retriever", "hybrid",
		"--fusion", "avg", "--limit", "5"}, &out, &emptyStderr{}, productionDependencies())
	if code != 2 {
		t.Fatalf("fusion=avg code = %d, want 2 (usage error)", code)
	}
}

// TestSearchHybridFusionBM25Weight covers the weighted share flag: an
// explicit --bm25-weight 1 keeps the pure BM25 ordering.
func TestSearchHybridFusionBM25Weight(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lib.db")
	store, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := store.(interface{ Close() error }); ok {
		defer c.Close() // release the journal before TempDir cleanup (Windows)
	}
	for _, c := range []string{"修复 go 测试失败", "配置 CI 流水线"} {
		sl := &slice.Slice{ID: "w-" + c[:4], Type: slice.Prompt, Scope: slice.Project, Content: []byte(c)}
		if err := store.Put(sl); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	code := run([]string{"search", "--query", "修复测试失败", "--db", db, "--retriever", "hybrid",
		"--bm25-weight", "1", "--limit", "2"}, &out, &emptyStderr{}, productionDependencies())
	if code != 0 {
		t.Fatalf("hybrid w=1 search code = %d, out:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "修复 go 测试失败") {
		t.Fatalf("hybrid w=1 search missed the repair slice:\n%s", out.String())
	}
	// Out-of-range weight is a usage error.
	code = run([]string{"search", "--query", "修复测试失败", "--db", db, "--retriever", "hybrid",
		"--bm25-weight", "1.5", "--limit", "2"}, &out, &emptyStderr{}, productionDependencies())
	if code != 2 {
		t.Fatalf("bm25-weight=1.5 code = %d, want 2 (usage error)", code)
	}
}

func TestSearchHybridRespectsLimitAboveDefaultPool(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "lib.db")
	store := openTestStore(t, db)
	for i := range 30 {
		sl := &slice.Slice{
			ID:      fmt.Sprintf("hlim-%02d", i),
			Type:    slice.Prompt,
			Scope:   slice.Project,
			Content: []byte(fmt.Sprintf("cobalt lighthouse protocol fixture number-%02d", i)),
		}
		if err := store.Put(sl); err != nil {
			t.Fatal(err)
		}
	}

	for _, tc := range []struct {
		name           string
		args           []string
		want           int
		wantDualRoutes int
	}{
		{
			name: "hybrid weighted pure bm25 limit 30",
			args: []string{"search", "--query", "cobalt lighthouse protocol fixture", "--db", db,
				"--retriever", "hybrid", "--bm25-weight", "1", "--limit", "30", "--json"},
			want: 30,
		},
		{
			name: "hybrid rrf limit 30",
			args: []string{"search", "--query", "cobalt lighthouse protocol fixture", "--db", db,
				"--retriever", "hybrid", "--fusion", "rrf", "--limit", "30", "--json"},
			want:           30,
			wantDualRoutes: 30,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			code := run(tc.args, &out, &emptyStderr{}, productionDependencies())
			if code != 0 {
				t.Fatalf("search code = %d, out:\n%s", code, out.String())
			}
			var resp struct {
				OK   bool           `json:"ok"`
				Data []searchResult `json:"data"`
			}
			if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
				t.Fatalf("decode search json: %v\nout:\n%s", err, out.String())
			}
			if !resp.OK {
				t.Fatalf("expected ok=true envelope, got %+v", resp)
			}
			if len(resp.Data) != tc.want {
				t.Fatalf("got %d results, want %d", len(resp.Data), tc.want)
			}
			if tc.wantDualRoutes > 0 {
				dualRoutes := 0
				for _, hit := range resp.Data {
					if hit.Score > 0.5 {
						dualRoutes++
					}
				}
				if dualRoutes != tc.wantDualRoutes {
					t.Fatalf("got %d dual-route RRF hits (Score > 0.5), want %d", dualRoutes, tc.wantDualRoutes)
				}
			}
		})
	}
}
