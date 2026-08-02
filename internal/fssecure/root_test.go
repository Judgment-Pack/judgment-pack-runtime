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

// The one write this package performs is bounded by the same handle every read
// is, and it appends: what is already in the file is never rewritten, and two
// records are two lines in the order they were written.
func TestRootAppendsInsideAndNeverRewrites(t *testing.T) {
	dir := t.TempDir()
	root := mustOpenRoot(t, dir)

	if err := root.MakeDir("records/evaluations"); err != nil {
		t.Fatalf("a directory inside the root must be creatable: %v", err)
	}
	// Making a directory that is already there is not a failure: every record
	// prepares its own directory, and the second record must not fail because
	// the first one succeeded.
	if err := root.MakeDir("records/evaluations"); err != nil {
		t.Fatalf("an existing directory is made: %v", err)
	}
	for _, record := range []string{"{\"n\":1}\n", "{\"n\":2}\n"} {
		if err := root.Append("records/evaluations/log.jsonl", []byte(record)); err != nil {
			t.Fatalf("appending %q: %v", record, err)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "records", "evaluations", "log.jsonl"))
	if err != nil || string(data) != "{\"n\":1}\n{\"n\":2}\n" {
		t.Fatalf("data=%q err=%v", data, err)
	}
	// The mode is the project's own trail, not something a reader of the
	// directory is entitled to by default.
	info, err := os.Stat(filepath.Join(dir, "records", "evaluations", "log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("the record file must not be group- or world-readable: %v", info.Mode())
	}
}

// Every refusal the reader makes, the writer makes: a path is not permitted to
// leave the root because it is being written rather than read.
func TestRootAppendRefusesEveryEscapeTheReaderRefuses(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	root := mustOpenRoot(t, dir)

	for _, relative := range []string{"", "..", "../escape.jsonl", "records/../../escape.jsonl", "/etc/passwd", "log\x00.jsonl"} {
		if err := root.Append(relative, []byte("x\n")); !errors.Is(err, ErrOutsideRoot) {
			t.Fatalf("%q must be refused as outside the root, got %v", relative, err)
		}
		if err := root.MakeDir(relative); !errors.Is(err, ErrOutsideRoot) {
			t.Fatalf("%q must be refused as a directory outside the root, got %v", relative, err)
		}
	}
	// Nothing was created outside the root while those were refused.
	if _, err := os.Lstat(filepath.Join(base, "escape.jsonl")); err == nil {
		t.Fatal("a refused append must not have written anything")
	}
	// A directory is not a record file, and a path through a regular file is a
	// write failure the operating system named rather than an escape.
	if err := root.MakeDir("records"); err != nil {
		t.Fatal(err)
	}
	// "Already there" is only "already made" when the entry is a directory:
	// mkdir reports the same "exists" for a regular file, and the refusal must
	// name the component that is wrong rather than defer to the append.
	if err := os.WriteFile(filepath.Join(dir, "occupied"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := root.MakeDir("occupied"); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("a regular file where a directory is asked for must be refused by name: %v", err)
	}
	if err := root.Append("records", []byte("x\n")); err == nil {
		t.Fatal("a directory must not be appended to as a regular file")
	}
	if err := os.WriteFile(filepath.Join(dir, "plain"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := root.Append("plain/log.jsonl", []byte("x\n")); err == nil || errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("a path through a regular file is a write failure, not an escape: %v", err)
	}

	if runtime.GOOS == "windows" {
		return
	}
	// A symlinked directory component pointing out of the root: lexically the
	// path is inside, and only resolving against the handle catches it.
	if err := os.Symlink(base, filepath.Join(dir, "out")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if err := root.Append("out/escape.jsonl", []byte("x\n")); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("a path leaving the root through a symlinked directory must be refused: %v", err)
	}
	if err := root.MakeDir("out/escape"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("a directory outside the root through a symlink must be refused: %v", err)
	}
	// A final component that is a symlink is refused whatever it points at,
	// exactly as it is for a read — including one pointing at a file inside the
	// root, and including one pointing at a file that is not there yet.
	target := filepath.Join(dir, "real.jsonl")
	if err := os.WriteFile(target, []byte("kept\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "alias.jsonl")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if err := root.Append("alias.jsonl", []byte("appended\n")); err == nil {
		t.Fatal("a final symlink must be refused")
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "kept\n" {
		t.Fatalf("the refused append must not have reached the link's target: data=%q err=%v", data, err)
	}
	if err := os.Symlink(filepath.Join(base, "escape.jsonl"), filepath.Join(dir, "outalias.jsonl")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if err := root.Append("outalias.jsonl", []byte("x\n")); err == nil {
		t.Fatal("a final symlink out of the root must be refused")
	}
	if _, err := os.Lstat(filepath.Join(base, "escape.jsonl")); err == nil {
		t.Fatal("the refused append must not have created the link's target")
	}

	// The refusal is side-effect-free, which is the half a check made only
	// after the open cannot deliver: this open carries O_CREATE and os.Root
	// follows a symlink that stays inside the root, so a link pointing at a
	// path that is not there yet would otherwise be refused *after* the file it
	// named had been planted — again on every run.
	if err := os.Symlink(filepath.Join(dir, "planted.json"), filepath.Join(dir, "inalias.jsonl")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if err := root.Append("inalias.jsonl", []byte("x\n")); err == nil {
		t.Fatal("a final symlink to a file that is not there must be refused")
	}
	if _, err := os.Lstat(filepath.Join(dir, "planted.json")); err == nil {
		t.Fatal("a refused append must not create the file the link pointed at")
	}

	// A hardlink is invisible to every path-based check there is — the alias is
	// a second name for one inode — so the link count is what stops a record
	// being appended into whatever else that inode is named by, a pack most
	// obviously.
	pack := filepath.Join(dir, "pack.json")
	if err := os.WriteFile(pack, []byte("{\"id\":\"hiring\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(pack, filepath.Join(dir, "linked.jsonl")); err != nil {
		t.Skipf("cannot create hardlink: %v", err)
	}
	if err := root.Append("linked.jsonl", []byte("record\n")); err == nil {
		t.Fatal("a file with more than one name must be refused")
	}
	if data, err := os.ReadFile(pack); err != nil || string(data) != "{\"id\":\"hiring\"}\n" {
		t.Fatalf("the refused append must not have reached the aliased file: data=%q err=%v", data, err)
	}
}

// The open that creates is exclusive, so a name that appears between the check
// and the open loses the race instead of being followed into existence. This is
// the interleaving a check-then-O_CREATE open cannot cover: the pre-open Lstat
// sees nothing, a symlink to an absent target is put there, and the open must
// not create that target.
func TestRootAppendNeverCreatesWhatItWasNotAskedFor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is privileged on Windows")
	}
	dir := t.TempDir()
	root := mustOpenRoot(t, dir)
	// The swap, performed in the window a pathname-based writer would leave: the
	// name is absent when Append is entered, and a link is in place by the time
	// the open runs. Doing it deterministically means creating the link first
	// and asserting the exclusive open loses to it, which is the same instant
	// from the open's point of view.
	if err := os.Symlink(filepath.Join(dir, "target.json"), filepath.Join(dir, "swapped.jsonl")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if err := root.Append("swapped.jsonl", []byte("x\n")); err == nil {
		t.Fatal("a name that became a symlink must be refused")
	}
	if _, err := os.Lstat(filepath.Join(dir, "target.json")); err == nil {
		t.Fatal("the refused append must not have created the link's target")
	}
	// And an ordinary create still works, on the same path, once the link is
	// gone: the exclusive branch is not a permanent refusal of a new trail.
	if err := os.Remove(filepath.Join(dir, "swapped.jsonl")); err != nil {
		t.Fatal(err)
	}
	if err := root.Append("swapped.jsonl", []byte("x\n")); err != nil {
		t.Fatalf("a genuinely absent trail is created: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "swapped.jsonl")); err != nil || string(data) != "x\n" {
		t.Fatalf("data=%q err=%v", data, err)
	}
}

// The containment question a directory poses is not the one a file poses. A
// declared audit directory becomes an intermediate component of everything
// written beneath it, so its final component has to be resolved — while a file's
// is deliberately left alone, because the open refuses a final symlink whatever
// it points at.
func TestContainsDirResolvesTheFinalComponent(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "project")
	if err := os.MkdirAll(filepath.Join(dir, "records"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := mustOpenRoot(t, dir)

	if err := root.ContainsDir("records"); err != nil {
		t.Fatalf("a directory inside the root is contained: %v", err)
	}
	// Not there yet is contained: the first record makes it, and a validate-time
	// check must not report a defect that does not exist.
	if err := root.ContainsDir("not/created/yet"); err != nil {
		t.Fatalf("an absent directory inside the root is contained: %v", err)
	}
	for _, relative := range []string{"..", "../outside", "/etc"} {
		if err := root.ContainsDir(relative); !errors.Is(err, ErrOutsideRoot) {
			t.Fatalf("%q must be refused: %v", relative, err)
		}
	}
	// A regular file is not a directory, and saying so at validate time beats
	// an unexplained refusal at the first record.
	if err := os.WriteFile(filepath.Join(dir, "plain"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := root.ContainsDir("plain"); err == nil || errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("a regular file is not a contained directory: %v", err)
	}

	if runtime.GOOS == "windows" {
		return
	}
	// The case Contains cannot see: a final component that is a symlink out of
	// the root. Every path written beneath it would leave the project.
	if err := os.Symlink(base, filepath.Join(dir, "escaping")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if err := root.ContainsDir("escaping"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("a final directory symlink out of the root must be refused: %v", err)
	}
	if err := root.Contains("escaping"); err != nil {
		t.Fatalf("the file form deliberately does not resolve it, which is why the directory form exists: %v", err)
	}
	// An inward one is a legitimate project layout and reads as one.
	if err := os.Symlink("records", filepath.Join(dir, "alias")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if err := root.ContainsDir("alias"); err != nil {
		t.Fatalf("a directory symlink inside the root is inside it: %v", err)
	}
}

// The trail's mode is the project's own record of its decisions, not something
// a directory listing hands out: a file put there by something else at a wider
// mode is tightened by the append rather than kept as it was found. It is a
// unix guarantee — on Windows a Go file mode reaches only the read-only
// attribute and not the DACL.
func TestRootAppendTightensAnExistingTrailFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits are not what a Windows file mode means")
	}
	dir := t.TempDir()
	root := mustOpenRoot(t, dir)
	trail := filepath.Join(dir, "evaluations.jsonl")
	if err := os.WriteFile(trail, []byte("{\"first\":true}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := root.Append("evaluations.jsonl", []byte("{\"second\":true}\n")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(trail)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("an appended trail must not stay group- or world-readable: %v", info.Mode())
	}
	// Tightening the mode is not rewriting the file: what was already in it
	// stays, and the record goes after it.
	if data, err := os.ReadFile(trail); err != nil || string(data) != "{\"first\":true}\n{\"second\":true}\n" {
		t.Fatalf("data=%q err=%v", data, err)
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
