//go:build !windows

package fssecure

import "io/fs"

// directoryAttribute has no Windows attributes to read here.
func directoryAttribute(fs.FileInfo) bool { return false }
