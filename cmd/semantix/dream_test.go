package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"semantix/kernel/graph"
)

func TestDreamRequiresIndependentHostEvidence(t *testing.T) {
	const observed = `{"role":"assistant","tool_calls":[{"id":"a","name":"editFile","arguments":{"path":"pkg/a.go"}},{"id":"b","name":"writeFile","arguments":{"path":"pkg/b.go"}},{"id":"c","name":"bash","arguments":{"command":"go test ./pkg"}}]}
{"role":"tool","tool_call_id":"a","workspace_mutation":true}
{"role":"tool","tool_call_id":"b","workspace_mutation":true}
{"role":"tool","tool_call_id":"c","verification":"passed"}
`
	dir := t.TempDir()
	write := func(name, body string) string {
		file := filepath.Join(dir, name)
		if err := os.WriteFile(file, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		return file
	}
	one := write("one.jsonl", observed)
	copy := write("copy.jsonl", observed)
	stale := write("stale.jsonl", observed+`{"role":"assistant","tool_calls":[{"id":"d","name":"editFile","arguments":{"path":"pkg/a.go"}}]}
{"role":"tool","tool_call_id":"d","workspace_mutation":true}
{"role":"assistant","content":"Tests passed after editing pkg/a.go"}
`)
	two := write("two.jsonl", `{"role":"user","content":"Another session"}`+"\n"+observed)
	run := func(paths ...string) ([]graph.Edge, string) {
		var out bytes.Buffer
		var args []string
		for _, path := range paths {
			args = append(args, "--session", path)
		}
		if err := runDream(args, &out, io.Discard, dependencies{}); err != nil {
			t.Fatal(err)
		}
		var edges []graph.Edge
		if err := json.Unmarshal(out.Bytes(), &edges); err != nil {
			t.Fatal(err)
		}
		return edges, out.String()
	}
	if edges, _ := run(one, copy); len(edges) != 0 {
		t.Fatalf("one distinct transcript published edges: %+v", edges)
	}
	if edges, _ := run(one, stale); len(edges) != 1 || edges[0].Relation != "co_edited" {
		t.Fatalf("a later mutation must invalidate verification: %+v", edges)
	}
	edges, before := run(one, two, copy)
	if len(edges) != 3 || edges[1].Relation != "verifies" || edges[1].From != "go test ./pkg" {
		t.Fatalf("unexpected snapshot: %+v", edges)
	}
	for _, edge := range edges {
		if len(edge.Sessions) != 2 {
			t.Fatalf("each edge must have exactly two sources: %+v", edge)
		}
	}
	if _, after := run(copy, two, one); before != after {
		t.Fatal("input order changed snapshot bytes")
	}
}
