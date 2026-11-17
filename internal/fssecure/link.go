package fssecure

import "io/fs"

// linkLike reports whether a mode read without following links (Lstat) names
// something that may lead elsewhere: a symbolic link, or what Go reads as
// irregular, which on Windows is a junction or another reparse point, with or
// without ModeDir. Every check here that refuses a final link uses it, so a
// junction is refused wherever a symbolic link is.
func linkLike(mode fs.FileMode) bool {
	return mode&(fs.ModeSymlink|fs.ModeIrregular) != 0
}
