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
