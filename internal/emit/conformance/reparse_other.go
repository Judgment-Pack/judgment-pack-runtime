//go:build !windows

package conformance

import "io/fs"

// reparsePoint says whether a directory entry is a Windows reparse point.
// Outside Windows there are none. It is a variable so that tests can report
// one.
var reparsePoint = func(fs.DirEntry) (bool, error) { return false, nil }
