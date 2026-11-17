package conformance

import (
	"fmt"
	"io/fs"
	"os"
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
		return walkBounded(root, limit, walkBudget{}, func(full string, _ fs.DirEntry, err error) error {
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
	err := walkBounded(root, 2, walkBudget{}, func(full string, entry fs.DirEntry, err error) error {
		seen = append(seen, filepath.Base(full))
		if entry.IsDir() && full != root {
			return fs.SkipDir
		}
		return err
	})
	if err != nil || len(seen) != 3 {
		t.Fatalf("a skipped directory was listed: %v %v", seen, err)
	}
	// A budget's room bounds each listing, and listed is told each count.
	held := map[string]int{}
	budget := walkBudget{
		room:   func(full string) int { return 2 - len(held) },
		listed: func(full string, n int) { held[filepath.Base(full)] = n },
	}
	walkAll := func(string, fs.DirEntry, error) error { return nil }
	// The root holds a and e, a holds b, and b, listed with no room left,
	// holds c.
	if err := walkBounded(root, 5, budget, walkAll); err != errTooManyEntries || len(held) != 2 || held[filepath.Base(root)] != 2 || held["a"] != 1 {
		t.Fatalf("past the room at a/b/: %v %v", held, err)
	}
	clear(held)
	budget.room = func(string) int { return 2 }
	if err := walkBounded(root, 5, budget, walkAll); err != nil || len(held) != 4 || held["c"] != 1 {
		t.Fatalf("within the room: %v %v", held, err)
	}
}

// countListings counts the directories the walk lists and the entries they
// hold, until the test ends, and the bound each listing was given.
func countListings(t *testing.T) (listings, entries *int, bounds map[string]int) {
	t.Helper()
	listings, entries, bounds = new(int), new(int), map[string]int{}
	saved := listWalked
	t.Cleanup(func() { listWalked = saved })
	listWalked = func(dir string, keep func(fs.DirEntry) bool, limit int) ([]fs.DirEntry, bool, error) {
		kept, over, err := saved(dir, keep, limit)
		*listings++
		*entries += len(kept)
		bounds[filepath.Base(dir)] = limit
		return kept, over, err
	}
	return listings, entries, bounds
}

func mkdirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// A record within the bounds is read whole however deep: three files, each
// below a chain of its own 255 directories, the most a path allows, and
// 765 directories in all. A few empty directories beside a record's files
// are read past, as before, in both modes.
func TestExpectedReadsADeepRecordAndEmptyDirectories(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "expected")
	files := map[string]string{}
	for _, top := range []string{"a", "b", "c"} {
		path := top + strings.Repeat("/d", emit.MaxPathBytes/2-2) + "/f"
		if len(path) != emit.MaxPathBytes-1 || strings.Count(path, "/") != emit.MaxPathBytes/2-1 {
			t.Fatalf("a path of %d bytes below %d directories", len(path), strings.Count(path, "/"))
		}
		files[path] = top
	}
	write(t, dir, files)
	for _, update := range []bool{false, true} {
		if entries, refused := layoutExpected(dir, "v/expected", update); len(refused) != 0 || len(entries) != 3 {
			t.Fatalf("update %v: %d files, %+v", update, len(entries), refused)
		}
	}
	dir = filepath.Join(t.TempDir(), "expected")
	write(t, dir, map[string]string{"gen/a": "a\n", "z": "z\n"})
	mkdirs(t, dir, "gen/empty", "gen/x/y/empty", "other")
	for _, update := range []bool{false, true} {
		if entries, refused := layoutExpected(dir, "v/expected", update); len(refused) != 0 || len(entries) != 2 {
			t.Fatalf("update %v: %d files, %+v", update, len(entries), refused)
		}
	}
}

// The directories walked are bounded by the files read: past the depth of
// one path for each, and of the path being walked, they are no record's,
// and the vector is refused at expected/. One file and 511 empty directories
// after it are read; one more directory is refused, in both modes.
func TestExpectedDirectoriesAreBoundedByTheFilesRead(t *testing.T) {
	for _, c := range []struct {
		dirs   int
		refuse bool
	}{{emit.MaxPathBytes - 1, false}, {emit.MaxPathBytes, true}} {
		dir := filepath.Join(t.TempDir(), "expected")
		write(t, dir, map[string]string{"a": ""})
		for n := range c.dirs {
			mkdirs(t, dir, fmt.Sprintf("d%04d", n))
		}
		for _, update := range []bool{false, true} {
			entries, refused := layoutExpected(dir, "v/expected", update)
			if c.refuse != (len(refused) == 1 && refused[0].Code == "EMIT-VECTOR" && refused[0].Location.Path == "v/expected") || !c.refuse && len(entries) != 1 {
				t.Fatalf("%d directories, update %v: %d files, %+v", c.dirs, update, len(entries), refused)
			}
		}
	}
}

// Each listing is bounded by the files: what the bound still allows, less
// the entries still to walk, and one more, so that the file past the bound
// is refused at its path as before. A listing past that refuses the vector
// at expected/, unread.
func TestExpectedListingsAreBoundedByTheFiles(t *testing.T) {
	saved := maxExpectedFiles
	defer func() { maxExpectedFiles = saved }()
	maxExpectedFiles = 3
	for _, c := range []struct {
		name  string
		files []string
		want  string
	}{
		{"three files", []string{"a", "b", "c"}, ""},
		{"one file past the bound", []string{"a", "b", "c", "d"}, "v/expected/d"},
		{"two past it", []string{"a", "b", "c", "d", "e"}, "v/expected"},
		{"two directories of two", []string{"a/1", "a/2", "b/1", "b/2"}, "v/expected/b/2"},
		// When b/ is listed, two files are read and c/ is still to walk, so
		// b/ may hold one entry and one more.
		{"three directories of two", []string{"a/1", "a/2", "b/1", "b/2", "c/1", "c/2"}, "v/expected"},
		{"three directories, the last of two", []string{"a/1", "b/1", "c/1", "c/2"}, "v/expected/c/2"},
	} {
		dir := filepath.Join(t.TempDir(), "expected")
		files := map[string]string{}
		for _, file := range c.files {
			files[file] = ""
		}
		write(t, dir, files)
		for _, update := range []bool{false, true} {
			_, refused := layoutExpected(dir, "v/expected", update)
			switch {
			case c.want == "" && len(refused) != 0:
				t.Fatalf("%s: refused %+v", c.name, refused)
			case c.want != "" && (len(refused) != 1 || refused[0].Code != "EMIT-VECTOR" || refused[0].Location.Path != c.want):
				t.Fatalf("%s, update %v: want a refusal at %s: %+v", c.name, update, c.want, refused)
			}
		}
	}
	// An empty directory, walked, leaves the room its entry took: after a/
	// and b/, w/ may hold three files.
	dir := filepath.Join(t.TempDir(), "expected")
	write(t, dir, map[string]string{"w/1": "", "w/2": "", "w/3": ""})
	mkdirs(t, dir, "a", "b")
	for _, update := range []bool{false, true} {
		if entries, refused := layoutExpected(dir, "v/expected", update); len(refused) != 0 || len(entries) != 3 {
			t.Fatalf("update %v: two empty directories before three files: %d files, %+v", update, len(entries), refused)
		}
	}
}

// A tree of empty directories is refused after a walk the size of one path,
// not of the tree: 3280 directories, 3 to each, 7 deep, are refused after
// 257 of them, in both modes. Before, the walk listed every one, and --update
// replaced the tree.
func TestATreeOfEmptyDirectoriesIsRefusedEarly(t *testing.T) {
	if testing.Short() {
		t.Skip("makes 3280 directories")
	}
	dir := filepath.Join(t.TempDir(), "expected")
	var grow func(path string, depth int)
	grow = func(path string, depth int) {
		mkdirs(t, dir, path)
		if depth == 7 {
			return
		}
		for _, name := range []string{"a", "b", "c"} {
			grow(path+"/"+name, depth+1)
		}
	}
	grow(".", 0)
	listings, entries, _ := countListings(t)
	for _, update := range []bool{false, true} {
		*listings, *entries = 0, 0
		if _, refused := layoutExpected(dir, "v/expected", update); len(refused) != 1 || refused[0].Code != "EMIT-VECTOR" || refused[0].Location.Path != "v/expected" {
			t.Fatalf("update %v: refused %+v", update, refused)
		}
		t.Logf("update %v: listed %d directories and %d entries", update, *listings, *entries)
		if *listings > emit.MaxPathBytes/2+1 {
			t.Fatalf("update %v: listed %d directories and %d entries; want at most the root and %d", update, *listings, *entries, emit.MaxPathBytes/2)
		}
	}
}
