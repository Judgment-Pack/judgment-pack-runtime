//go:build !windows

package conformance

import (
	"os"
	"path/filepath"
	"slices"
	"syscall"
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

// A link below expected/ is not a file of the record: the vector is refused
// at its path before any run.
func TestALinkInExpectedIsRefused(t *testing.T) {
	f := newFixture(t)
	f.registryVector("v", map[string]string{"expected/gen/b": "b\n"})
	if err := os.Symlink("b", filepath.Join(f.vectors, "v", "expected", "gen", "a")); err != nil {
		t.Fatal(err)
	}
	report := f.run(f.manifest(requestMode, nil), false)
	if report.Outcome != OutcomeNotRun || len(report.Rows) != 0 || len(report.Diagnostics) != 1 {
		t.Fatalf("report: %s", dump(report))
	}
	if d := report.Diagnostics[0]; d.Code != "EMIT-VECTOR" || d.Location.Document != DocumentVector || d.Location.Path != "v/expected/gen/a" {
		t.Fatalf("diagnostic: %+v", d)
	}
}

// A vector file that is not a regular file when the vector is read, a FIFO
// here, is EMIT-PATH-UNREADABLE at its path, and is not read.
func TestAVectorFileThatIsNotRegularIsUnreadable(t *testing.T) {
	for _, file := range []string{"options.json", "kinds/queue.json", "expected-failure.json", "registry.json", "request.json"} {
		t.Run(file, func(t *testing.T) {
			f := newFixture(t)
			if file == "request.json" {
				write(t, f.vectors, map[string]string{"v/request.json": "{}", "v/expected-failure.json": `{"code":"EMIT-PROCESS"}`})
			} else {
				f.registryVector("v", map[string]string{"options.json": "{}", "expected-failure.json": `{"code":"EMIT-PROCESS"}`})
			}
			full := filepath.Join(f.vectors, "v", filepath.FromSlash(file))
			if err := os.Remove(full); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Mkfifo(full, 0o644); err != nil {
				t.Fatal(err)
			}
			vectors, refused := readVectors(f.vectors, false)
			if len(refused) > 0 || len(vectors) != 1 {
				t.Fatalf("layout: %+v", refused)
			}
			refused = vectors[0].load()
			if len(refused) != 1 {
				t.Fatalf("refused: %+v", refused)
			}
			if d := refused[0]; d.Code != "EMIT-PATH-UNREADABLE" || d.Location.Document != DocumentVector || d.Location.Path != "v/"+file {
				t.Fatalf("diagnostic: %+v", d)
			}
		})
	}
}

// A file the emitter gives as executable is written executable by --update,
// and a run whose expected/ holds it otherwise fails check 1 on its mode.
func TestExpectedFileModes(t *testing.T) {
	f := newFixture(t)
	write(t, f.root, map[string]string{"answers/tool.json": `{"emitResponseVersion":"1.0.0","files":[{"path":"gen/tool","text":"x\n","mode":"0755"}],"diagnostics":[]}` + "\n"})
	m := f.manifest([]string{"echo", "answers/tool.json"}, map[string]any{"environments": "all"})
	f.registryVector("v", nil)
	if report := f.run(m, true); rowOf(t, report, "v", checkGolden).Reason != reasonUpdated {
		t.Fatalf("update: %s", dump(report))
	}
	written := filepath.Join(f.vectors, "v", "expected", "gen", "tool")
	if info, err := os.Stat(written); err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("expected/gen/tool: %v %v", info, err)
	}
	if err := os.Chmod(written, 0o644); err != nil {
		t.Fatal(err)
	}
	row := rowOf(t, f.run(m, false), "v", checkGolden)
	if want := []emit.Difference{{Path: "gen/tool", Difference: emit.DifferenceMode}}; row.Status != StatusFail || row.Reason != reasonDifferent || !slices.Equal(row.Differences, want) {
		t.Fatalf("golden: %+v", row)
	}
}
