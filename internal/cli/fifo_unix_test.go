//go:build unix

package cli

import (
	"path/filepath"
	"syscall"
	"testing"
)

// makeFIFO makes a FIFO in a fresh directory and returns its path.
func makeFIFO(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
