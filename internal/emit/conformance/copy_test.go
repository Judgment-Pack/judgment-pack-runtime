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
	w, failure := newWorkspace(source, namesNone)
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

// A stray-write row stays in the report when the copy cannot be made again
// after it: the run stops, not run, with the fail row and the reason.
func TestStrayWriteRowStaysWhenTheCopyCannotBeMadeAgain(t *testing.T) {
	f := newFixture(t)
	m := f.manifest([]string{"stray", "create", "new.txt"}, map[string]any{"environments": "all"})
	f.registryVector("a", nil)
	f.registryVector("b", nil)
	saved := makeCopyDir
	defer func() { makeCopyDir = saved }()
	made := 0
	makeCopyDir = func(dir, pattern string) (string, error) {
		if made++; made > 1 {
			return "", errors.New("no more copies")
		}
		return saved(dir, pattern)
	}
	report := f.run(m, true)
	if report.Outcome != OutcomeNotRun || report.Exit() != ExitNotRun || len(report.Diagnostics) != 1 || report.Diagnostics[0].Code != "EMIT-PATH-UNREADABLE" {
		t.Fatalf("report: %s", dump(report))
	}
	row := rowOf(t, report, "a", checkStrayWrites)
	if want := []StrayWrite{{"EMIT-STRAY-WRITE", ".", StrayCreated}}; row.Status != StatusFail || row.Reason != reasonStray || !slices.Equal(row.StrayWrites, want) {
		t.Fatalf("check 6: %+v", row)
	}
	for _, r := range report.Rows {
		if r.Vector == "b" {
			t.Fatalf("vector b ran: %s", dump(report))
		}
	}
}
