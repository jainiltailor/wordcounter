package walker_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/example/wordcounter/internal/walker"
)

// createTree builds a temp directory tree with the given structure.
// pathMap maps relative path → content. Directories are created automatically.
func createTree(t *testing.T, pathMap map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range pathMap {
		abs := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func sortedNames(paths []string) []string {
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Base(p)
	}
	sort.Strings(names)
	return names
}

func TestWalk_SingleFile(t *testing.T) {
	t.Parallel()
	root := createTree(t, map[string]string{"a.txt": "hello"})
	files, err := walker.Walk([]string{filepath.Join(root, "a.txt")}, walker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
}

func TestWalk_DirectoryNonRecursive(t *testing.T) {
	t.Parallel()
	root := createTree(t, map[string]string{
		"a.txt":       "aa",
		"b.txt":       "bb",
		"sub/c.txt":   "cc", // should NOT appear
	})
	files, err := walker.Walk([]string{root}, walker.Options{Recursive: false})
	if err != nil {
		t.Fatal(err)
	}
	names := sortedNames(files)
	if len(names) != 2 || names[0] != "a.txt" || names[1] != "b.txt" {
		t.Errorf("got %v, want [a.txt b.txt]", names)
	}
}

func TestWalk_DirectoryRecursive(t *testing.T) {
	t.Parallel()
	root := createTree(t, map[string]string{
		"a.txt":         "aa",
		"sub/b.txt":     "bb",
		"sub/sub2/c.txt":"cc",
	})
	files, err := walker.Walk([]string{root}, walker.Options{Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	names := sortedNames(files)
	if len(names) != 3 {
		t.Errorf("got %v, want 3 files", names)
	}
}

func TestWalk_IncludeFilter(t *testing.T) {
	t.Parallel()
	root := createTree(t, map[string]string{
		"main.go":  "go code",
		"notes.md": "markdown",
		"data.csv": "csv data",
	})
	files, err := walker.Walk([]string{root}, walker.Options{
		Include: []string{"*.go", "*.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	names := sortedNames(files)
	if len(names) != 2 {
		t.Errorf("got %v, want [main.go notes.md]", names)
	}
}

func TestWalk_ExcludeFilter(t *testing.T) {
	t.Parallel()
	root := createTree(t, map[string]string{
		"main.go":      "go",
		"main_test.go": "test",
		"util.go":      "util",
	})
	files, err := walker.Walk([]string{root}, walker.Options{
		Exclude: []string{"*_test.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	names := sortedNames(files)
	if len(names) != 2 {
		t.Errorf("got %v, want 2 non-test files", names)
	}
	for _, n := range names {
		if filepath.Ext(n) == "" || n == "main_test.go" {
			t.Errorf("unexpected file: %s", n)
		}
	}
}

func TestWalk_DeduplicatesPaths(t *testing.T) {
	t.Parallel()
	root := createTree(t, map[string]string{"a.txt": "aa"})
	path := filepath.Join(root, "a.txt")
	// Pass the same file twice.
	files, err := walker.Walk([]string{path, path}, walker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Errorf("expected deduplication: got %d files, want 1", len(files))
	}
}

func TestWalk_MissingPathIsSkipped(t *testing.T) {
	t.Parallel()
	real := createTree(t, map[string]string{"ok.txt": "ok"})
	files, err := walker.Walk([]string{
		filepath.Join(real, "ok.txt"),
		"/does/not/exist.txt",
	}, walker.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Errorf("expected 1 real file, got %d", len(files))
	}
}
