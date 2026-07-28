package fssecure

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Relative is the lexical half of the containment rule, and it has to be usable
// before the named file exists: a project configuration is checked for escaping
// paths at validate time, not only when something is read.
func TestRelativeAcceptsInsideAndRefusesOutside(t *testing.T) {
	inside := map[string]string{
		"pack.json":                     "pack.json",
		"packs/pack.json":               filepath.Join("packs", "pack.json"),
		"./packs/../packs/pack.json":    filepath.Join("packs", "pack.json"),
		"packs/nested/deep.json":        filepath.Join("packs", "nested", "deep.json"),
		"packs/nested/../other/x.json":  filepath.Join("packs", "other", "x.json"),
		"packs/./nested/./deep.json":    filepath.Join("packs", "nested", "deep.json"),
		"a/b/c/../../../a/b/pack.json":  filepath.Join("a", "b", "pack.json"),
		"packs//double//sep/pack.json":  filepath.Join("packs", "double", "sep", "pack.json"),
		"packs/trailing/pack.json/":     filepath.Join("packs", "trailing", "pack.json"),
		"deeply/nested/but/fine/p.json": filepath.Join("deeply", "nested", "but", "fine", "p.json"),
	}
	for relative, want := range inside {
		got, err := Relative(relative)
		if err != nil {
			t.Fatalf("%q must resolve inside the root: %v", relative, err)
		}
		if got != want {
			t.Fatalf("%q cleaned to %q, want %q", relative, got, want)
		}
	}

	outside := []string{
		"",
		"..",
		"../escape.json",
		"packs/../../escape.json",
		"a/b/../../../escape.json",
		"/etc/passwd",
		`\etc\passwd`,
		"pack\x00.json",
	}
	for _, relative := range outside {
		if _, err := Relative(relative); !errors.Is(err, ErrOutsideRoot) {
			t.Fatalf("%q must be refused as outside the root, got %v", relative, err)
		}
	}
}

// Resolve answers where a path would be. It is a diagnostic, and it must apply
// the same lexical refusal a read does — a caller must never be handed a
// pathname for something the reader would refuse.
func TestResolveJoinsOntoTheRootAndRefusesEscapes(t *testing.T) {
	root := t.TempDir()
	resolved, err := Resolve(root, "packs/pack.json")
	if err != nil {
		t.Fatalf("a path inside the root must resolve: %v", err)
	}
	if want := filepath.Join(mustAbs(t, root), "packs", "pack.json"); resolved != want {
		t.Fatalf("resolved = %q, want %q", resolved, want)
	}
	for _, relative := range []string{"../secret.json", "/etc/passwd", ""} {
		if _, err := Resolve(root, relative); !errors.Is(err, ErrOutsideRoot) {
			t.Fatalf("%q must be refused: %v", relative, err)
		}
	}
	// A sibling whose name merely starts with the root's is not inside it, and no
	// declared path can reach it.
	if _, err := Resolve(root, "../"+filepath.Base(root)+"-backup/pack.json"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("a sibling directory must not be reachable: %v", err)
	}
}

