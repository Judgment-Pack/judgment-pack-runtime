//go:build windows

package conformance

import (
	"io/fs"
	"syscall"
)

// reparsePoint says whether a directory entry carries
// FILE_ATTRIBUTE_REPARSE_POINT: a symbolic link, a junction, or another
// reparse point. An entry whose attributes cannot be read is taken as one,
// which refusesReparse then refuses unless Go reads it as a link or a file.
func reparsePoint(entry fs.DirEntry) (bool, error) {
	info, err := entry.Info()
	if err != nil {
		return false, err
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return true, nil
	}
	return data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0, nil
}
