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

// hardLinked reports whether an opened file has more than one name. Append
// refuses one, so a record cannot be written into a file the trail's name was
// hardlinked to. The link count is the only way to ask: a hardlink is
// indistinguishable from the original by every path-based check there is.
func hardLinked(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink > 1
}

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
