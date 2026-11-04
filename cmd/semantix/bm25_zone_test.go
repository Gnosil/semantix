package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"semantix/kernel/slice"
)

func TestBM25ZonesThroughCLI(t *testing.T) {
	db := filepath.Join(t.TempDir(), "lib.db")
	store := openTestStore(t, db)
	for i := 0; i < 101; i++ {
		content := "web server"
		if i == 0 {
			content = "计算机设计"
		}
		if err := store.Put(&slice.Slice{ID: fmt.Sprint(i), Type: slice.Prompt, Scope: slice.User, Content: []byte(content)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range []string{"search", "lookup"} {
		for _, tc := range []struct{ query, want string }{
			{"量子计算机纠错码最小距离", "miss"},
			{"计算机设计", "hit"},
		} {
			t.Run(command+"/"+tc.want, func(t *testing.T) {
				var out, stderr bytes.Buffer
				args := []string{"--query", tc.query, "--scope", "user", "--db", db, "--json"}
				if command == "search" {
					args = append(args, "--retriever", "bm25")
				}
				if code := run(append([]string{command}, args...), &out, &stderr, productionDependencies()); code != 0 {
					t.Fatalf("exit %d: %s", code, stderr.String())
				}
				var result struct {
					Data []struct {
						Zone  string
						Score float64
					}
				}
				if err := json.Unmarshal(out.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if len(result.Data) != 1 || result.Data[0].Zone != tc.want || result.Data[0].Score <= 0.7 {
					t.Fatalf("unexpected result: %s", out.String())
				}
			})
		}
	}
}
