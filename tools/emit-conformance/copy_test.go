package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestCopyTrackedCopiesOnlyTrackedPaths: a tracked file is copied as the
// working tree holds it, an ignored file is not, and a tracked symbolic link
// that points out of the tree stays a link with its target, so the harness
// still sees and refuses it.
func TestCopyTrackedCopiesOnlyTrackedPaths(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	if runtime.GOOS == "windows" {
		t.Skip("symbolic links need extra privileges on Windows")
	}
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "commit.gpgSign=false",
			"-c", "user.name=test", "-c", "user.email=test@example.invalid"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write(".gitignore", ".venv/\n")
	write("a/tracked.txt", "committed\n")
	if err := os.Symlink("/outside/the/tree", filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-q", "-m", "fixture")
	write("a/tracked.txt", "edited\n") // a local edit is kept
	write(".venv/ignored.txt", "ignored\n")
	if err := os.Symlink("/outside/the/tree", filepath.Join(root, ".venv", "python3")); err != nil {
		t.Fatal(err)
	}

	dir, cleanup, err := copyTracked(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "a", "tracked.txt"))
	if err != nil || string(got) != "edited\n" {
		t.Fatalf("the tracked file: %q, %v; want the working tree's edit", got, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, ".venv")); !os.IsNotExist(err) {
		t.Fatalf("the ignored directory was copied (Lstat: %v)", err)
	}
	if target, err := os.Readlink(filepath.Join(dir, "link")); err != nil || target != "/outside/the/tree" {
		t.Fatalf("the tracked link: %q, %v; want a link to /outside/the/tree", target, err)
	}
	cleanup()
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("the copy was not removed (Lstat: %v)", err)
	}
}
