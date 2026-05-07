// Package walker discovers files to process from paths/globs provided by the
// caller. It supports recursive directory traversal, include-pattern filtering,
// and exclude-pattern filtering. All filtering is done with filepath.Match so
// patterns follow standard shell glob syntax (*, ?, [abc]).
package walker

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Options configures file discovery behaviour.
type Options struct {
	// Recursive enables traversal into sub-directories. Default: false.
	Recursive bool

	// Include is a list of glob patterns a filename must match.
	// An empty list means "match all". Matching is done against the
	// base filename only (not the full path), so "*.go" works as expected.
	Include []string

	// Exclude is a list of glob patterns that disqualify a file.
	// Checked after Include. Matching is against base filename.
	Exclude []string

	// FollowSymlinks controls whether symbolic links to directories are
	// traversed. Disabled by default to avoid infinite loops.
	FollowSymlinks bool
}

// Walk expands each entry in paths into a deduplicated list of regular files
// that satisfy the options. An entry may be a file or a directory.
// Walk does NOT return an error for individual unreadable files; it skips them
// so that a single bad file does not abort the whole run.
func Walk(paths []string, opts Options) ([]string, error) {
	seen := make(map[string]struct{})
	var files []string

	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil {
			// Skip entirely unreadable entries (permission denied, missing).
			continue
		}

		if !info.IsDir() {
			// Single file — apply filters and add directly.
			abs, _ := filepath.Abs(p)
			if _, dup := seen[abs]; !dup && matchFile(info.Name(), opts) {
				seen[abs] = struct{}{}
				files = append(files, abs)
			}
			continue
		}

		// Directory path — walk it.
		err = walkDir(p, opts, seen, &files)
		if err != nil {
			return nil, err
		}
	}

	return files, nil
}

// walkDir traverses a directory tree and collects matching files.
func walkDir(root string, opts Options, seen map[string]struct{}, out *[]string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Skip unreadable entries gracefully.
			return nil
		}

		if d.IsDir() {
			if path == root {
				return nil // always descend into root itself
			}
			// Skip hidden directories (e.g. .git, .cache).
			if strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			if !opts.Recursive {
				return filepath.SkipDir
			}
			return nil
		}

		// Symlinks to files are included; symlinks to dirs require FollowSymlinks.
		if d.Type()&fs.ModeSymlink != 0 {
			if !opts.FollowSymlinks {
				return nil
			}
		}

		if !matchFile(d.Name(), opts) {
			return nil
		}

		abs, _ := filepath.Abs(path)
		if _, dup := seen[abs]; !dup {
			seen[abs] = struct{}{}
			*out = append(*out, abs)
		}
		return nil
	})
}

// matchFile returns true if name satisfies Include patterns (or Include is
// empty) and does NOT match any Exclude pattern.
func matchFile(name string, opts Options) bool {
	// Must match at least one include pattern (if any are defined).
	if len(opts.Include) > 0 {
		matched := false
		for _, pat := range opts.Include {
			if ok, _ := filepath.Match(pat, name); ok {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Must not match any exclude pattern.
	for _, pat := range opts.Exclude {
		if ok, _ := filepath.Match(pat, name); ok {
			return false
		}
	}

	return true
}
