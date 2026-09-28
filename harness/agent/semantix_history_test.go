package agent

import (
	"context"
	"strings"
	"testing"

	"semantix/harness/event"
	"semantix/harness/provider"
	"semantix/harness/semantix"
	"semantix/harness/tool"
	"semantix/kernel/inject"
	"semantix/kernel/slice"
)

func TestSemantixHistoryUsesUserRoleAndFixedSystemPolicy(t *testing.T) {
	base := []provider.Message{
		{Role: provider.RoleSystem, Content: "base system"},
		{Role: provider.RoleUser, Content: "current task"},
	}
	got := prependSemantixHistory(base, "[semantix-reuse]\nhistorical evidence\n[/semantix-reuse]")
	if len(got) != 3 {
		t.Fatalf("messages=%d, want 3: %+v", len(got), got)
	}
	if got[0].Role != provider.RoleSystem || !strings.Contains(got[0].Content, semantixHistoryPolicy) {
		t.Fatalf("system policy missing: %+v", got[0])
	}
	if strings.Contains(got[0].Content, "historical evidence") {
		t.Fatalf("slice body retained system authority: %+v", got[0])
	}
	if got[1].Role != provider.RoleUser || !strings.Contains(got[1].Content, "historical evidence") {
		t.Fatalf("history message = %+v, want user-role evidence", got[1])
	}
	if got[2].Role != base[1].Role || got[2].Content != base[1].Content {
		t.Fatalf("current task changed: got %+v want %+v", got[2], base[1])
	}
}

func TestSamplingRequestCoalescesSemantixHistoryForStrictProviders(t *testing.T) {
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "base system"},
		{Role: provider.RoleUser, Content: "current task"},
	}}
	a := New(&fakeProvider{reply: "ok"}, tool.NewRegistry(), sess, Options{StrictAlternatingRoles: true}, event.Discard)
	a.turn.injectBlock = "[semantix-reuse]\nhistorical evidence\n[/semantix-reuse]"
	prepared, err := a.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i, msg := range prepared.req.Messages {
		if msg.Role == provider.RoleSystem && strings.Contains(msg.Content, "historical evidence") {
			t.Fatalf("message %d gives slice body system authority: %+v", i, msg)
		}
		if i > 0 && prepared.req.Messages[i-1].Role == msg.Role {
			t.Fatalf("adjacent %s roles after strict projection: %+v", msg.Role, prepared.req.Messages)
		}
	}
	if len(prepared.req.Messages) < 2 || prepared.req.Messages[1].Role != provider.RoleUser ||
		!strings.Contains(prepared.req.Messages[1].Content, "historical evidence") ||
		!strings.Contains(prepared.req.Messages[1].Content, "current task") {
		t.Fatalf("history and current task not preserved in user context: %+v", prepared.req.Messages)
	}
}

func TestSemantixHistoryStaysWithCurrentTurn(t *testing.T) {
	base := []provider.Message{
		{Role: provider.RoleSystem, Content: "base system"},
		{Role: provider.RoleUser, Content: "old task"},
		{Role: provider.RoleAssistant, Content: "old answer"},
		{Role: provider.RoleUser, Content: "current task"},
	}
	got := prependSemantixHistory(base, "historical evidence")
	if len(got) != 5 {
		t.Fatalf("messages=%d, want 5: %+v", len(got), got)
	}
	want := []string{"base system", "old task", "old answer", "historical evidence", "current task"}
	for i, content := range want {
		if !strings.Contains(got[i].Content, content) {
			t.Fatalf("message[%d]=%q, want content %q; history must stay with current turn", i, got[i].Content, content)
		}
	}
}

func TestManagedContextReplacementKeepsTranscriptAndFallsBack(t *testing.T) {
	source := "<semantix-managed-context project=\"demo\" revision=\"v1\">\n{\n  \"release_channel\": \"stable\"\n}</semantix-managed-context>"
	result, err := (&inject.Injector{Budget: 4096, AllowedTypes: map[slice.SliceType]bool{slice.Context: true}, MinOrigin: slice.OriginSessionAuto}).BuildHits("release channel", []slice.Hit{{Score: 1, Slice: &slice.Slice{
		ID: "s1", Type: slice.Context, Content: []byte(source),
		Meta: slice.SliceMeta{ProjectSlug: "demo", BaseCommit: "v1", Origin: slice.OriginUserCurated, SourceSession: "session"},
	}}})
	if err != nil || result == nil || result.Text == "" {
		t.Fatalf("real L2 assembly failed: result=%+v err=%v", result, err)
	}
	block := result.Text
	sess := &Session{Messages: []provider.Message{
		{Role: provider.RoleSystem, Content: "policy"},
		{Role: provider.RoleUser, Content: source},
		{Role: provider.RoleUser, Content: "Which release channel?"},
	}}
	a := New(&fakeProvider{reply: "ok"}, tool.NewRegistry(), sess, Options{}, event.Discard)
	a.semantix = semantix.NewBridge(semantix.Config{Enabled: true, Mode: "replace"})
	a.turn.injectBlock = block
	prepared, err := a.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.req.Messages) != 4 ||
		prepared.req.Messages[1].Content != `<semantix-managed-context project="demo" revision="v1">{"release_channel":"stable"}</semantix-managed-context>` ||
		!strings.Contains(prepared.req.Messages[2].Content, `source="session"`) ||
		!strings.Contains(prepared.req.Messages[2].Content, "content=earlier managed context user message") ||
		strings.Contains(prepared.req.Messages[2].Content, "release_channel") ||
		prepared.req.Messages[3].Content != "Which release channel?" ||
		strings.Contains(prepared.req.Messages[0].Content, "semantix-reuse") ||
		!strings.Contains(prepared.req.Messages[0].Content, semantixHistoryPolicy) ||
		sess.Messages[1].Content != source {
		t.Fatalf("replacement changed the wrong context: %+v", prepared.req.Messages)
	}
	a.turn.injectBlock = strings.Replace(block, `commit="v1"`, `commit="stale"`, 1)
	fallback, err := a.prepareSamplingRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(joinContents(fallback.req.Messages), a.turn.injectBlock) ||
		!strings.Contains(joinContents(fallback.req.Messages), source) {
		t.Fatalf("mismatched provenance did not keep original injection: %+v", fallback.req.Messages)
	}
}
