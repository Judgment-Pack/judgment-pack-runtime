package conformance

import (
	"os"
	"path/filepath"
	"slices"
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
