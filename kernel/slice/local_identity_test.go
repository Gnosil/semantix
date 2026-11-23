package slice

import (
	"path/filepath"
	"testing"
)

// #531: a future shared identity format must not change the existing local
// IDs. Check golden values, not just determinism within a single build.
func TestLocalSliceIdentityCompatibility(t *testing.T) {
	const content = "slice identity fixture"
	cases := []struct {
		typ   SliceType
		scope Scope
		want  string
	}{
		{Prompt, Project, "0ebba12817d3b186"},
		{Context, Project, "fa4b2b59d0fca5e5"},
		{Context, User, "4331e3153d539da3"},
	}
	path := filepath.Join(t.TempDir(), "local.jsonl")
	st, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestStore(st)
	for _, tc := range cases {
		id := sliceID([]byte(content), tc.typ, tc.scope)
		if id != tc.want {
			t.Fatalf("local %s/%s ID = %q, want %q", tc.typ, tc.scope, id, tc.want)
		}
		if err := st.Put(&Slice{ID: id, Type: tc.typ, Scope: tc.scope, Content: []byte(content)}); err != nil {
			t.Fatal(err)
		}
	}
	closeTestStore(st)
	reopened, err := NewFileStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestStore(reopened)
	for _, tc := range cases {
		got, err := reopened.Get(tc.want)
		if err != nil {
			t.Fatalf("load existing local ID %s: %v", tc.want, err)
		}
		if got.ID != tc.want || got.Type != tc.typ || got.Scope != tc.scope || string(got.Content) != content {
			t.Fatalf("local round trip changed record %s: %+v", tc.want, got)
		}
	}
}
