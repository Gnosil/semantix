package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"semantix/kernel/graph"
	"semantix/kernel/slice"
)

func runDream(args []string, stdout, stderr io.Writer, _ dependencies) error {
	flags := flag.NewFlagSet("dream", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var sessions []string
	flags.Var(stringListFlag{&sessions}, "session", "Semantix session JSONL file or directory (repeatable; one project)")
	if err := flags.Parse(args); err != nil {
		return usageWrap(err)
	}
	if len(sessions) == 0 || flags.NArg() != 0 {
		return usagef("dream: supply at least one --session file or directory for one project")
	}
	files, err := collectSessionFiles(sessions)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return usagef("dream: no session files found")
	}
	var result graph.Graph
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		observations, err := slice.DreamObservations(data)
		if err != nil {
			return fmt.Errorf("replay %s: %w", file, err)
		}
		// A copied transcript cannot count as independent evidence.
		id := fmt.Sprintf("%x", sha256.Sum256(data))
		for _, observation := range observations {
			result.Observe(id, observation)
		}
	}
	return json.NewEncoder(stdout).Encode(result.Edges())
}
