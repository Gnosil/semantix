package slice

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestFileStoreSourceSessionsDoNotAlias(t *testing.T) {
	for _, operation := range []string{"Put", "Get", "List", "ListAll"} {
		t.Run(operation, func(t *testing.T) {
			st := reopen(t, filepath.Join(t.TempDir(), "memory.jsonl"))
			sl := &Slice{ID: "context", Type: Context, Scope: Project, Content: []byte("cache operations"), Meta: SliceMeta{SourceSession: "first", SourceSessions: []string{"first", "second"}}}
			if err := st.Put(sl); err != nil {
				t.Fatal(err)
			}
			var out *Slice
			var err error
			switch operation {
			case "Put":
				out = sl
			case "Get":
				out, err = st.Get(sl.ID)
			case "List", "ListAll":
				var all []*Slice
				if operation == "List" {
					all, err = st.List(Project)
				} else {
					all, err = st.ListAll()
				}
				if len(all) != 1 {
					t.Fatalf("list length=%d, want 1", len(all))
				}
				out = all[0]
			}
			if err != nil {
				t.Fatal(err)
			}
			out.Meta.SourceSessions[0] = "caller mutation"
			got, err := st.Get(sl.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got.Meta.SourceSessions, []string{"first", "second"}) {
				t.Fatalf("%s aliases stored source sessions: %v", operation, got.Meta.SourceSessions)
			}
		})
	}
}

func TestFileStoreSourceSessionsPersistAndExportImport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "memory.jsonl")
	st := reopen(t, path)
	input := &Slice{ID: "context", Type: Context, Scope: Project, Content: []byte("cache operations"), Meta: SliceMeta{SourceSession: "first", SourceSessions: []string{"first", "second"}, Origin: OriginSessionAuto}}
	if err := st.Put(input); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || len(data) != 0 {
		t.Fatalf("expected journal-only write, base bytes=%d, err=%v", len(data), err)
	}
	closeTestStore(st)
	st = reopen(t, path)
	assertSources := func(st Store) {
		t.Helper()
		got, err := st.Get(input.ID)
		if err != nil || got == nil {
			t.Fatalf("Get after round trip: %+v %v", got, err)
		}
		if got.Meta.SourceSession != "first" || !slices.Equal(got.Meta.SourceSessions, input.Meta.SourceSessions) {
			t.Fatalf("lost source metadata: %+v", got.Meta)
		}
	}
	assertSources(st)
	if err := st.(*fileStore).Compact(); err != nil {
		t.Fatal(err)
	}
	closeTestStore(st)
	st = reopen(t, path)
	assertSources(st)
	var backup bytes.Buffer
	if n, skipped, err := Export(st, &backup); err != nil || n != 1 || skipped != 0 {
		t.Fatalf("Export=(%d,%d,%v)", n, skipped, err)
	}
	// The additive field leaves legacy JSON readable and omits absent lists.
	legacy := &Slice{ID: "legacy", Type: Context, Scope: Project, Content: []byte("legacy memory"), Meta: SliceMeta{SourceSession: "legacy-session"}}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("source_sessions")) {
		t.Fatalf("absent additive metadata serialized: %s", raw)
	}
	backup.Write(raw)
	backup.WriteByte('\n')
	dst := reopen(t, filepath.Join(t.TempDir(), "restored.jsonl"))
	if n, skipped, err := Import(dst, &backup, OriginImport); err != nil || n != 2 || skipped != 0 {
		t.Fatalf("Import=(%d,%d,%v)", n, skipped, err)
	}
	assertSources(dst)
	got, err := dst.Get("legacy")
	if err != nil || got == nil || got.Meta.SourceSession != "legacy-session" || got.Meta.SourceSessions != nil {
		t.Fatalf("legacy JSON changed: %+v %v", got, err)
	}
	imported, err := dst.Get("context")
	if err != nil || imported.Meta.Origin != OriginImport {
		t.Fatalf("import trust boundary changed: %+v %v", imported, err)
	}
}
