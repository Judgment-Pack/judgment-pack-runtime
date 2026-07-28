//go:build unix

package fssecure

import (
	"errors"
	"os"
	"syscall"
)

// nonBlockingOpen keeps an open on a FIFO from waiting for a writer. Root adds it
// to every open it makes; the regular-file check refuses the FIFO immediately
// after, which is the point — the refusal must not depend on someone else writing.
const nonBlockingOpen = syscall.O_NONBLOCK

func openRegular(filePath string) (*os.File, error) {
	fd, err := syscall.Open(filePath, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), filePath)
	openedInfo, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	pathInfo, err := os.Lstat(filePath)
	if err != nil || pathInfo.Mode()&os.ModeSymlink != 0 || !os.SameFile(openedInfo, pathInfo) {
		file.Close()
		return nil, errors.New("path changed or resolves through a final symlink")
	}
	return file, nil
}

func IsRemotePath(string) bool { return false }
