package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"semantix/kernel/bm25"
	"semantix/kernel/config"
	"semantix/kernel/slice"
)

type dependencies struct {
	newExtractor func() slice.Extractor
	openStore    func(string) (slice.Store, error)
	newIndex     func() slice.Index
	resolved     *config.Resolved // M2-U20 effective config (nil in unit tests)
}

func productionDependencies() dependencies {
	return dependencies{
		newExtractor: slice.NewExtractor,
		openStore:    slice.NewFileStore,
		newIndex: func() slice.Index {
			return bm25.New()
		},
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, productionDependencies()))
}

// usageError marks a command-line usage mistake (unknown flag, missing or
// invalid argument). run() maps it to exit code 2 per the U19 contract
// (docs/reports/cli-v2-architecture.md §4.3).
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// usagef builds a usageError from a format string.
func usagef(format string, args ...any) error {
	return usageError{err: fmt.Errorf(format, args...)}
}

// usageWrap marks an existing error (e.g. a flag parse error) as a usage
// mistake while preserving its error chain so errors.Is/errors.As still
// work on it (flag.ErrHelp must stay detectable through the wrapper).
func usageWrap(err error) error {
	if err == nil {
		return nil
	}
	var ue usageError
	if errors.As(err, &ue) {
		return err // already marked
	}
	return usageError{err: err}
}

// isUsage reports whether err carries the usage-error marker.
func isUsage(err error) bool {
	var ue usageError
	return errors.As(err, &ue)
}

// commandGroup is one branch of the CLI v2 command tree
// (docs/reports/cli-v2-architecture.md §3). U19 freezes the four groups;
// later units (U21/U23-U27) mount commands into the empty ones.
type commandGroup int

const (
	groupKernelOps   commandGroup = iota // kernel 运维（U19：现有 8 命令，行为不变）
	groupProduct                         // 产品与管理（U21/U23/U24/U25 挂载）
	groupMaintenance                     // 维护（U26 挂载）
	groupService                         // 服务模式（U27 挂载）
)

func (g commandGroup) String() string {
	switch g {
	case groupKernelOps:
		return "Kernel operations"
	case groupProduct:
		return "Product & management"
	case groupMaintenance:
		return "Maintenance"
	case groupService:
		return "Service mode"
	}
	return "Unknown group"
}

// plannedByGroup lists commands planned for groups not implemented yet. They
// render in help as planned and reject dispatch with exit 2 until their unit
// lands. Empty today — the former service-mode placeholders (serve/watch)
// were never implemented and were removed in the tool-set minimization.
var plannedByGroup = map[commandGroup][]string{}

// commandSpec registers one CLI command in the command tree. All commands
// share one dispatch signature; error-returning commands are adapted with
// errCommand so the exit-code contract is enforced in one place.
type commandSpec struct {
	name    string
	group   commandGroup
	usage   string // one-line invocation synopsis (help)
	summary string // one-line description (help)
	run     func(args []string, stdout, stderr io.Writer, deps dependencies) int
}

// commands is the command tree, in help order within each group. It is
// populated in init() rather than via a variable initializer so buildCommands
// can reference package-level helpers without an init dependency cycle.
var commands []commandSpec

func init() {
	commands = buildCommands()
}

