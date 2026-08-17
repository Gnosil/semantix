// Command semantix-agent is the Semantix coding agent CLI/TUI, built on the
// vendored Reasonix harness (see harness/ATTRIBUTION.md) with the semantix
// kernel integrated in-process.
package main

import (
	"os"

	"semantix/harness/entry"
)

// Build identity injected via -ldflags.
var (
	version      = "dev"
	gitCommit    = ""
	buildTimeUTC = ""
)

func main() {
	os.Exit(entry.Run(os.Args[1:], entry.BuildInfo{
		Version:      version,
		GitCommit:    gitCommit,
		BuildTimeUTC: buildTimeUTC,
	}))
}
