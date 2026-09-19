package slice

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestConsolidatePreservesProvenance(t *testing.T) {
	for _, mismatch := range []string{"none", "project", "commit", "deps", "origin", "scope"} {
		t.Run(mismatch, func(t *testing.T) {
			store, err := NewFileStore(filepath.Join(t.TempDir(), "project.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer closeTestStore(store)
			a := &Slice{ID: "a", Type: Context, Scope: Project, Content: []byte("Project context:\nFrequent paths:\n- parser.go (2)\n- lexer.go (1)"), Meta: SliceMeta{SourceSession: "source-a", ProjectSlug: "repo", BaseCommit: "revision", Origin: OriginSessionAuto}}
			b := &Slice{ID: "b", Type: Context, Scope: Project, Content: []byte("Project context:\nFrequent paths:\n- parser.go (3)\n- lexer.go (2)"), Meta: a.Meta}
			b.Meta.SourceSession = "source-b"
			switch mismatch {
			case "project":
				b.Meta.ProjectSlug = "other"
			case "commit":
				b.Meta.BaseCommit = "other"
			case "deps":
				b.Meta.Deps = map[string]string{"lexer.go": "different"}
			case "origin":
				b.Meta.Origin = OriginImport
			case "scope":
				b.Scope = User
			}
			for _, s := range []*Slice{a, b} {
				if err := store.Put(s); err != nil {
					t.Fatal(err)
				}
			}
			res, err := ConsolidateContext(store, ConsolidateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			all, err := store.ListAll()
			if err != nil {
				t.Fatal(err)
			}
			if mismatch != "none" {
				if res.Merged != 0 || len(all) != 2 {
					t.Fatalf("incompatible %s merged: %+v", mismatch, res)
				}
				return
			}
			if res.Merged != 1 || len(all) != 1 {
				t.Fatalf("compatible merge: %+v", res)
			}
			raw, _ := json.Marshal(all[0].Meta)
			var m struct {
				Sources []string `json:"source_sessions"`
			}
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(m.Sources, []string{"source-a", "source-b"}) {
				t.Fatalf("merged sources = %v, want both sessions", m.Sources)
			}
			if all[0].Meta.BaseCommit != "revision" || all[0].Meta.ProjectSlug != "repo" {
				t.Fatalf("lost boundaries: %+v", all[0].Meta)
			}
			// A subsequent consolidation must retain earlier member sources.
			c := &Slice{ID: "c", Type: Context, Scope: Project, Content: a.Content, Meta: a.Meta}
			c.Meta.SourceSession = "source-c"
			if err := store.Put(c); err != nil {
				t.Fatal(err)
			}
			if _, err := ConsolidateContext(store, ConsolidateOptions{}); err != nil {
				t.Fatal(err)
			}
			all, _ = store.ListAll()
			raw, _ = json.Marshal(all[0].Meta)
			_ = json.Unmarshal(raw, &m)
			if !reflect.DeepEqual(m.Sources, []string{"source-a", "source-b", "source-c"}) {
				t.Fatalf("repeat merge sources=%v", m.Sources)
			}
		})
	}
}

func TestConsolidatePreservesFeedback(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "project.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer closeTestStore(store)
	for i, id := range []string{"a", "b"} {
		s := &Slice{ID: id, Type: Context, Scope: Project, Content: []byte("Project context:\nFrequent paths:\n- parser.go (2)"), Stats: SliceStats{Injected: 2, Harmful: 1, UserFeedback: -1, LastUsed: int64(i + 10)}}
		if err := store.Put(s); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ConsolidateContext(store, ConsolidateOptions{}); err != nil {
		t.Fatal(err)
	}
	all, _ := store.ListAll()
	if len(all) != 1 || all[0].Stats.Injected != 4 || all[0].Stats.Harmful != 2 || all[0].Stats.UserFeedback != -2 || all[0].Stats.LastUsed != 11 {
		t.Fatalf("lost feedback: %+v", all)
	}
}
