//go:build unix

package startup

import "io/fs"

// checkPermissions refuses a file that others may read: the permission bits
// of a Unix file are the ones the kernel checks.
func checkPermissions(perm fs.FileMode) error {
	if perm&0o004 != 0 {
		return ErrWorldReadable
	}
	return nil
}