// A Root reads what is inside it, bounds the read, and refuses every escape a
// pathname check could see plus the one it could not.
func TestRootReadsInsideAndRefusesEveryEscape(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "project")
	if err := os.MkdirAll(filepath.Join(dir, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(dir, "packs", "pack.json")
	if err := os.WriteFile(inside, []byte(`{"in":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "secret.json"), []byte(`{"secret":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	root := mustOpenRoot(t, dir)

	data, err := root.Read("packs/pack.json", 1024)
	if err != nil || string(data) != `{"in":true}` {
		t.Fatalf("data=%q err=%v", data, err)
	}
	if _, err := root.Read("packs/pack.json", 4); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("the byte limit must still apply inside the root: %v", err)
	}
	if _, err := root.Read("../secret.json", 1024); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("a lexically escaping path must be refused: %v", err)
	}
	// A path inside the root that names nothing is a read failure, not an escape:
	// the two call for different fixes and must not report the same way.
	if _, err := root.Read("packs/absent.json", 1024); err == nil || errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("an absent file inside the root is not an escape: %v", err)
	}
	if err := root.Contains("packs/absent.json"); errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("an absent file inside the root is contained: %v", err)
	}
	// A directory is not a pack.
	if _, err := root.Read("packs", 1024); err == nil {
		t.Fatal("a directory must not read as a regular file")
	}
	// A failure the operating system named is reported in its own words, not as an
	// escape: "a component of the path is a file" and "the path left the project"
	// are different defects with different fixes, and reporting the first as the
	// second would send a reader looking for a containment bug that is not there.
	if _, err := root.Read("packs/pack.json/nested.json", 1024); err == nil || errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("a path through a regular file is a read failure, not an escape: %v", err)
	}

	if runtime.GOOS == "windows" {
		return
	}
	// A symlinked directory inside the root pointing out of it: lexically the path
	// is inside, and only resolving against the handle catches it.
	if err := os.Symlink(base, filepath.Join(dir, "out")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if _, err := root.Read("out/secret.json", 1024); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("a path leaving the root through a symlinked directory must be refused: %v", err)
	}
	if err := root.Contains("out/secret.json"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("the containment check must refuse it too, before any read: %v", err)
	}
	// The final component is refused whatever it points at, including a file
	// inside the root: a pack is a file, not a name for one.
	if err := os.Symlink(inside, filepath.Join(dir, "alias.json")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if _, err := root.Read("alias.json", 1024); err == nil {
		t.Fatal("a final symlink must still be refused")
	}
	// And one pointing out of the root is refused as an escape.
	if err := os.Symlink(filepath.Join(base, "secret.json"), filepath.Join(dir, "outalias.json")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if _, err := root.Read("outalias.json", 1024); err == nil {
		t.Fatal("a final symlink out of the root must be refused")
	}
	// An inward symlinked *directory* component still resolves: it never leaves
	// the root, and refusing it would refuse a legitimate project layout.
	if err := os.Symlink("packs", filepath.Join(dir, "alias-dir")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if data, err := root.Read("alias-dir/pack.json", 1024); err != nil || string(data) != `{"in":true}` {
		t.Fatalf("a symlinked directory inside the root is inside it: data=%q err=%v", data, err)
	}
}

// The race the handle exists to close: an intermediate directory component is
// swapped for a symlink pointing out of the root *after* the path has been
// checked and before it is opened.
//
// A pathname-based reader resolves the path, states that it is contained, and
// then opens that pathname — and the open walks whatever the components denote
// at that later instant. This test performs the swap deterministically, in the
// interval such a reader would leave open, and asserts the read is still bounded
// by the directory the handle was opened on. The final assertion is the control:
// the same pathname, read the ordinary way, really does escape, so the test
// would fail against the design this one replaced rather than passing vacuously.
func TestRootContainmentHoldsWhenAnIntermediateComponentIsRetargeted(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory symlink creation is privileged on Windows")
	}
	base := t.TempDir()
	dir := filepath.Join(base, "project")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(filepath.Join(dir, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "packs", "pack.json"), []byte(`{"in":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// The same filename outside the root, which is what the swap would deliver.
	if err := os.WriteFile(filepath.Join(outside, "pack.json"), []byte(`{"secret":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	root := mustOpenRoot(t, dir)

	// Resolution: the path is checked and found contained, and reads the file it
	// names. This is the instant a pathname-based reader would report on.
	if err := root.Contains("packs/pack.json"); err != nil {
		t.Fatalf("the path must be contained before the swap: %v", err)
	}
	if data, err := root.Read("packs/pack.json", 1024); err != nil || string(data) != `{"in":true}` {
		t.Fatalf("data=%q err=%v", data, err)
	}

	// The swap: the checked intermediate component is renamed away and replaced
	// with a symlink pointing out of the root.
	if err := os.Rename(filepath.Join(dir, "packs"), filepath.Join(dir, "packs-real")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "packs")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	// The open after the swap is still resolved against the held directory, so it
	// is refused rather than following the new component out of the root.
	if _, err := root.Read("packs/pack.json", 1024); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("the read must be refused as outside the root after the swap, got %v", err)
	}
	if err := root.Contains("packs/pack.json"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("the containment check must refuse it after the swap too, got %v", err)
	}

	// Control: opening the same pathname the ordinary way does escape. Without
	// this, the assertions above could pass for a reason unrelated to the swap.
	escaped, err := os.ReadFile(filepath.Join(dir, "packs", "pack.json"))
	if err != nil || string(escaped) != `{"secret":true}` {
		t.Fatalf("the swap must actually redirect the pathname, or this test proves nothing: data=%q err=%v", escaped, err)
	}
}

// The root is the directory that was opened, not the name it was opened under.
// Renaming the directory afterward moves neither the handle nor what it reads.
func TestRootIsTheDirectoryOpenedNotItsPathname(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pack.json"), []byte(`{"in":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	root := mustOpenRoot(t, dir)
	if !strings.HasSuffix(root.Dir(), "project") {
		t.Fatalf("Dir must report the pathname it was opened on: %q", root.Dir())
	}
	// A different directory takes over the pathname. The handle still reads the
	// directory it was opened on. Windows expresses the same containment the
	// other way around: an open directory cannot be renamed at all, so the
	// pathname can never be retargeted while the handle is held.
	if err := os.Rename(dir, filepath.Join(base, "moved")); err != nil {
		if runtime.GOOS == "windows" {
			data, readErr := root.Read("pack.json", 1024)
			if readErr != nil || string(data) != `{"in":true}` {
				t.Fatalf("the held handle must keep reading its directory: data=%q err=%v", data, readErr)
			}
			return
		}
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pack.json"), []byte(`{"impostor":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := root.Read("pack.json", 1024)
	if err != nil || string(data) != `{"in":true}` {
		t.Fatalf("the handle must still read the directory it opened: data=%q err=%v", data, err)
	}
}

func TestOpenRootRefusesAMissingDirectory(t *testing.T) {
	if _, err := OpenRoot(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("opening a root that is not there must fail")
	}
}

func mustOpenRoot(t *testing.T, dir string) *Root {
	t.Helper()
	root, err := OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return absolute
}
