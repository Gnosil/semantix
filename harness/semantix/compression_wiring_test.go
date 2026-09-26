package semantix

import (
	"context"
	"strings"
	"testing"

	"semantix/kernel/slice"
)

// compressBridgeFixtureSlices: two Context slices sharing a full sentence, so
// the dedup compressor has real work when enabled. Three filler slices pad
// the library past the strict MinLibrarySize floor.
func compressBridgeFixtureSlices() []*slice.Slice {
	return []*slice.Slice{
		{ID: "ctx-a", Type: slice.Context, Scope: slice.Project,
			Content: []byte("部署前必须运行完整测试套件验证。部署脚本位于 deploy/release.sh，它负责构建镜像并推送。回滚脚本在 rollback.sh，会把上一个版本重新拉起。部署完成后运行冒烟检查确认服务健康。发布窗口只在工作日白天开放，避免无人值守时出问题。"),
			Meta:    slice.SliceMeta{SourceSession: "boot-1"}},
		{ID: "ctx-b", Type: slice.Context, Scope: slice.Project,
			Content: []byte("发布流程：部署前必须运行完整测试套件验证。然后打 tag 并推送镜像仓库。发布完成后在群里同步版本号。发布窗口只在工作日白天开放。变更摘要要附上关联的 issue 链接。"),
			Meta:    slice.SliceMeta{SourceSession: "boot-2"}},
		{ID: "fill-1", Type: slice.Memory, Scope: slice.Project,
			Content: []byte("监控告警接入 pagerduty 值班表"), Meta: slice.SliceMeta{SourceSession: "boot-3"}},
		{ID: "fill-2", Type: slice.Memory, Scope: slice.Project,
			Content: []byte("数据库迁移前备份快照"), Meta: slice.SliceMeta{SourceSession: "boot-4"}},
		{ID: "fill-3", Type: slice.Prompt, Scope: slice.Project,
			Content: []byte("代码评审清单和安全检查项"), Meta: slice.SliceMeta{SourceSession: "boot-5"}},
	}
}

// TestBridgeCompressionWiring: config-level compression="dedup" reaches the
// injector — the assembled block drops the shared unit (smaller text, fewer
// bytes) while staying a well-formed reuse block; the default path keeps
// both copies.
func TestBridgeCompressionWiring(t *testing.T) {
	build := func(compression string) InjectResult {
		t.Helper()
		dir := writeKernelDir(t, compressBridgeFixtureSlices(), nil)
		b := NewBridge(Config{
			Enabled:      true,
			Mode:         "strict",
			Budget:       4096,
			ProjectDir:   dir,
			WorkspaceDir: dir,
			GreyMode:     "audit", // GW4-realistic: grey slices enter under audit markers
			Compression:  compression,
		})
		b.SetLabel("compress-wiring")
		defer b.Close()
		return b.InjectDetailed(context.Background(), "部署 发布 测试套件")
	}

	legacy := build("")
	compressed := build("dedup")

	if !strings.HasPrefix(legacy.Text, "[semantix-reuse]") {
		t.Fatalf("legacy block missing marker: %q", legacy.Text[:40])
	}
	if !strings.HasPrefix(compressed.Text, "[semantix-reuse]") {
		t.Fatalf("compressed block missing marker: %q", compressed.Text[:40])
	}
	// Shared unit appears twice without compression...
	if n := strings.Count(legacy.Text, "部署前必须运行完整测试套件验证"); n != 2 {
		t.Fatalf("legacy block should carry both copies, got %d", n)
	}
	// ...and once under compression.
	if n := strings.Count(compressed.Text, "部署前必须运行完整测试套件验证"); n != 1 {
		t.Fatalf("compressed block should carry one copy, got %d", n)
	}
	if len(compressed.Text) >= len(legacy.Text) {
		t.Fatalf("compressed block %d bytes must be smaller than legacy %d",
			len(compressed.Text), len(legacy.Text))
	}
	// The Compressed decision flag lives on kernel inject.CandidateDecision
	// (covered by kernel/inject tests); the bridge-level event contract is
	// deliberately unchanged in v1.
}
