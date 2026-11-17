package conformance

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
)

// vectorNames lists a vector directory's entries by name.
func vectorNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

// --update writes the new expected/ whole before it replaces the old one: a
// write that fails partway leaves expected/ as it was, and no temporary
// directory beside it. One that succeeds leaves only the new expected/.
func TestUpdateIsWholeOrNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "v")
	write(t, dir, map[string]string{"request.json": "{}", "expected/gen/old": "old\n"})
	// The second file is below the first, which no write can make.
	broken := []emit.File{
		{Path: "gen/a", Data: []byte("a\n"), Mode: emit.ModeRegular},
		{Path: "gen/a/b", Data: []byte("b\n"), Mode: emit.ModeRegular},
	}
	if err := writeExpected(dir, broken); err == nil {
		t.Fatal("a write that cannot be made succeeded")
	}
	if data, err := os.ReadFile(filepath.Join(dir, "expected", "gen", "old")); err != nil || string(data) != "old\n" {
		t.Fatalf("expected/ after a failed write: %q %v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "expected", "gen", "a")); !os.IsNotExist(err) {
		t.Fatalf("a failed write left a file in expected/: %v", err)
	}
	if names := vectorNames(t, dir); !slices.Equal(names, []string{"expected", "request.json"}) {
		t.Fatalf("the vector after a failed write: %v", names)
	}
	if err := writeExpected(dir, broken[:1]); err != nil {
		t.Fatal(err)
	}
	if names := vectorNames(t, filepath.Join(dir, "expected", "gen")); !slices.Equal(names, []string{"a"}) {
		t.Fatalf("expected/gen after the write: %v", names)
	}
	if names := vectorNames(t, dir); !slices.Equal(names, []string{"expected", "request.json"}) {
		t.Fatalf("the vector after the write: %v", names)
	}
}

// The vectors directory and kinds/ are listed under their bounds: past
// either, the layout refuses, however many other files lie beside them,
// which are listed by batches and never held.
func TestListingsAreBounded(t *testing.T) {
	savedVectors, savedKinds := maxVectors, maxKindFiles
	defer func() { maxVectors, maxKindFiles = savedVectors, savedKinds }()
	maxVectors, maxKindFiles = 2, 2
	f := newFixture(t)
	f.registryVector("a", map[string]string{"expected/gen/a": "a\n"})
	f.registryVector("b", map[string]string{"expected/gen/a": "a\n"})
	beside := map[string]string{}
	for i := range 3 * listBatch {
		beside["notes-"+strconv.Itoa(i)+".txt"] = "x"
	}
	write(t, f.vectors, beside)
	vectors, refused := readVectors(f.vectors, false)
	if len(refused) > 0 || len(vectors) != 2 || vectors[0].Name != "a" || vectors[1].Name != "b" {
		t.Fatalf("at the bounds: %d vectors, %+v", len(vectors), refused)
	}
	if !slices.Equal(vectors[0].kinds, []string{"queue.json", "topic.json"}) {
		t.Fatalf("kinds: %v", vectors[0].kinds)
	}
	f.registryVector("c", map[string]string{"expected/gen/a": "a\n"})
	if _, refused := readVectors(f.vectors, false); len(refused) != 1 || refused[0].Code != "EMIT-VECTOR" || refused[0].Location.Path != "" {
		t.Fatalf("past the vectors' bound: %+v", refused)
	}
	maxVectors = 3
	write(t, filepath.Join(f.vectors, "c"), map[string]string{"kinds/extra.json": "{}"})
	if _, refused := readVectors(f.vectors, false); len(refused) != 1 || refused[0].Code != "EMIT-VECTOR" || refused[0].Location.Path != "c/kinds" {
		t.Fatalf("past the kinds' bound: %+v", refused)
	}
}

// When expected/ can be neither replaced nor put back, --update keeps the
// temporary directory that holds the old one, names it in the error, and the
// vector's layout is refused until it is removed: the old files are not
// lost.
func TestUpdateKeepsTheOldExpectedWhenItCannotBePutBack(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "v")
	write(t, dir, map[string]string{"request.json": "{}", "expected/gen/old": "old\n"})
	target := filepath.Join(dir, "expected")
	saved := rename
	defer func() { rename = saved }()
	rename = func(from, to string) error {
		if to == target {
			return os.ErrPermission
		}
		return saved(from, to)
	}
	err := writeExpected(dir, []emit.File{{Path: "gen/a", Data: []byte("a\n"), Mode: emit.ModeRegular}})
	if err == nil {
		t.Fatal("a failed replace and restore succeeded")
	}
	var kept string
	for _, name := range vectorNames(t, dir) {
		if strings.HasPrefix(name, expectedUpdatePrefix) {
			kept = filepath.Join(dir, name)
		}
	}
	if kept == "" || !strings.Contains(err.Error(), kept) {
		t.Fatalf("the temporary directory was not kept and named: %v, %v", vectorNames(t, dir), err)
	}
	if data, err := os.ReadFile(filepath.Join(kept, "old", "gen", "old")); err != nil || string(data) != "old\n" {
		t.Fatalf("the old expected/ is lost: %q %v", data, err)
	}
	rename = saved
	if _, refused := layoutVector(dir, "v", false); len(refused) == 0 {
		t.Fatal("a vector with the kept directory beside it was not refused")
	}
}
