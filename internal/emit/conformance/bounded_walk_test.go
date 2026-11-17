package conformance

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
)

// A vector's directory and its expected/ are listed under bounds on their
// entries: past one the vector is refused at that directory, unread, and at
// it the vector is read. In expected/, files and directories are bounded
// apart, so a deep tree of few files is not refused for its directories.
func TestVectorListingsAreBounded(t *testing.T) {
	savedVector, savedFiles, savedDirs := maxVectorEntries, maxExpectedFiles, maxExpectedDirs
	defer func() { maxVectorEntries, maxExpectedFiles, maxExpectedDirs = savedVector, savedFiles, savedDirs }()
	files := map[string]string{"request.json": "{}", "expected/gen/a": "a\n", "expected/gen/b": "b\n"}
	dir := filepath.Join(t.TempDir(), "v")
	write(t, dir, files)
	refusedAt := func(want string) {
		t.Helper()
		_, refused := layoutVector(dir, "v", false)
		switch {
		case want == "" && len(refused) != 0:
			t.Fatalf("a vector at the bounds was refused: %+v", refused)
		case want != "" && (len(refused) != 1 || refused[0].Code != "EMIT-VECTOR" || refused[0].Location.Path != want):
			t.Fatalf("want a refusal at %s: %+v", want, refused)
		}
	}

	maxVectorEntries, maxExpectedFiles, maxExpectedDirs = 2, 2, 1
	refusedAt("")
	maxVectorEntries = 1
	refusedAt("v")
	// Each bound alone: the other is wide, so the walk's own limit (their
	// sum) is not what refuses.
	maxVectorEntries, maxExpectedFiles, maxExpectedDirs = 2, 1, 5
	refusedAt("v/expected/gen/b")
	maxExpectedFiles, maxExpectedDirs = 5, 0
	refusedAt("v/expected")
}

// The bounds themselves: 7000 files at gen/<n>/x/f, 21001 entries, are read;
// a path at emit.MaxPathBytes is read and one byte past it is refused.
func TestExpectedBoundsFollowTheResponseBounds(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 7000 files")
	}
	dir := filepath.Join(t.TempDir(), "v")
	files := map[string]string{"request.json": "{}"}
	for n := 0; n < 7000; n++ {
		files[fmt.Sprintf("expected/gen/%d/x/f", n)] = ""
	}
	write(t, dir, files)
	entries, refused := layoutExpected(filepath.Join(dir, "expected"), "v/expected", false)
	if len(refused) != 0 || len(entries) != 7000 {
		t.Fatalf("7000 nested files: %d entries, %+v", len(entries), refused)
	}

	for _, c := range []struct {
		length int
		refuse bool
	}{{emit.MaxPathBytes, false}, {emit.MaxPathBytes + 1, true}} {
		dir := filepath.Join(t.TempDir(), "expected")
		segment := strings.Repeat("d", 99)
		var path string
		for len(path)+len(segment)+1 < c.length-2 {
			path += segment + "/"
		}
		path += strings.Repeat("f", c.length-len(path))
		write(t, dir, map[string]string{path: ""})
		entries, refused := layoutExpected(dir, "v/expected", false)
		if c.refuse != (len(refused) == 1 && refused[0].Code == "EMIT-VECTOR" && refused[0].Location.Path == "v/expected") || c.refuse == (len(entries) == 1) {
			t.Fatalf("a path of %d bytes: %d entries, %+v", len(path), len(entries), refused)
		}
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
