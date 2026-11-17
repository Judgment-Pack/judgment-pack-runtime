//go:build windows

package fssecure

import (
	"io/fs"
	"syscall"
)

// directoryAttribute reports whether Windows marks the entry a directory
// (FILE_ATTRIBUTE_DIRECTORY). Go reads a junction as irregular without
// ModeDir, but its attributes still carry the directory bit.
func directoryAttribute(info fs.FileInfo) bool {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0
}
