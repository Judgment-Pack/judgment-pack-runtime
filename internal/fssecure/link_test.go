package fssecure

import (
	"io/fs"
	"testing"
	"time"
)

type modeInfo fs.FileMode

func (m modeInfo) Name() string       { return "m" }
func (m modeInfo) Size() int64        { return 0 }
func (m modeInfo) Mode() fs.FileMode  { return fs.FileMode(m) }
func (m modeInfo) ModTime() time.Time { return time.Time{} }
func (m modeInfo) IsDir() bool        { return fs.FileMode(m).IsDir() }
func (m modeInfo) Sys() any           { return nil }

// A symbolic link is refused everywhere. On Windows a reparse point Go reads
// as irregular is refused when it is a directory and left to the same-file
// check when it is a file, as a synced placeholder is; elsewhere an irregular
// entry is refused. Go clears ModeDir on a junction, so the directory bit
// comes from its attributes (TestLinkLikeReadsTheDirectoryAttribute, on
// Windows); the ModeIrregular|ModeDir row only keeps the mode check honest.
func TestLinkLike(t *testing.T) {
	saved := linkGOOS
	defer func() { linkGOOS = saved }()
	for _, c := range []struct {
		goos string
		mode fs.FileMode
		want bool
	}{
		{"linux", fs.ModeSymlink, true},
		{"linux", fs.ModeIrregular, true},
		{"linux", 0, false},
		{"linux", fs.ModeDir, false},
		{"linux", fs.ModeNamedPipe, false},
		{"windows", fs.ModeSymlink, true},
		{"windows", fs.ModeSymlink | fs.ModeDir, true},
		{"windows", fs.ModeIrregular | fs.ModeDir, true},
		{"windows", fs.ModeIrregular, false},
		{"windows", 0, false},
		{"windows", fs.ModeDir, false},
	} {
		linkGOOS = c.goos
		if got := LinkLike(modeInfo(c.mode)); got != c.want {
			t.Errorf("%s: LinkLike(%v) = %v", c.goos, c.mode, got)
		}
	}
}
