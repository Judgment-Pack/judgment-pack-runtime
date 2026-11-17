package emit

import (
	"io/fs"
	"testing"
)

// The root's own mode must be a directory's and not a link's: a Windows
// junction reads as irregular, with or without ModeDir (Go 1.23 and later),
// and a walk from it would never go below it. Each mode below is injected,
// so the rule is pinned on every system; the Windows test makes a real
// junction.
func TestTheRootMustBeADirectoryNotALinkOrJunction(t *testing.T) {
	for _, tc := range []struct {
		mode     fs.FileMode
		root     bool
		linkLike bool
	}{
		{fs.ModeDir | 0o755, true, false},
		{fs.ModeDir | fs.ModeIrregular, false, true},
		{fs.ModeIrregular, false, true},
		{fs.ModeSymlink, false, true},
		{fs.ModeDir | fs.ModeSymlink, false, true},
		{0o644, false, false},
		{fs.ModeNamedPipe, false, false},
	} {
		if got := rootIsDirectory(tc.mode); got != tc.root {
			t.Errorf("rootIsDirectory(%v) = %v, want %v", tc.mode, got, tc.root)
		}
		if got := linkLike(tc.mode); got != tc.linkLike {
			t.Errorf("linkLike(%v) = %v, want %v", tc.mode, got, tc.linkLike)
		}
	}
}
