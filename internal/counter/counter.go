// Package counter implements the parallel file word-counting engine.
//
// Architecture:
//
//	Dispatcher ──▶ [job channel] ──▶ Worker 1 ──▶ [result channel] ──┐
//	                              ──▶ Worker 2 ──▶ [result channel] ──┼──▶ Aggregator ──▶ Stats
//	                              ──▶ Worker N ──▶ [result channel] ──┘
//
// Each worker is a goroutine that reads a file path from the job channel,
// counts words/lines/chars in a single pass using a streaming scanner
// (no full-file load into memory), and sends a Result to the result channel.
// The aggregator collects results and builds the final Stats.
package counter

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// ─── Data types ────────────────────────────────────────────────────────────

// FileResult holds all counts for a single file. Errors are embedded rather
// than surfaced as fatal so that one bad file does not abort the whole run.
type FileResult struct {
	Path  string
	Words int64
	Lines int64
	Chars int64 // Unicode code points (runes), not bytes
	Bytes int64
	Error error  // non-nil means this file was unreadable
}

// Stats is the aggregated output across all files.
type Stats struct {
	Files   []*FileResult // per-file results, order matches input order
	Total   FileResult    // sum across all successful files
	Errors  []*FileResult // files that could not be processed
	Workers int           // how many workers ran
}

// ─── Options ────────────────────────────────────────────────────────────────

// Options configures the parallel counter.
type Options struct {
	// Workers is the number of parallel goroutines.
	// Defaults to 4 if zero or negative.
	Workers int
}

func (o Options) workers() int {
	if o.Workers <= 0 {
		return 4
	}
	return o.Workers
}

// ─── Public API ─────────────────────────────────────────────────────────────

// Count processes all files in parallel and returns aggregated Stats.
// It respects context cancellation: if ctx is cancelled, in-flight workers
// finish their current file and then exit; no new files are dispatched.
//
// Count never returns a non-nil error for individual file failures; those are
// recorded in Stats.Errors. It only returns an error if ctx was cancelled.
func Count(ctx context.Context, files []string, opts Options) (*Stats, error) {
	numWorkers := opts.workers()

	// Buffered channels keep workers busy without blocking the dispatcher.
	jobCh := make(chan string, numWorkers*2)
	resultCh := make(chan *FileResult, numWorkers*2)

	// ── Dispatcher: feeds file paths into jobCh ──────────────────────────
	go func() {
		defer close(jobCh) // signals workers to exit after draining
		for _, path := range files {
			select {
			case <-ctx.Done():
				return // context cancelled — stop dispatching
			case jobCh <- path:
			}
		}
	}()

	// ── Workers: each reads from jobCh, processes a file, sends result ───
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobCh { // exits when jobCh is closed
				select {
				case <-ctx.Done():
					return
				default:
					resultCh <- processFile(path)
				}
			}
		}()
	}

	// ── Closer: waits for all workers then closes result channel ─────────
	go func() {
		wg.Wait()
		close(resultCh) // signals aggregator to stop
	}()

	// ── Aggregator: collects results preserving input order ───────────────
	// Build an index so we can insert results in original order even though
	// workers finish in non-deterministic order.
	indexMap := make(map[string]int, len(files))
	for i, f := range files {
		indexMap[f] = i
	}

	ordered := make([]*FileResult, len(files))
	var errs []*FileResult
	var total FileResult

	for res := range resultCh {
		// Place result at original position.
		if idx, ok := indexMap[res.Path]; ok {
			ordered[idx] = res
		}

		if res.Error != nil {
			errs = append(errs, res)
			continue
		}
		total.Words += res.Words
		total.Lines += res.Lines
		total.Chars += res.Chars
		total.Bytes += res.Bytes
	}

	// Filter out any nils (shouldn't happen but be safe).
	var results []*FileResult
	for _, r := range ordered {
		if r != nil {
			results = append(results, r)
		}
	}

	stats := &Stats{
		Files:   results,
		Total:   total,
		Errors:  errs,
		Workers: numWorkers,
	}

	if ctx.Err() != nil {
		return stats, ctx.Err()
	}
	return stats, nil
}

// ─── File processing ────────────────────────────────────────────────────────

// processFile opens, scans, and counts a single file in one streaming pass.
// Memory usage is O(line_length), not O(file_size) — safe for huge files.
func processFile(path string) *FileResult {
	res := &FileResult{Path: path}

	f, err := os.Open(path)
	if err != nil {
		res.Error = err
		return res
	}
	defer f.Close()

	// Single-pass streaming scan: one line at a time.
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024) // 1 MB max line

	for scanner.Scan() {
		line := scanner.Text()
		res.Lines++
		res.Bytes += int64(len(scanner.Bytes())) + 1 // +1 for newline
		res.Chars += int64(utf8.RuneCountInString(line)) + 1
		res.Words += int64(countWords(line))
	}

	if err := scanner.Err(); err != nil {
		res.Error = err
	}

	return res
}

// countWords counts words in a single line using the same definition as wc(1):
// sequences of characters separated by Unicode whitespace.
// Using strings.Fields is clean but allocates; we use a state machine instead
// to keep allocations at zero per line.
func countWords(line string) int {
	count := 0
	inWord := false
	for _, r := range line {
		if unicode.IsSpace(r) {
			inWord = false
		} else {
			if !inWord {
				count++
			}
			inWord = true
		}
	}
	return count
}

// ─── Utility helpers used by reporter ───────────────────────────────────────

// ShortPath returns a display-friendly path: relative to cwd if possible,
// otherwise the absolute path. Never returns an error.
func ShortPath(abs string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return abs
	}
	rel, err := filepath.Rel(cwd, abs)
	if err != nil {
		return abs
	}
	// Avoid "../../../deep/path" — use abs if relative is longer.
	if len(rel) > len(abs) || strings.HasPrefix(rel, "../../..") {
		return abs
	}
	return rel
}
