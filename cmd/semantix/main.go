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
	groupKernelOps commandGroup = iota // kernel 运维（U19：现有 8 命令，行为不变）
	groupProduct                       // 产品与管理（U21/U23/U24/U25 挂载）
	groupMaintenance                   // 维护（U26 挂载）
	groupService                       // 服务模式（U27 挂载）
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

// plannedByGroup lists the commands planned for groups that U19 does not
// implement yet. They render in help as planned and reject dispatch with
// exit 2 until their unit lands.
var plannedByGroup = map[commandGroup][]string{
	groupMaintenance: {},
	groupService:     {"serve", "watch"},
}

// commandSpec registers one CLI command in the command tree. All commands
// share one dispatch signature; error-returning commands are adapted with
// errCommand so the exit-code contract is enforced in one place.
type commandSpec struct {
	name    string
	group   commandGroup
	usage   string // one-line invocation synopsis (help)
	summary string // one-line description (help)
	run     func(args []string, stdout, stderr io.Writer, deps dependencies) int

	// completionFlags lists the flags offered by shell completion (U23,
	// Issue #117). Keep it in sync with the command's FlagSet;
	// TestCompletionFlagsMatchCommandHelp fails on drift.
	completionFlags []string
	// flagValues maps flags whose values come from a closed set (e.g.
	// --scope) to the candidates shell completion offers for them.
	flagValues map[string][]string
}

// completionFlagSets are the closed-set flag values shared by several
// commands (U23, Issue #117); they are offered by shell completion.
var (
	scopeValues     = []string{"session", "project", "user"}
	embedderValues  = []string{"hash", "model"}
	retrieverValues = []string{"bm25", "vector", "hybrid"}
	protocolValues  = []string{"openai", "anthropic"}
)

// commands is the command tree, in help order within each group. It is
// populated in init() rather than via a variable initializer: the completion
// generators (U23) read commands, and buildCommands references them, so a
// direct initializer would create an init dependency cycle.
var commands []commandSpec

func init() {
	commands = buildCommands()
}

