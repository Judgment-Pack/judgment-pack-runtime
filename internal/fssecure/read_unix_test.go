//go:build unix

package fssecure

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReadRegularRejectsFIFOWithoutBlocking(t *testing.T) {
	target := filepath.Join(t.TempDir(), "input.fifo")
	if err := syscall.Mkfifo(target, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := ReadRegular(target, 1024)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected FIFO rejection")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FIFO read blocked")
	}
}

// The rooted reader refuses a FIFO on the same terms, and must not wait for a
// writer to do it: a project that names a FIFO where a pack belongs would
// otherwise hang the command rather than fail it.
func TestRootRejectsFIFOWithoutBlocking(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "input.fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := mustOpenRoot(t, dir)
	done := make(chan error, 1)
	go func() {
		_, err := root.Read("input.fifo", 1024)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected FIFO rejection")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("FIFO read blocked")
	}
}
