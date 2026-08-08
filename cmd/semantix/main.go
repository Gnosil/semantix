package main

import (
	"fmt"
	"io"
	"os"

	"semantix/kernel/bm25"
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
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "semantix: unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
	if err != nil {
		fmt.Fprintf(stderr, "semantix %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  semantix extract --input <session.jsonl> [--scope project|user|session]")
	fmt.Fprintln(w, "  semantix search [flags] <query>")
}

type storeCloser interface {
	Close() error
}

func closeStore(store slice.Store) {
	if closer, ok := store.(storeCloser); ok {
		_ = closer.Close()
	}
}
