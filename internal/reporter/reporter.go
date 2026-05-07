// Package reporter formats counter.Stats into human-readable or
// machine-parseable output. Supported formats: table (default), json, csv.
package reporter

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/jainiltailor/wordcounter/internal/counter"
)

// Format is the output format selector.
type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatCSV   Format = "csv"
)

// Options configures report generation.
type Options struct {
	Format    Format
	SortBy    string // "words", "lines", "chars", "bytes", "name" — default: input order
	Descending bool
	ShowBytes  bool
	NoColor    bool
	Out        io.Writer // defaults to os.Stdout
}

func (o *Options) out() io.Writer {
	if o.Out != nil {
		return o.Out
	}
	return os.Stdout
}

// ─── ANSI colour helpers ─────────────────────────────────────────────────────

const (
	colReset  = "\033[0m"
	colBold   = "\033[1m"
	colCyan   = "\033[36m"
	colGreen  = "\033[32m"
	colYellow = "\033[33m"
	colRed    = "\033[31m"
	colGray   = "\033[90m"
)

func (o *Options) c(code string) string {
	if o.NoColor {
		return ""
	}
	return code
}

// ─── Public API ──────────────────────────────────────────────────────────────

// Print writes the formatted report to o.Out (defaults to os.Stdout).
func Print(stats *counter.Stats, opts Options) error {
	files := applySort(stats.Files, opts)

	switch opts.Format {
	case FormatJSON:
		return printJSON(files, stats, opts)
	case FormatCSV:
		return printCSV(files, stats, opts)
	default:
		return printTable(files, stats, opts)
	}
}

// ─── Table format ────────────────────────────────────────────────────────────

func printTable(files []*counter.FileResult, stats *counter.Stats, opts Options) error {
	w := tabwriter.NewWriter(opts.out(), 0, 0, 2, ' ', 0)

	// Header
	header := fmt.Sprintf("%s%sLINES\tWORDS\tCHARS%s",
		opts.c(colBold), opts.c(colCyan),
		opts.c(colReset),
	)
	if opts.ShowBytes {
		header += fmt.Sprintf("%s\tBYTES%s", opts.c(colBold+colCyan), opts.c(colReset))
	}
	header += "\tFILE"
	fmt.Fprintln(w, header)
	fmt.Fprintln(w, strings.Repeat("─", 6)+"\t"+strings.Repeat("─", 6)+"\t"+strings.Repeat("─", 6)+"\t"+"────────────────────────────")

	for _, f := range files {
		if f.Error != nil {
			fmt.Fprintf(w, "%s%s\t%s\t%s\t%s%s\n",
				opts.c(colRed), "ERR", "ERR", "ERR",
				counter.ShortPath(f.Path)+": "+f.Error.Error(),
				opts.c(colReset),
			)
			continue
		}
		line := fmt.Sprintf("%s%d\t%d\t%d%s",
			opts.c(colGray),
			f.Lines, f.Words, f.Chars,
			opts.c(colReset),
		)
		if opts.ShowBytes {
			line += fmt.Sprintf("\t%d", f.Bytes)
		}
		line += "\t" + opts.c(colGreen) + counter.ShortPath(f.Path) + opts.c(colReset)
		fmt.Fprintln(w, line)
	}

	// Separator + total
	if len(files) > 1 {
		fmt.Fprintln(w, strings.Repeat("─", 6)+"\t"+strings.Repeat("─", 6)+"\t"+strings.Repeat("─", 6)+"\t"+"────────────────────────────")
		total := fmt.Sprintf("%s%s%d\t%d\t%d%s",
			opts.c(colBold), opts.c(colYellow),
			stats.Total.Lines, stats.Total.Words, stats.Total.Chars,
			opts.c(colReset),
		)
		if opts.ShowBytes {
			total += fmt.Sprintf("\t%d", stats.Total.Bytes)
		}
		total += fmt.Sprintf("\t%sTOTAL (%d files, %d workers)%s",
			opts.c(colBold), len(files), stats.Workers, opts.c(colReset))
		fmt.Fprintln(w, total)
	}

	// Errors summary
	if len(stats.Errors) > 0 {
		fmt.Fprintf(opts.out(), "\n%s%d file(s) could not be read:%s\n",
			opts.c(colRed), len(stats.Errors), opts.c(colReset))
		for _, e := range stats.Errors {
			fmt.Fprintf(opts.out(), "  • %s: %v\n", counter.ShortPath(e.Path), e.Error)
		}
	}

	return w.Flush()
}

// ─── JSON format ─────────────────────────────────────────────────────────────

type jsonFile struct {
	Path  string `json:"path"`
	Lines int64  `json:"lines"`
	Words int64  `json:"words"`
	Chars int64  `json:"chars"`
	Bytes int64  `json:"bytes,omitempty"`
	Error string `json:"error,omitempty"`
}

type jsonOutput struct {
	Files   []jsonFile `json:"files"`
	Total   jsonFile   `json:"total"`
	Workers int        `json:"workers"`
	Errors  int        `json:"errors"`
}

func printJSON(files []*counter.FileResult, stats *counter.Stats, opts Options) error {
	out := jsonOutput{Workers: stats.Workers, Errors: len(stats.Errors)}
	for _, f := range files {
		jf := jsonFile{
			Path:  counter.ShortPath(f.Path),
			Lines: f.Lines, Words: f.Words, Chars: f.Chars,
		}
		if opts.ShowBytes {
			jf.Bytes = f.Bytes
		}
		if f.Error != nil {
			jf.Error = f.Error.Error()
		}
		out.Files = append(out.Files, jf)
	}
	out.Total = jsonFile{
		Path:  "TOTAL",
		Lines: stats.Total.Lines,
		Words: stats.Total.Words,
		Chars: stats.Total.Chars,
		Bytes: stats.Total.Bytes,
	}
	enc := json.NewEncoder(opts.out())
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// ─── CSV format ──────────────────────────────────────────────────────────────

func printCSV(files []*counter.FileResult, stats *counter.Stats, opts Options) error {
	out := opts.out()
	header := "path,lines,words,chars"
	if opts.ShowBytes {
		header += ",bytes"
	}
	fmt.Fprintln(out, header)

	emit := func(f *counter.FileResult) {
		row := fmt.Sprintf("%q,%d,%d,%d",
			counter.ShortPath(f.Path), f.Lines, f.Words, f.Chars)
		if opts.ShowBytes {
			row += fmt.Sprintf(",%d", f.Bytes)
		}
		fmt.Fprintln(out, row)
	}

	for _, f := range files {
		if f.Error == nil {
			emit(f)
		}
	}
	// Emit total row
	total := stats.Total
	total.Path = "TOTAL"
	emit(&total)
	return nil
}

// ─── Sort helpers ─────────────────────────────────────────────────────────────

func applySort(files []*counter.FileResult, opts Options) []*counter.FileResult {
	if opts.SortBy == "" || opts.SortBy == "name" && !opts.Descending {
		return files // already in input / name order by default
	}

	sorted := make([]*counter.FileResult, len(files))
	copy(sorted, files)

	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		var less bool
		switch opts.SortBy {
		case "words":
			less = a.Words < b.Words
		case "lines":
			less = a.Lines < b.Lines
		case "chars":
			less = a.Chars < b.Chars
		case "bytes":
			less = a.Bytes < b.Bytes
		case "name":
			less = counter.ShortPath(a.Path) < counter.ShortPath(b.Path)
		default:
			less = a.Words < b.Words
		}
		if opts.Descending {
			return !less
		}
		return less
	})
	return sorted
}
