// wordcounter — parallel file word counting CLI
//
// Usage:
//
//	wordcounter [flags] <path|glob> [<path|glob> ...]
//
// Examples:
//
//	wordcounter *.go
//	wordcounter -r -include="*.go" ./...
//	wordcounter -w 8 -format=json -sort=words -desc ./src
//	wordcounter -bytes file1.txt file2.txt
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"

	"github.com/example/wordcounter/internal/counter"
	"github.com/example/wordcounter/internal/reporter"
	"github.com/example/wordcounter/internal/walker"
)

// ─── CLI flags ────────────────────────────────────────────────────────────────

type config struct {
	workers    int
	recursive  bool
	include    string // comma-separated globs
	exclude    string // comma-separated globs
	format     string
	sortBy     string
	descending bool
	showBytes  bool
	noColor    bool
	version    bool
}

const version = "1.0.0"

func parseFlags() (config, []string) {
	cfg := config{}

	flag.IntVar(&cfg.workers, "w", runtime.NumCPU(),
		"number of parallel worker goroutines (default: number of CPU cores)")
	flag.BoolVar(&cfg.recursive, "r", false,
		"recursively traverse directories")
	flag.StringVar(&cfg.include, "include", "",
		"comma-separated glob patterns to include (e.g. '*.go,*.md')")
	flag.StringVar(&cfg.exclude, "exclude", "",
		"comma-separated glob patterns to exclude (e.g. '*.pb.go,*_test.go')")
	flag.StringVar(&cfg.format, "format", "table",
		"output format: table | json | csv")
	flag.StringVar(&cfg.sortBy, "sort", "",
		"sort results by: words | lines | chars | bytes | name")
	flag.BoolVar(&cfg.descending, "desc", false,
		"sort in descending order")
	flag.BoolVar(&cfg.showBytes, "bytes", false,
		"show byte counts in output")
	flag.BoolVar(&cfg.noColor, "no-color", false,
		"disable ANSI colour output")
	flag.BoolVar(&cfg.version, "version", false,
		"print version and exit")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `wordcounter v%s — parallel file word counter

Usage:
  wordcounter [flags] <path> [<path> ...]

Flags:
`, version)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Examples:
  wordcounter *.txt
  wordcounter -r -include="*.go" ./
  wordcounter -w 8 -format=json -sort=words -desc ./src
  wordcounter -bytes -no-color report.md > out.txt
`)
	}

	flag.Parse()
	return cfg, flag.Args()
}

// ─── Main ─────────────────────────────────────────────────────────────────────

func main() {
	cfg, paths := parseFlags()

	if cfg.version {
		fmt.Printf("wordcounter v%s (go%s, %s/%s)\n",
			version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	if len(paths) == 0 {
		fmt.Fprintln(os.Stderr, "error: no paths provided")
		flag.Usage()
		os.Exit(1)
	}

	// ── Context with graceful cancellation on SIGINT/SIGTERM ─────────────
	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// ── Discover files ─────────────────────────────────────────────────────
	walkOpts := walker.Options{
		Recursive: cfg.recursive,
		Include:   splitGlobs(cfg.include),
		Exclude:   splitGlobs(cfg.exclude),
	}

	files, err := walker.Walk(paths, walkOpts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error walking paths: %v\n", err)
		os.Exit(1)
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "error: no matching files found")
		os.Exit(1)
	}

	// ── Count words in parallel ────────────────────────────────────────────
	stats, err := counter.Count(ctx, files, counter.Options{
		Workers: cfg.workers,
	})

	// ── Report results (even if ctx cancelled — partial results are useful)
	repOpts := reporter.Options{
		Format:     reporter.Format(cfg.format),
		SortBy:     cfg.sortBy,
		Descending: cfg.descending,
		ShowBytes:  cfg.showBytes,
		NoColor:    cfg.noColor || !isTTY(),
	}

	if printErr := reporter.Print(stats, repOpts); printErr != nil {
		fmt.Fprintf(os.Stderr, "output error: %v\n", printErr)
		os.Exit(1)
	}

	// Exit with error code if context was cancelled or any file failed.
	if err != nil {
		os.Exit(2) // partial results, interrupted
	}
	if len(stats.Errors) > 0 {
		os.Exit(3) // completed but some files failed
	}
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// splitGlobs splits a comma-separated glob string into individual patterns,
// trimming whitespace and discarding empty entries.
func splitGlobs(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// isTTY returns true if stdout is a terminal (colour is appropriate).
// Falls back to false on any error (e.g. piped output).
func isTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}
