package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"semantix/kernel/bm25"
	"semantix/kernel/config"
	"semantix/kernel/slice"
)

type dependencies struct {
	newExtractor func() slice.Extractor
	openStore    func(string) (slice.Store, error)
	newIndex     func() slice.Index
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

func run(args []string, stdout, stderr io.Writer, deps dependencies) int {
	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	var err error
	switch args[0] {
	case "extract":
		err = runExtract(args[1:], stdout, stderr, deps)
	case "search":
		err = runSearch(args[1:], stdout, stderr, deps)
	case "verify":
		return runVerify(args[1:], stdout, deps)
	case "eval":
		return runEval(args[1:], stdout)
	case "eval-judge":
		return runEvalJudge(args[1:], stdout)
	case "usage":
		return runUsage(args[1:], stdout)
	case "lookup":
		err = runLookup(args[1:], stdout, stderr, deps)
	case "inject":
		err = runInject(args[1:], stdout, stderr, deps)
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "semantix: unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
	if err != nil {
		// `--help` is a successful request, not a failure: the Go flag
		// package reports ErrHelp after printing usage for any subcommand.
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		if _, ok := config.IsError(err); ok {
			fmt.Fprintf(stderr, "semantix %s: %v\n", args[0], err)
			return 2
		}
		fmt.Fprintf(stderr, "semantix %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  semantix extract --input <session.jsonl> [--scope project|user|session]")
	fmt.Fprintln(w, "  semantix search [flags] <query>")
	fmt.Fprintln(w, "  semantix verify --session <file|dir> [--holdout 0.3]  (M0-2 replay validation)")
	fmt.Fprintln(w, "  semantix eval --set <oracle.tsv> [--tau-*]            (Issue #7 single-vs-three comparison)")
	fmt.Fprintln(w, "  semantix lookup --query <q> [--limit N] [--db ...]     (semantix_lookup tool)")
	fmt.Fprintln(w, "  semantix inject --query <q> [--budget N] [--db ...]    (L2 injection block)")
}

type storeCloser interface {
	Close() error
}

func closeStore(store slice.Store) {
	if closer, ok := store.(storeCloser); ok {
		_ = closer.Close()
	}
}
