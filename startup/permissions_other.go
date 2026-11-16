//go:build !unix

package startup

import "io/fs"

// checkPermissions refuses every file. Outside Unix the permission bits Go
// reports are synthesized (on Windows, from the read-only attribute), so they
// cannot show that others may not read a file.
func checkPermissions(fs.FileMode) error {
	return ErrPermissionsUnknown
}