func buildCommands() []commandSpec {
	return []commandSpec{
		{name: "extract", group: groupKernelOps,
			usage:   "semantix extract --input <session.jsonl> [--scope project|user|session]",
			summary: "extract session JSONL into semantic slices",
			run:     errCommand("extract", runExtract)},
		{name: "search", group: groupKernelOps,
			usage:   "semantix search [flags] <query>",
			summary: "semantic retrieval (bm25 / vector / hybrid)",
			run:     errCommand("search", runSearch)},
		{name: "verify", group: groupKernelOps,
			usage:   "semantix verify --session <file|dir> [--holdout 0.3]",
			summary: "offline replay validation (exit 3 = gate not met)",
			run:     depsCommand(runVerify)},
		{name: "probe", group: groupKernelOps,
			usage:   "semantix probe --sessions <f1,f2,...> | --dir <dir> [--topk 5] [--t-step-split] [--json]",
			summary: "W0 cross-session hit-rate probe: ordered replay, whose slices serve later sessions",
			run:     errCommand("probe", runProbe)},
		{name: "eval", group: groupKernelOps,
			usage:   "semantix eval --set <oracle.tsv> [--tau-*]",
			summary: "retrieval strategy comparison (Issue #7)",
			run:     depsCommand(runEval)},
		{name: "eval-judge", group: groupKernelOps,
			usage:   "semantix eval-judge [--stub yes|no] [--judge-*] [--audit <tsv>]",
			summary: "LLM judge authenticity evaluation (Issue #8)",
			run:     intCommand(runEvalJudge)},
		{name: "calibrate", group: groupKernelOps,
			usage:   "semantix calibrate [--audit <oracle.tsv>] [--usage <usage.jsonl>] [--stub yes|no]",
			summary: "L3 negative-observability calibration report (Issue #262)",
			run:     intCommand(runCalibrate)},
		{name: "usage", group: groupKernelOps,
			usage:   "semantix usage [--db <usage.jsonl>] [--evolve-db <dir>]",
			summary: "cost-savings statistics (Issue #60)",
			run:     depsCommand(runUsage)},
		{name: "dashboard", group: groupKernelOps,
			usage:   "semantix dashboard [--db ...] [--usage ...] [--config ...] [--json]",
			summary: "ANSI reuse snapshot (U31, Issue #155)",
			run:     runDashboard},
		{name: "lookup", group: groupKernelOps,
			usage:   "semantix lookup --query <q> [--limit N] [--db ...]",
			summary: "single-query retrieval (harness tool backend)",
			run:     errCommand("lookup", runLookup)},
		{name: "inject", group: groupKernelOps,
			usage:   "semantix inject --query <q> [--budget N] [--db ...]",
			summary: "L2 injection block generation (harness backend)",
			run:     errCommand("inject", runInject)},
		{name: "doctor", group: groupProduct,
			usage:   "semantix doctor [--config <path>] [--db <path>] [--json]",
			summary: "health check (db / config / embedder / judge; exit 3 on any FAIL)",
			run: func(args []string, stdout, stderr io.Writer, _ dependencies) int {
				return runDoctor(args, stdout, stderr)
			}},
		{name: "install", group: groupProduct,
			usage:   "semantix install --target semantix-agent|claude-code|custom [--dir <path>] [--uninstall]",
			summary: "install agent-skill (skill + tool schema) into a harness",
			run: func(args []string, stdout, stderr io.Writer, _ dependencies) int {
				return runInstall(args, stdout, stderr)
			}},
		{name: "version", group: groupProduct,
			usage:   "semantix version [--json]",
			summary: "version + commit + build time",
			run: func(args []string, stdout, stderr io.Writer, _ dependencies) int {
				return runVersion(args, stdout, stderr)
			}},
		{name: "gc", group: groupMaintenance,
			usage:   "semantix gc [--retention-days N] [--min-weight W] [--max-slices M] [--no-rescore] [--no-archive] [--dry-run] [--json]",
			summary: "rescore weights, prune stale / low-weight slices, enforce the library cap",
			run:     errCommand("gc", runGC)},
		{name: "trust", group: groupMaintenance,
			usage:   "semantix trust <slice-id> [--origin user-curated] [--db <path>] [--audit-db <path>]",
			summary: "upgrade a slice's provenance tag (Issue #279, audit-logged)",
			run:     depsCommand(runTrust),
		},
		{name: "import", group: groupMaintenance,
			usage:   "semantix import --input <file.jsonl> [--trust] [--db <path>] [--audit-db <path>]",
			summary: "restore slices from JSONL, stamped as import (Issue #279)",
			run:     depsCommand(runImport),
		},
		{name: "export", group: groupMaintenance,
			usage:   "semantix export [--out <file.jsonl>] [--db <path>] [--scope project]",
			summary: "dump the library as JSONL (lossless import round-trip; embeddings intact)",
			run:     depsCommand(runExport),
		},
	}
}

func run(args []string, stdout, stderr io.Writer, deps dependencies) int {
	if len(args) == 0 {
		// Bare `semantix` (no subcommand): launch the co-located coding agent
		// with the current directory as its workspace. When no agent binary is
		// installed (kernel-only install), fall back to the command help so the
		// old behavior is preserved.
		if agent := findAgentBinary(); agent != "" {
			return launchAgent(agent)
		}
		printHelp(stderr)
		return 2
	}

	// M2-U20 config layer: resolve semantix.toml (flag > env > file > default)
	// once per invocation so every command's flag defaults come from config.
	// An invalid config file is a usage-class error (exit 2, §4.3); a missing
	// file is fine — built-in defaults apply.
	cfg, cfgErr := config.Load(config.Options{})
	if cfgErr != nil {
		var cerr *config.Error
		if errors.As(cfgErr, &cerr) && cerr.Kind == config.KindInvalid {
			fmt.Fprintf(stderr, "semantix: invalid config: %v\n", cfgErr)
			return 2
		}
		fmt.Fprintf(stderr, "semantix: config: %v\n", cfgErr)
		return 1
	}
	deps.resolved = cfg

	switch args[0] {
	case "help", "-h", "--help":
		return runHelp(args[1:], stdout, stderr)
	}
	cmd := findCommand(args[0])
	if cmd == nil {
		fmt.Fprintf(stderr, "semantix: unknown command %q\n\n", args[0])
		printHelp(stderr)
		return 2
	}
	return cmd.run(args[1:], stdout, stderr, deps)
}

