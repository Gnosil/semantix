package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"semantix/harness/provider"
	semantixbridge "semantix/harness/semantix"
	"semantix/kernel/slice"
)

func TestShadowRetrievalKeepsProviderMessagesByteIdenticalToOff(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".semantix"), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := slice.NewFileStore(filepath.Join(dir, ".semantix", "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Put(&slice.Slice{ID: "ctx-1", Type: slice.Context, Scope: slice.Project, Content: []byte("修复 go 测试失败")}); err != nil {
		t.Fatal(err)
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		_ = closer.Close()
	}

	off := semantixbridge.NewBridge(semantixbridge.Config{Enabled: true, Mode: "off", ProjectDir: dir})
	shadow := semantixbridge.NewBridge(semantixbridge.Config{Enabled: true, Mode: "shadow", ProjectDir: dir})
	offResult := off.InjectDetailed(context.Background(), "修复 go 测试")
	shadowResult := shadow.InjectDetailed(context.Background(), "修复 go 测试")
	if shadowResult.Diagnostics == nil || len(shadowResult.Diagnostics.Candidates) == 0 {
		t.Fatalf("shadow retrieval did not run: %+v", shadowResult.Diagnostics)
	}

	base := []provider.Message{{Role: provider.RoleSystem, Content: "system"}, {Role: provider.RoleUser, Content: "修复 go 测试"}}
	assemble := func(block string) []provider.Message {
		if block == "" {
			return append([]provider.Message(nil), base...)
		}
		return prependSemantixHistory(base, block)
	}
	offJSON, err := json.Marshal(assemble(offResult.Text))
	if err != nil {
		t.Fatal(err)
	}
	shadowJSON, err := json.Marshal(assemble(shadowResult.Text))
	if err != nil {
		t.Fatal(err)
	}
	if string(offJSON) != string(shadowJSON) {
		t.Fatalf("provider messages differ\noff:    %s\nshadow: %s", offJSON, shadowJSON)
	}
}
