package conformance

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// The copy's root is compared with what it was: an emitter that changes its
// bits has written to the copy.
func TestStrayWriteToTheCopyRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a directory's permission bits are not kept on Windows")
	}
	source := t.TempDir()
	write(t, source, map[string]string{"README": "readme\n"})
	w, failure := newWorkspace(source)
	if failure != nil {
		t.Fatalf("workspace: %+v", failure)
	}
	defer w.close()
	if changes, failure := w.strayWrites(); len(changes) != 0 || failure != nil {
		t.Fatalf("a fresh copy changed: %+v %+v", changes, failure)
	}
	info, err := os.Lstat(w.root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(w.root, info.Mode().Perm()^0o055); err != nil {
		t.Fatal(err)
	}
	changes, failure := w.strayWrites()
	if want := []StrayWrite{{"EMIT-STRAY-WRITE", ".", StrayChanged}}; !slices.Equal(changes, want) || failure != nil {
		t.Fatalf("changes: %+v %+v", changes, failure)
	}
	// The copy is made afresh, its root as it was.
	if changes, failure := w.strayWrites(); len(changes) != 0 || failure != nil {
		t.Fatalf("the fresh copy changed: %+v %+v", changes, failure)
	}
}

// A file is copied only to the bytes the copy's bound has left, whatever
// its size read before: one that grew past them is errCopyBound.
func TestCopyFileBoundsTheBytesCopied(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	if err := os.WriteFile(source, []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := copyFile(source, filepath.Join(dir, "short"), 0o644, 9); !errors.Is(err, errCopyBound) {
		t.Fatalf("past the bound: %v", err)
	}
	state, err := copyFile(source, filepath.Join(dir, "whole"), 0o644, 10)
	if err != nil || state.size != 10 {
		t.Fatalf("at the bound: %+v %v", state, err)
	}
}