// findCommand looks a command up in the command tree by name.
// reorderPositional moves non-flag arguments to the end of args so Go's
// flag package (which stops at the first non-flag argument) keeps parsing
// flags that follow a positional value, e.g. `trust <id> --db x`.
func reorderPositional(args []string) []string {
	var positional, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-" || a == "--" || strings.HasPrefix(a, "-") {
			rest = append(rest, a)
			// A flag's value is the next argument unless it also starts
			// with '-' or the flag used the --flag=value form.
			if !strings.Contains(a, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				rest = append(rest, args[i])
			}
		} else {
			positional = append(positional, a)
		}
	}
	return append(rest, positional...)
}

func findCommand(name string) *commandSpec {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

// runHelp implements `semantix help [command]`: with a command argument it
// prints that command's synopsis; otherwise the full grouped command tree.
func runHelp(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printHelp(stdout)
		return 0
	}
	cmd := findCommand(args[0])
	if cmd == nil {
		fmt.Fprintf(stderr, "semantix help: unknown command %q\n", args[0])
		return 2
	}
	fmt.Fprintf(stdout, "Usage: %s\n\n%s\n", cmd.usage, cmd.summary)
	return 0
}

// printHelp renders the command tree grouped by the four U19 groups. Groups
// without registered commands list their planned names (U21/U23-U27).
func printHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  semantix <command> [flags]")
	fmt.Fprintln(w, "  semantix help               list commands by group")
	fmt.Fprintln(w, "  semantix help <command>     show one command's synopsis")
	fmt.Fprintln(w, "  semantix <command> --help   show one command's flags")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Exit codes: 0 ok · 1 runtime error · 2 usage error · 3 gate not met")
	fmt.Fprintln(w)
	for g := groupKernelOps; g <= groupService; g++ {
		var inGroup []commandSpec
		for _, c := range commands {
			if c.group == g {
				inGroup = append(inGroup, c)
			}
		}
		if len(inGroup) == 0 {
			// A group with no commands and nothing planned is not shown at
			// all — no empty section headers in the minimized command tree.
			if planned := plannedByGroup[g]; len(planned) > 0 {
				fmt.Fprintf(w, "%s (planned: %s)\n", g, strings.Join(planned, " "))
				fmt.Fprintln(w)
			}
			continue
		}
		fmt.Fprintf(w, "%s\n", g)
		for _, c := range inGroup {
			fmt.Fprintf(w, "  %-11s %s\n", c.name, c.summary)
		}
		if planned := plannedByGroup[g]; len(planned) > 0 {
			fmt.Fprintf(w, "  (planned: %s)\n", strings.Join(planned, " "))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, `Run "semantix help <command>" or "semantix <command> --help" for details.`)
}

// errCommand adapts an error-returning command (extract/search/lookup/inject/
// export/import/gc) to the dispatch signature, mapping errors to the U19
// exit-code contract: 0 ok, 0 for --help, 2 usage error, 1 runtime error.
// When the invocation requested --json, the failure also emits the §4.2
// envelope (ok:false, error.code) so a JSON consumer stays parseable.
func errCommand(name string, fn func(args []string, stdout, stderr io.Writer, deps dependencies) error) func(args []string, stdout, stderr io.Writer, deps dependencies) int {
	return func(args []string, stdout, stderr io.Writer, deps dependencies) int {
		if err := fn(args, stdout, stderr, deps); err == nil {
			return 0
		} else if errors.Is(err, flag.ErrHelp) {
			return 0 // --help is a successful request, not a failure
		} else if isUsage(err) {
			if wantsJSON(args) {
				_ = writeErrorEnvelope(stdout, name, 2, err.Error())
			}
			fmt.Fprintf(stderr, "semantix %s: %v\n", name, err)
			return 2
		} else {
			fmt.Fprintf(stderr, "semantix %s: %v\n", name, err)
			return 1
		}
	}
}

// intCommand adapts commands that already return an exit code directly
// (eval-judge/usage) to the dispatch signature.
func intCommand(fn func(args []string, stdout io.Writer) int) func(args []string, stdout, stderr io.Writer, deps dependencies) int {
	return func(args []string, stdout, stderr io.Writer, deps dependencies) int {
		return fn(args, stdout)
	}
}

// depsCommand adapts commands that return an exit code and take deps but
// no stderr (verify/eval) to the dispatch signature. Their diagnostics
// keep going to stdout, as before U19.
func depsCommand(fn func(args []string, stdout io.Writer, deps dependencies) int) func(args []string, stdout, stderr io.Writer, deps dependencies) int {
	return func(args []string, stdout, stderr io.Writer, deps dependencies) int {
		return fn(args, stdout, deps)
	}
}

type storeCloser interface {
	Close() error
}

func closeStore(store slice.Store) {
	if closer, ok := store.(storeCloser); ok {
		_ = closer.Close()
	}
}
