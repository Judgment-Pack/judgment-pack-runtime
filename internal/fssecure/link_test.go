package fssecure

import (
	"io/fs"
	"testing"
)

// A junction reads as irregular, with or without ModeDir, and is refused
// wherever a symbolic link is.
func TestLinkLikeCoversJunctions(t *testing.T) {
	for mode, want := range map[fs.FileMode]bool{
		fs.ModeSymlink:                true,
		fs.ModeSymlink | fs.ModeDir:   true,
		fs.ModeIrregular:              true,
		fs.ModeIrregular | fs.ModeDir: true,
		0:                             false,
		fs.ModeDir:                    false,
		fs.ModeNamedPipe:              false,
	} {
		if linkLike(mode) != want {
			t.Errorf("linkLike(%v) = %v", mode, !want)
		}
	}
}
