// Render controlled candidates through the real Semantix L2 assembly path.
// This experiment isolates assembly; it does not claim retrieval or agent E2E coverage.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"semantix/kernel/inject"
	"semantix/kernel/slice"
)

func main() {
	var contents []string
	if err := json.NewDecoder(os.Stdin).Decode(&contents); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	blocks := make([]string, 0, len(contents))
	for _, content := range contents {
		in := inject.Injector{AllowedTypes: map[slice.SliceType]bool{slice.Context: true}, CurrentCommit: "controlled-v1", MinOrigin: slice.OriginSessionAuto}
		result, err := in.BuildHits("project context", []slice.Hit{{Score: 1, Slice: &slice.Slice{
			ID: "controlled-context", Type: slice.Context, Content: []byte(content),
			Meta: slice.SliceMeta{ProjectSlug: "replacement-experiment", BaseCommit: "controlled-v1", Origin: slice.OriginUserCurated, SourceSession: "controlled-fixture"},
		}}})
		if err != nil || result == nil || result.Text == "" {
			fmt.Fprintln(os.Stderr, "L2 assembly rejected controlled candidate", err)
			os.Exit(1)
		}
		blocks = append(blocks, result.Text)
	}
	if err := json.NewEncoder(os.Stdout).Encode(blocks); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
