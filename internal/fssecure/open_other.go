//go:build !unix && !windows

package fssecure

import (
	"errors"
	"os"
)

// nonBlockingOpen is not portable to a platform this package makes no assumption
// about, so the open carries no extra flag and the regular-file check stands alone.
const nonBlockingOpen = 0

func openRegular(filePath string) (*os.File, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
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
