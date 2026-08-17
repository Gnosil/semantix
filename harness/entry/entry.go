// Package entry is the public seam between cmd/semantix-agent and the
// vendored harness internals: semantix/harness/internal/... is only
// importable from within semantix/harness/..., so the top-level command
// goes through this package (spec h2h3-resource-orchestration §3).
package entry

import (
	"runtime/debug"

	"semantix/harness/internal/cli"
	"semantix/harness/internal/config"
	"semantix/harness/internal/crashreport"

	// Blank imports wire compile-time built-ins into their registries,
	// mirroring the upstream cmd/reasonix entrypoint.
	_ "semantix/harness/internal/provider/anthropic"
	_ "semantix/harness/internal/provider/openai"
	_ "semantix/harness/internal/provider/responses"
	_ "semantix/harness/internal/tool/builtin"
)

// BuildInfo carries -ldflags build identity through to `version --verbose/--json`.
type BuildInfo struct {
	Version      string
	GitCommit    string
	BuildTimeUTC string
}

// Run executes the harness CLI with crash capture and returns the exit code.
func Run(args []string, info BuildInfo) (exitCode int) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = crashreport.CapturePanic(config.ReasonixHomeDir(), info.Version, recovered, debug.Stack())
			panic(recovered)
		}
	}()
	return cli.RunWithBuildInfo(args, cli.BuildInfo{
		Version:      info.Version,
		GitCommit:    info.GitCommit,
		BuildTimeUTC: info.BuildTimeUTC,
	})
}
