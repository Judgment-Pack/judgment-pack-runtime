//go:build unix

package emit

import (
	"os"
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

// A descriptor list that shows only 0, 1 and 2 is not taken as complete:
// closeInherited marks an inherited descriptor above them by number.
func TestCloseInheritedScansWhenTheListShowsOnlyStandardDescriptors(t *testing.T) {
	fd, err := syscall.Dup(1)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	if fd < 3 {
		t.Fatalf("descriptor %d is not above standard error", fd)
	}
	listed := t.TempDir()
	for _, name := range []string{"0", "1", "2"} {
		if err := os.WriteFile(filepath.Join(listed, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	saved := inheritedDir
	inheritedDir = listed
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
