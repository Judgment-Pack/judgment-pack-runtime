//go:build unix

package emit

import (
	"path/filepath"
	"syscall"
	"testing"
)

// Where the descriptor list cannot be read, closeInherited still marks an
// inherited descriptor close-on-exec, by number.
func TestCloseInheritedFallsBackToNumbers(t *testing.T) {
	fd, err := syscall.Dup(1)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	saved := inheritedDir
	inheritedDir = filepath.Join(t.TempDir(), "missing")
	defer func() { inheritedDir = saved }()
	closeInherited()
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFD, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	if flags&syscall.FD_CLOEXEC == 0 {
		t.Fatalf("descriptor %d was not marked close-on-exec", fd)
	}
}
