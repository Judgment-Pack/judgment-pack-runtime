//go:build !windows

package conformance

import "io/fs"

// reparsePoint says whether a directory entry is a Windows reparse point.
// Outside Windows there are none.
func reparsePoint(fs.DirEntry) (bool, error) { return false, nil }
