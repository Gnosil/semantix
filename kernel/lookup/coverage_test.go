package lookup_test

import (
	"context"
	"fmt"
	"testing"

	"semantix/kernel/bm25"
	"semantix/kernel/cache"
	"semantix/kernel/inject"
	"semantix/kernel/lookup"
	"semantix/kernel/slice"
	"semantix/kernel/zone"
)

func TestBM25CoverageAcrossL2Consumers(t *testing.T) {
	idx := bm25.New()
	for i := 0; i < 101; i++ {
		content := "web server"
		if i == 0 {
			content = "计算机设计"
		}
		if err := idx.Insert(&slice.Slice{
			ID: fmt.Sprint(i), Type: slice.Prompt, Scope: slice.Project,
			Content: []byte(content), Meta: slice.SliceMeta{Origin: slice.OriginUserCurated},
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		query string
		zone  string
		count int
	}{
		{"量子计算机纠错码最小距离", "miss", 0},
		{"计算机设计", "hit", 1},
	} {
		t.Run(tc.zone, func(t *testing.T) {
			results, err := lookup.Execute(idx, map[string]any{"query": tc.query})
			if err != nil || len(results) != 1 || results[0].Zone != tc.zone {
				t.Fatalf("lookup = %+v, %v", results, err)
			}
			hits, err := (&cache.L3Decider{Index: idx}).DecideL2(context.Background(), cache.Query{UserInput: tc.query, Scope: slice.Project})
			if err != nil || len(hits) != tc.count {
				t.Fatalf("DecideL2 = %+v, %v", hits, err)
			}
			zones := zone.Default()
			block, err := (&inject.Injector{Index: idx, Scope: slice.Project, Zones: &zones}).Build(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			if len(block.Slices) != tc.count {
				t.Fatalf("injected %d slices, want %d", len(block.Slices), tc.count)
			}
		})
	}
}
