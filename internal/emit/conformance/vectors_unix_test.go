//go:build !windows

package conformance

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Judgment-Pack/judgment-pack-runtime/internal/emit"
)

// swapForALink moves dir away and puts a link to it in its place, as a
// writer to the vectors directory could after the layout: the link leads to
// the same bytes, so only a check at the read refuses it.
func swapForALink(t *testing.T, dir string) string {
	t.Helper()
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(dir, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, dir); err != nil {
		t.Fatal(err)
	}
	return moved
}

// A directory of a vector swapped for a link after its layout is not read
// through: the vector itself, kinds/, and a directory below expected/.
func TestAVectorDirectorySwappedForALinkIsNotRead(t *testing.T) {
	cases := map[string]struct {
		swap string
		path string
	}{
		"the vector":               {".", "v/registry.json"},
		"kinds/":                   {"kinds", "v/kinds/queue.json"},
		"a directory of expected/": {"expected/gen", "v/expected/gen/a"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.registryVector("v", map[string]string{"expected/gen/a": "a\n"})
			vectors, refused := readVectors(f.vectors, false)
			if len(refused) > 0 || len(vectors) != 1 {
				t.Fatalf("layout: %+v", refused)
			}
			swapForALink(t, filepath.Join(f.vectors, "v", filepath.FromSlash(c.swap)))
			refused = vectors[0].load()
			if len(refused) == 0 {
				t.Fatal("the vector was read through a link")
			}
			if d := refused[0]; d.Code != "EMIT-PATH-UNREADABLE" || d.Location.Document != DocumentVector || d.Location.Path != c.path {
				t.Fatalf("diagnostic: %+v", refused)
			}
		})
	}
}

// --update does not write through a vector directory swapped for a link.
func TestUpdateDoesNotWriteThroughAVectorSwappedForALink(t *testing.T) {
	vectors := t.TempDir()
	dir := filepath.Join(vectors, "v")
	write(t, dir, map[string]string{"request.json": "{}"})
	moved := swapForALink(t, dir)
	if err := writeExpected(dir, []emit.File{{Path: "gen/a", Data: []byte("a\n"), Mode: emit.ModeRegular}}); err == nil {
		t.Fatal("expected/ was written through a link")
	}
	if _, err := os.Lstat(filepath.Join(moved, dirExpected)); !os.IsNotExist(err) {
		t.Fatalf("expected/ reached the link's target: %v", err)
	}
}
