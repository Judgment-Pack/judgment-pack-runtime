package fssecure

import (
	"io/fs"
	"runtime"
)

// LinkLike reports whether a final component, read without following links
// (Lstat), may lead elsewhere, so that the checks here refuse it as a link:
//
//   - a symbolic link, on every platform;
//   - on Windows, what Go reads as irregular (a reparse point other than a
//     symbolic link) that is a directory: a junction, which is always one,
//     or another directory reparse point. A file reparse point, such as a
//     synced folder's placeholder, is not refused here; it is held to the
//     same-file check after the open like any other file;
//   - elsewhere, what Go reads as irregular, an unknown file type.
//
// No final component these checks accept is a directory, so refusing
// directory reparse points loses nothing a caller could have opened.
func LinkLike(info fs.FileInfo) bool {
	mode := info.Mode()
	if mode&fs.ModeSymlink != 0 {
		return true
	}
	if mode&fs.ModeIrregular == 0 {
		return false
	}
	return linkGOOS != "windows" || mode.IsDir() || directoryAttribute(info)
}

// linkGOOS is the platform LinkLike answers for; a test sets it.
var linkGOOS = runtime.GOOS
