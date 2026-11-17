//go:build linux || darwin

// Linux and Darwin only: elsewhere Rlimit's fields are signed.

package emit

import (
	"path/filepath"
	"syscall"
	"testing"
)

// The scan goes up to the hard descriptor limit, not the soft one: a
// descriptor opened before the soft limit was lowered sits above it and is
// still marked.
func TestCloseInheritedScansAboveTheSoftLimit(t *testing.T) {
	fd, err := syscall.Dup(1)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	var limit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		t.Fatal(err)
	}
	if uint64(limit.Max) <= uint64(fd) {
		t.Skipf("descriptor %d is not below the hard limit %d", fd, limit.Max)
	}
	saved := inheritedDir
	inheritedDir = filepath.Join(t.TempDir(), "missing")
	defer func() { inheritedDir = saved }()
	lowered := limit
	lowered.Cur = uint64(fd)
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &lowered); err != nil {
		t.Fatal(err)
	}
	closeInherited()
	if err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limit); err != nil {
		t.Fatal(err)
	}
	flags, _, errno := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_GETFD, 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	if flags&syscall.FD_CLOEXEC == 0 {
		t.Fatalf("descriptor %d above the soft limit %d was not marked close-on-exec", fd, lowered.Cur)
	}
}