func buildCommands() []commandSpec {
	return []commandSpec{
	{name: "extract", group: groupKernelOps,
		usage:   "semantix extract --input <session.jsonl> [--scope project|user|session]",
		summary: "extract session JSONL into semantic slices",
		run:     errCommand("extract", runExtract),
		completionFlags: []string{"--input", "--scope", "--db", "--project-db", "--user-db",
			"--session", "--project", "--task-type", "--language", "--fingerprint", "--l3-safe", "--embedder"},
		flagValues: map[string][]string{"--scope": scopeValues, "--embedder": embedderValues}},
	{name: "search", group: groupKernelOps,
		usage:   "semantix search [flags] <query>",
		summary: "semantic retrieval (bm25 / vector / hybrid)",
		run:     errCommand("search", runSearch),
		completionFlags: []string{"--query", "--scope", "--limit", "--db", "--project-db", "--user-db",
			"--json", "--retriever", "--embedder", "--tau-high", "--tau-low", "--abs-high", "--abs-low"},
		flagValues: map[string][]string{"--scope": scopeValues, "--retriever": retrieverValues, "--embedder": embedderValues}},
	{name: "verify", group: groupKernelOps,
		usage:   "semantix verify --session <file|dir> [--holdout 0.3]",
		summary: "offline replay validation (exit 3 = gate not met)",
		run:     depsCommand(runVerify),
		completionFlags: []string{"--session", "--db", "--project", "--holdout", "--scope", "--grey-target",
			"--strict", "--tau-high", "--tau-low", "--abs-high", "--abs-low",
			"--judge-protocol", "--judge-base-url", "--judge-model"},
		flagValues: map[string][]string{"--scope": scopeValues, "--judge-protocol": protocolValues}},
	{name: "eval", group: groupKernelOps,
		usage:   "semantix eval --set <oracle.tsv> [--tau-*]",
		summary: "retrieval strategy comparison (Issue #7)",
		run:     intCommand(runEval),
		completionFlags: []string{"--set", "--train-frac", "--scope", "--tau-high", "--tau-low",
			"--abs-high", "--abs-low", "--single-tau", "--grey-target", "--strict", "--json"},
		flagValues: map[string][]string{"--scope": scopeValues}},
	{name: "eval-judge", group: groupKernelOps,
		usage:   "semantix eval-judge [--stub yes|no] [--judge-*] [--audit <tsv>]",
		summary: "LLM judge authenticity evaluation (Issue #8)",
		run:     intCommand(runEvalJudge),
		completionFlags: []string{"--audit", "--stub", "--judge-base-url", "--judge-model",
			"--judge-protocol", "--p-prom", "--min-consistency", "--json"},
		flagValues: map[string][]string{"--stub": {"yes", "no", "error"}, "--judge-protocol": protocolValues}},
	{name: "usage", group: groupKernelOps,
		usage:   "semantix usage [--db <usage.jsonl>] [--evolve-db <dir>]",
		summary: "cost-savings statistics (Issue #60)",
		run:     depsCommand(runUsage),
		completionFlags: []string{"--db", "--cost-miss", "--cost-hit", "--evolve-db"}},
	{name: "dashboard", group: groupKernelOps,
		usage:   "semantix dashboard [--db ...] [--usage ...] [--config ...] [--json]",
		summary: "ANSI reuse snapshot (U31, Issue #155)",
		run:     runDashboard,
		completionFlags: []string{"--db", "--usage", "--config", "--json", "--no-color", "--width"}},
	{name: "lookup", group: groupKernelOps,
		usage:   "semantix lookup --query <q> [--limit N] [--db ...]",
		summary: "single-query retrieval (harness tool backend)",
		run:     errCommand("lookup", runLookup),
		completionFlags: []string{"--query", "--scope", "--limit", "--db", "--json",
			"--tau-high", "--tau-low", "--abs-high", "--abs-low"},
		flagValues:      map[string][]string{"--scope": scopeValues}},
	{name: "inject", group: groupKernelOps,
		usage:   "semantix inject --query <q> [--budget N] [--db ...]",
		summary: "L2 injection block generation (harness backend)",
		run:     errCommand("inject", runInject),
		completionFlags: []string{"--query", "--scope", "--k", "--budget", "--db",
			"--tau-high", "--tau-low", "--abs-high", "--abs-low"},
		flagValues:      map[string][]string{"--scope": scopeValues}},
	{name: "doctor", group: groupProduct,
		usage:   "semantix doctor [--config <path>] [--db <path>] [--json]",
		summary: "health check (db / config / embedder / judge; exit 3 on any FAIL)",
		run: func(args []string, stdout, stderr io.Writer, _ dependencies) int {
			return runDoctor(args, stdout, stderr)
		},
		completionFlags: []string{"--config", "--db", "--json"}},
	{name: "completion", group: groupProduct,
		usage:   "semantix completion bash|zsh|fish",
		summary: "print shell completion script (bash / zsh / fish)",
		run: func(args []string, stdout, stderr io.Writer, _ dependencies) int {
			return runCompletion(args, stdout, stderr)
		}},
	{name: "install", group: groupProduct,
		usage:   "semantix install --target reasonix|claude-code|custom [--dir <path>] [--uninstall]",
		summary: "install agent-skill (skill + tool schema) into a harness",
		run: func(args []string, stdout, stderr io.Writer, _ dependencies) int {
			return runInstall(args, stdout, stderr)
		}},
	{name: "init", group: groupProduct,
		usage:   "semantix init [--config <path>] [--force]",
		summary: "generate semantix.toml + .semantix/ skeleton",
		run: func(args []string, stdout, stderr io.Writer, _ dependencies) int {
			return runInit(args, stdout, stderr)
		},
		completionFlags: []string{"--config", "--force"}},
	{name: "config", group: groupProduct,
		usage:   "semantix config [--config <path>] [--db <path>] [--json]",
		summary: "print effective config with per-key source annotation",
		run: func(args []string, stdout, stderr io.Writer, _ dependencies) int {
			return runConfig(args, stdout, stderr)
		},
		completionFlags: []string{"--config", "--db", "--json"}},
	{name: "version", group: groupProduct,
		usage:   "semantix version [--json]",
		summary: "version + commit + build time",
		run: func(args []string, stdout, stderr io.Writer, _ dependencies) int {
			return runVersion(args, stdout, stderr)
		},
		completionFlags: []string{"--json"}},
	{name: "export", group: groupMaintenance,
		usage:   "semantix export --output <file> [--db ...] [--json]",
		summary: "JSONL backup of the slice store (incl. Meta)",
		run:     errCommand("export", runExport),
		completionFlags: []string{"--output", "--db", "--json"}},
	{name: "import", group: groupMaintenance,
		usage:   "semantix import --input <file> [--db ...] [--json]",
		summary: "restore the slice store from an export",
		run:     errCommand("import", runImport),
		completionFlags: []string{"--input", "--db", "--json"}},
	{name: "gc", group: groupMaintenance,
		usage:   "semantix gc [--retention-days N] [--min-weight W] [--max-slices M] [--no-rescore] [--no-archive] [--dry-run] [--json]",
		summary: "rescore weights, prune stale / low-weight slices, enforce the library cap",
		run:     errCommand("gc", runGC),
		completionFlags: []string{"--retention-days", "--min-weight", "--max-slices", "--no-rescore", "--no-archive", "--dry-run", "--json"}},
	}
}

func run(args []string, stdout, stderr io.Writer, deps dependencies) int {
	if len(args) == 0 {
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
			if planned := plannedByGroup[g]; len(planned) > 0 {
				fmt.Fprintf(w, "%s (planned: %s)\n", g, strings.Join(planned, " "))
			} else {
				fmt.Fprintf(w, "%s\n", g)
			}
			fmt.Fprintln(w)
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
