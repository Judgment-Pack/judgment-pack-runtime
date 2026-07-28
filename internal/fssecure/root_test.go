package fssecure

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Resolve is the lexical half of the containment rule, and it has to be usable
// before the named file exists: a project configuration is checked for escaping
// paths at validate time, not only when something is read.
func TestResolveAcceptsInsideAndRefusesOutside(t *testing.T) {
	root := t.TempDir()

	for _, relative := range []string{"pack.json", "packs/pack.json", "./packs/../packs/pack.json", "packs/nested/deep.json"} {
		resolved, err := Resolve(root, relative)
		if err != nil {
			t.Fatalf("%q must resolve inside the root: %v", relative, err)
		}
		if !within(mustAbs(t, root), resolved) {
			t.Fatalf("%q resolved to %q, which is not inside %q", relative, resolved, root)
		}
	}

	outside := []string{
		"",
		"..",
		"../escape.json",
		"packs/../../escape.json",
		"/etc/passwd",
		`\etc\passwd`,
		"pack\x00.json",
	}
	for _, relative := range outside {
		if _, err := Resolve(root, relative); !errors.Is(err, ErrOutsideRoot) {
			t.Fatalf("%q must be refused as outside the root, got %v", relative, err)
		}
	}

	// A sibling whose name merely starts with the root's is not inside it. The
	// prefix comparison has to carry a separator or this passes wrongly.
	sibling := root + "-backup"
	if within(root, sibling) {
		t.Fatalf("%q must not count as inside %q", sibling, root)
	}
}

// ReadWithin refuses at read time what Resolve refuses at validate time, and it
// refuses one more thing Resolve cannot see: a path that leaves the root through
// a symlinked directory component.
func TestReadWithinReadsInsideAndRefusesEveryEscape(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "packs", "pack.json")
	if err := os.WriteFile(inside, []byte(`{"in":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(base, "secret.json")
	if err := os.WriteFile(secret, []byte(`{"secret":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	data, err := ReadWithin(root, "packs/pack.json", 1024)
	if err != nil || string(data) != `{"in":true}` {
		t.Fatalf("data=%q err=%v", data, err)
	}
	if _, err := ReadWithin(root, "packs/pack.json", 4); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("the byte limit must still apply inside the root: %v", err)
	}
	if _, err := ReadWithin(root, "../secret.json", 1024); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("a lexically escaping path must be refused: %v", err)
	}

	if runtime.GOOS == "windows" {
		return
	}
	// A symlinked directory inside the root pointing out of it: lexically the path
	// is inside, and only canonicalizing the containing directory catches it.
	if err := os.Symlink(base, filepath.Join(root, "out")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if _, err := ReadWithin(root, "out/secret.json", 1024); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("a path leaving the root through a symlinked directory must be refused: %v", err)
	}
	// And the final component is still refused by the open itself, even when the
	// symlink points at a file inside the root.
	if err := os.Symlink(inside, filepath.Join(root, "alias.json")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if _, err := ReadWithin(root, "alias.json", 1024); err == nil {
		t.Fatal("a final symlink must still be refused")
	}
}

// ResolveWithin is the containment rule for a caller that needs the path rather
// than the bytes — the evaluator reaching a pack by decision id. It has to refuse
// everything ReadWithin refuses before the open, and it has to hand back the
// canonicalized target: a caller that resolved one path and opened another would
// apply the two halves of the rule to two different files.
func TestResolveWithinRefusesEveryEscapeAReadWould(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	if err := os.MkdirAll(filepath.Join(root, "packs"), 0o755); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "packs", "pack.json")
	if err := os.WriteFile(inside, []byte(`{"in":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "secret.json"), []byte(`{"secret":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	resolved, err := ResolveWithin(root, "packs/pack.json")
	if err != nil {
		t.Fatalf("a path inside the root must resolve: %v", err)
	}
	if !within(mustAbs(t, mustEval(t, root)), resolved) {
		t.Fatalf("%q is not inside %q", resolved, root)
	}
	if data, err := ReadRegular(resolved, 1024); err != nil || string(data) != `{"in":true}` {
		t.Fatalf("the resolved path is the one to open: data=%q err=%v", data, err)
	}
	// A file that does not exist yet resolves: the check is about where the path
	// is, and the read is what reports that nothing is there.
	if _, err := ResolveWithin(root, "packs/absent.json"); err != nil {
		t.Fatalf("an absent file inside the root is not an escape: %v", err)
	}
	if _, err := ResolveWithin(root, "../secret.json"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("a lexically escaping path must be refused: %v", err)
	}

	if runtime.GOOS == "windows" {
		return
	}
	if err := os.Symlink(base, filepath.Join(root, "out")); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if _, err := ResolveWithin(root, "out/secret.json"); !errors.Is(err, ErrOutsideRoot) {
		t.Fatalf("a path leaving the root through a symlinked directory must be refused: %v", err)
	}
}

func mustEval(t *testing.T, path string) string {
	t.Helper()
	evaluated, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return evaluated
}

func mustAbs(t *testing.T, path string) string {
	t.Helper()
	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return absolute
}
