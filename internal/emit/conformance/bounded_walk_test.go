package conformance

import (
	"io/fs"
	"path/filepath"
	"testing"
)

// A vector's directory and its expected/ are listed under a bound on their
// entries: past it the vector is refused at that directory, unread, and at
// the bound it is read.
func TestVectorListingsAreBounded(t *testing.T) {
	savedVector, savedExpected := maxVectorEntries, maxExpectedEntries
	defer func() { maxVectorEntries, maxExpectedEntries = savedVector, savedExpected }()
	files := map[string]string{"request.json": "{}", "expected/gen/a": "a\n", "expected/gen/b": "b\n"}

	maxVectorEntries, maxExpectedEntries = 2, 3
	dir := filepath.Join(t.TempDir(), "v")
	write(t, dir, files)
	if _, refused := layoutVector(dir, "v", false); len(refused) != 0 {
		t.Fatalf("a vector at the bounds was refused: %+v", refused)
	}

	maxVectorEntries = 1
	if _, refused := layoutVector(dir, "v", false); len(refused) != 1 || refused[0].Code != "EMIT-VECTOR" || refused[0].Location.Path != "v" {
		t.Fatalf("a vector directory past its bound: %+v", refused)
	}

	maxVectorEntries, maxExpectedEntries = 2, 2
	if _, refused := layoutVector(dir, "v", false); len(refused) != 1 || refused[0].Code != "EMIT-VECTOR" || refused[0].Location.Path != "v/expected" {
		t.Fatalf("an expected/ past its bound: %+v", refused)
	}
}

// The bound counts directories and entries in every directory below the
// root, not only regular files, and SkipDir skips a directory unlisted.
func TestWalkBoundedCountsEveryEntry(t *testing.T) {
	root := t.TempDir()
	write(t, root, map[string]string{"a/b/c/d": "", "e": ""})
	var seen []string
	walk := func(limit int) error {
		seen = nil
		return walkBounded(root, limit, func(full string, _ fs.DirEntry, err error) error {
			relative, _ := filepath.Rel(root, full)
			seen = append(seen, filepath.ToSlash(relative))
			return err
		})
	}
	if err := walk(5); err != nil || len(seen) != 6 || seen[1] != "a" || seen[4] != "a/b/c/d" || seen[5] != "e" {
		t.Fatalf("at the bound: %v %v", seen, err)
	}
	if err := walk(4); err != errTooManyEntries {
		t.Fatalf("past the bound by one nested entry: %v %v", seen, err)
	}
	seen = nil
	err := walkBounded(root, 2, func(full string, entry fs.DirEntry, err error) error {
		seen = append(seen, filepath.Base(full))
		if entry.IsDir() && full != root {
			return fs.SkipDir
		}
		return err
	})
	if err != nil || len(seen) != 3 {
		t.Fatalf("a skipped directory was listed: %v %v", seen, err)
	}
}
