//go:build unix

package startup

import "io/fs"

// checkPermissions refuses a file that others may read: the permission bits
// of a Unix file are the ones the kernel checks.
func checkPermissions(perm fs.FileMode, rules FileRules) error {
	if perm&0o004 != 0 && !rules.AllowOtherRead {
		return ErrWorldReadable
	}
	return nil
}
