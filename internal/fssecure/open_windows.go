//go:build windows

package fssecure

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// nonBlockingOpen has no Windows equivalent: the FIFO case it guards against on
// Unix does not arise here, and os.Root rejects an unsupported flag.
const nonBlockingOpen = 0

// hardLinked cannot be answered here: os.FileInfo's Windows Sys() carries no
// link count, and reaching for one would mean a syscall this package does not
// make. Append's other refusals stand, and the trail's containment does not
// rest on this one.
func hardLinked(os.FileInfo) bool { return false }

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

func IsRemotePath(filePath string) bool {
	volume := filepath.VolumeName(filePath)
	return strings.HasPrefix(volume, `\\`) || strings.HasPrefix(filePath, `\\`)
}
