package inject

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"semantix/kernel/fingerprint"
	"semantix/kernel/slice"
)

const observationTranscript = `{"role":"user","content":"Fix parser regression"}
{"role":"assistant","tool_calls":[{"id":"r1","name":"read_file","arguments":{"path":"parser.go"}},{"id":"r2","name":"read_file","arguments":{"path":"parser.go"}},{"id":"e1","name":"edit_file","arguments":{"path":"parser.go"}}]}
{"role":"tool","tool_call_id":"e1","content":"edited parser.go","workspace_mutation":true}
{"role":"assistant","tool_calls":[{"id":"t1","name":"bash","arguments":{"command":"go test ./..."}}]}
{"role":"tool","tool_call_id":"t1","content":"ok","verification":"passed"}
{"role":"assistant","content":"Parser regression fixed and tested."}
`

func TestHistoricalObservationsRetainSourceNotSnapshotValidity(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"parser.go", "unrelated.go"} {
		if err := os.WriteFile(filepath.Join(root, p), []byte("before"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	deps, err := fingerprint.Capture(root, []string{"parser.go", "unrelated.go"})
	if err != nil {
		t.Fatal(err)
	}
	meta := slice.SliceMeta{SourceSession: "past-task", ProjectSlug: "repo", BaseCommit: "old", Origin: slice.OriginSessionAuto, Deps: deps}
	items, err := slice.NewExtractor().Extract([]byte(observationTranscript), meta)
	if err != nil {
		t.Fatal(err)
	}
	cards, err := slice.Distill([]byte(observationTranscript), meta)
	if err != nil {
		t.Fatal(err)
	}
	items = append(items, cards...)
	db := filepath.Join(t.TempDir(), "project.db")
	store, err := slice.NewFileStore(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if err := store.Put(item); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.(interface{ Close() error }).Close(); err != nil {
		t.Fatal(err)
	}
	store = newTestStore(t, db)
	in := &Injector{Scope: slice.Project, Budget: 4096, RootDir: root, CurrentCommit: "new", MinOrigin: slice.OriginSessionAuto,
		AllowedTypes: map[slice.SliceType]bool{slice.Context: true, slice.Memory: true, slice.Result: true}}
	for _, phase := range []string{"unchanged", "unrelated changed", "related changed", "related deleted"} {
		switch phase {
		case "unrelated changed":
			err = os.WriteFile(filepath.Join(root, "unrelated.go"), []byte("after"), 0600)
		case "related changed":
			err = os.WriteFile(filepath.Join(root, "parser.go"), []byte("after"), 0600)
		case "related deleted":
			err = os.Remove(filepath.Join(root, "parser.go"))
		}
		if err != nil {
			t.Fatal(err)
		}
		t.Run(phase, func(t *testing.T) {
			observations := 0
			for _, item := range items {
				got, err := store.Get(item.ID)
				if err != nil {
					t.Fatal(err)
				}
				if got.Meta.SourceSession != meta.SourceSession || got.Meta.BaseCommit != "old" || !reflect.DeepEqual(got.Meta.Deps, deps) {
					t.Fatalf("source evidence changed: %+v", got.Meta)
				}
				out, err := in.BuildHits("parser regression", []slice.Hit{{Slice: got, Score: 2}})
				if err != nil {
					t.Fatal(err)
				}
				if got.Type == slice.Context || got.Type == slice.Memory {
					observations++
					if len(out.Slices) != 1 || !strings.Contains(out.Text, "applicability=historical_observation; revalidate_before_use") {
						t.Errorf("historical card %s excluded/misrepresented: %+v", got.Content, out)
					}
				} else if got.Type == slice.Result && phase != "unchanged" && len(out.Slices) != 0 {
					t.Errorf("stale result admitted: %+v", out)
				}
			}
			if observations != 4 {
				t.Fatalf("observations=%d want all four templates", observations)
			}
		})
	}
}

func TestHistoricalFlagDoesNotPromoteArbitraryClaims(t *testing.T) {
	root := t.TempDir()
	in := &Injector{Scope: slice.Project, Budget: 4096, RootDir: root, CurrentCommit: "new", MinOrigin: slice.OriginSessionAuto,
		AllowedTypes: map[slice.SliceType]bool{slice.Context: true, slice.Memory: true, slice.Result: true}}
	for _, tc := range []struct {
		name string
		typ  slice.SliceType
		meta string
		want string
	}{
		{"legacy", slice.Context, `{"base_commit":"old","SourceSession":"s","origin":"session-auto"}`, "stale_commit"},
		{"result cannot claim history", slice.Result, `{"historical":true,"base_commit":"old","SourceSession":"s","origin":"session-auto","result_status":"verified"}`, "stale_commit"},
		{"no revision", slice.Memory, `{"historical":true,"SourceSession":"s","origin":"session-auto"}`, "commit_unknown"},
		{"no source", slice.Context, `{"historical":true,"base_commit":"old","origin":"session-auto"}`, "stale_commit"},
		{"unsafe path", slice.Context, `{"historical":true,"base_commit":"old","SourceSession":"s","origin":"session-auto","deps":{"../outside":"x"}}`, "dependency_path_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var meta slice.SliceMeta
			if err := json.Unmarshal([]byte(tc.meta), &meta); err != nil {
				t.Fatal(err)
			}
			sl := &slice.Slice{ID: "card", Type: tc.typ, Scope: slice.Project, Content: []byte("parser regression"), Meta: meta}
			out, err := in.BuildHits("parser regression", []slice.Hit{{Slice: sl, Score: 2}})
			if err != nil {
				t.Fatal(err)
			}
			if len(out.Slices) != 0 || out.Decisions[0].Reason != tc.want {
				t.Fatalf("unexpected admission: %+v", out)
			}
		})
	}
	var input slice.SliceMeta
	if err := json.Unmarshal([]byte(`{"historical":true}`), &input); err != nil {
		t.Fatal(err)
	}
	items, err := slice.NewExtractor().Extract([]byte(observationTranscript), input)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.Type == slice.Context {
			continue
		}
		b, _ := json.Marshal(item.Meta)
		if strings.Contains(string(b), `"historical":true`) {
			t.Fatalf("input flag promoted non-observation %s", item.Type)
		}
	}
}
