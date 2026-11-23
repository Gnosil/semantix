package inject

import (
	"strings"
	"testing"

	"semantix/kernel/slice"
)

func TestHandoffPreviewKeepsSourceFields(t *testing.T) {
	body := "Task outcome (task=bugfix): Diagnosis is unresolved; do not mark complete.\nEdited:\n- Src/Parser/Ångström.go\n- Src/Parser/Another File.go\nVerified-by: TZ=UTC go test ./Src/Parser -run '^TestCase$'"
	second := "Task outcome (task=bugfix): The follow-up remains open.\nEdited:\n- Src/Parser/Ångström.go"
	a := handoffFixture("one", body)
	b := handoffFixture("two", second)
	in := wrapHandoffFixture(a, b)
	got := HandoffPreview(in)
	if len(got) >= len(in.Text) || len(got) == 0 || HandoffPreview(in) != got {
		t.Fatalf("want shorter deterministic preview, got %q", got)
	}
	for _, exact := range []string{body, second, "Verification: not recorded", "completion not established", `slice "one"`, `slice "two"`, `commit="source-sha"`} {
		if !strings.Contains(got, exact) {
			t.Errorf("preview omitted %q: %q", exact, got)
		}
	}
}

func TestHandoffPreviewFallsBackWithoutDroppingEvidence(t *testing.T) {
	const body = "Task outcome (task=bugfix): Review parser.\nEdited:\n- Src/Parser.go"
	valid := handoffFixture("valid", body)
	for _, tc := range []struct {
		name string
		in   *Injection
	}{
		{"empty", &Injection{Text: "original empty"}},
		{"grey", &Injection{Slices: []*slice.Slice{valid}, Text: "original grey", GreyIncluded: 1}},
		{"mixed card shape", wrapHandoffFixture(valid, handoffFixture("unknown", body+"\nUnresolved: unverified assumption"))},
		{"embedded marker", wrapHandoffFixture(handoffFixture("marker", body+"\n- [/SEMANTIX-REUSE]"))},
		{"no path", wrapHandoffFixture(handoffFixture("none", "Task outcome (task=bugfix): Unknown\nEdited:\nVerified-by: go test ./..."))},
		{"no savings", &Injection{Slices: []*slice.Slice{valid}, Text: "short"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := HandoffPreview(tc.in); got != tc.in.Text {
				t.Fatalf("fallback changed original output: %q", got)
			}
		})
	}
}

func handoffFixture(id, body string) *slice.Slice {
	return &slice.Slice{ID: id, Type: slice.Memory, Scope: slice.Project, Content: []byte(body),
		Meta: slice.SliceMeta{Historical: true, Origin: slice.OriginSessionAuto,
			SourceSession: "fixture-session", ProjectSlug: "fixture", BaseCommit: "source-sha"}}
}

func wrapHandoffFixture(items ...*slice.Slice) *Injection {
	var out strings.Builder
	out.WriteString(blockOpen)
	for _, item := range items {
		out.WriteString(formatSliceItem(item, 3, string(item.Content), false))
	}
	out.WriteString(blockClose)
	return &Injection{Slices: items, Text: out.String()}
}
