package counter_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jainiltailor/wordcounter/internal/counter"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// writeTemp creates a temp file with the given content and returns its path.
// t.Cleanup ensures the file is removed after the test.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "wc_test_*.txt")
	if err != nil {
		t.Fatalf("writeTemp: %v", err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("writeTemp write: %v", err)
	}
	f.Close()
	return f.Name()
}

// ─── Unit tests ──────────────────────────────────────────────────────────────

func TestCount_SingleFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		wantWords int64
		wantLines int64
		wantChars int64
	}{
		{
			name:      "empty file",
			content:   "",
			wantWords: 0, wantLines: 0, wantChars: 0,
		},
		{
			name:      "single word",
			content:   "hello\n",
			wantWords: 1, wantLines: 1, wantChars: 6, // 5 chars + newline
		},
		{
			name:      "multiple words on one line",
			content:   "the quick brown fox\n",
			wantWords: 4, wantLines: 1, wantChars: 20,
		},
		{
			name:      "multiple lines",
			content:   "foo bar\nbaz qux quux\n",
			wantWords: 5, wantLines: 2, wantChars: 21,
		},
		{
			name:      "extra whitespace between words",
			content:   "  hello   world  \n",
			wantWords: 2, wantLines: 1,
		},
		{
			name:      "tabs as whitespace",
			content:   "one\ttwo\tthree\n",
			wantWords: 3, wantLines: 1,
		},
		{
			name:      "blank lines don't add words",
			content:   "hello\n\n\nworld\n",
			wantWords: 2, wantLines: 4,
		},
		{
			name:      "unicode content",
			content:   "नमस्ते दुनिया\n", // Hindi "hello world"
			wantWords: 2, wantLines: 1,
		},
		{
			name:      "only whitespace",
			content:   "   \n\t\n  \n",
			wantWords: 0, wantLines: 3,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			path := writeTemp(t, tc.content)
			stats, err := counter.Count(context.Background(), []string{path}, counter.Options{Workers: 1})

			if err != nil {
				t.Fatalf("Count returned error: %v", err)
			}
			if len(stats.Files) != 1 {
				t.Fatalf("expected 1 result, got %d", len(stats.Files))
			}

			res := stats.Files[0]
			if res.Error != nil {
				t.Fatalf("file error: %v", res.Error)
			}
			if res.Words != tc.wantWords {
				t.Errorf("Words: got %d, want %d", res.Words, tc.wantWords)
			}
			if res.Lines != tc.wantLines {
				t.Errorf("Lines: got %d, want %d", res.Lines, tc.wantLines)
			}
			if tc.wantChars > 0 && res.Chars != tc.wantChars {
				t.Errorf("Chars: got %d, want %d", res.Chars, tc.wantChars)
			}
		})
	}
}

func TestCount_MultipleFiles_Parallel(t *testing.T) {
	t.Parallel()

	// Create 20 files with known content — test that parallel processing
	// produces correct aggregated totals and preserves order.
	const numFiles = 20
	files := make([]string, numFiles)
	var totalWords int64

	for i := 0; i < numFiles; i++ {
		words := i + 1                              // file i has i+1 words
		content := strings.Repeat("word ", words) + "\n"
		files[i] = writeTemp(t, content)
		totalWords += int64(words)
	}

	stats, err := counter.Count(context.Background(), files, counter.Options{Workers: 4})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Total word count must match
	if stats.Total.Words != totalWords {
		t.Errorf("total words: got %d, want %d", stats.Total.Words, totalWords)
	}

	// Results must be in INPUT order (not completion order)
	for i, res := range stats.Files {
		want := int64(i + 1)
		if res.Words != want {
			t.Errorf("files[%d]: got %d words, want %d", i, res.Words, want)
		}
	}

	// Worker count recorded correctly
	if stats.Workers != 4 {
		t.Errorf("workers: got %d, want 4", stats.Workers)
	}
}

func TestCount_MissingFile_ReportedAsError(t *testing.T) {
	t.Parallel()

	good := writeTemp(t, "hello world\n")
	bad := filepath.Join(t.TempDir(), "does_not_exist.txt")

	stats, err := counter.Count(context.Background(), []string{good, bad}, counter.Options{Workers: 2})

	// Count itself should not error — file errors are embedded in Stats
	if err != nil {
		t.Fatalf("unexpected top-level error: %v", err)
	}
	if len(stats.Errors) != 1 {
		t.Fatalf("expected 1 error result, got %d", len(stats.Errors))
	}
	if stats.Errors[0].Error == nil {
		t.Error("expected non-nil error for missing file")
	}
	// The good file still counted
	if stats.Total.Words != 2 {
		t.Errorf("total words: got %d, want 2", stats.Total.Words)
	}
}

func TestCount_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Create many files so cancellation has something to interrupt.
	files := make([]string, 50)
	for i := range files {
		files[i] = writeTemp(t, strings.Repeat("word ", 100)+"\n")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := counter.Count(ctx, files, counter.Options{Workers: 4})
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

// BenchmarkCount_Workers measures throughput as worker count scales.
// Run with: go test -bench=. -benchmem -benchtime=3s ./internal/counter/
func BenchmarkCount_Workers(b *testing.B) {
	// Create a 50KB file (realistic small source file size).
	content := strings.Repeat("the quick brown fox jumps over the lazy dog\n", 1000)
	dir := b.TempDir()

	// Create 20 files to give the pool real work.
	files := make([]string, 20)
	for i := range files {
		path := filepath.Join(dir, fmt.Sprintf("file%d.txt", i))
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			b.Fatal(err)
		}
		files[i] = path
	}

	workerCounts := []int{1, 2, 4, 8}
	for _, w := range workerCounts {
		w := w
		b.Run(fmt.Sprintf("workers=%d", w), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := counter.Count(context.Background(), files, counter.Options{Workers: w})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkCount_FileSize measures memory and time vs file size.
func BenchmarkCount_FileSize(b *testing.B) {
	sizes := []struct {
		name  string
		lines int
	}{
		{"1KB", 20},
		{"100KB", 2000},
		{"1MB", 20000},
	}

	for _, sz := range sizes {
		sz := sz
		b.Run(sz.name, func(b *testing.B) {
			content := strings.Repeat("the quick brown fox jumps\n", sz.lines)
			path := filepath.Join(b.TempDir(), "bench.txt")
			if err := os.WriteFile(path, []byte(content), 0644); err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := counter.Count(context.Background(), []string{path}, counter.Options{Workers: 1})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
