//go:build unix

package startup

import (
	"io/fs"
	"syscall"
)

// openNoWait is the flag ReadFile opens a file with. A named pipe put at the
// path after the check and before the open would make a plain open wait for
// a writer; opened without blocking, it is found on the opened file and
// refused. A regular file reads the same either way.
const openNoWait = syscall.O_NONBLOCK

// checkPermissions refuses a file that others may read, unless the rules
// allow it, and a file that others may write, always: the permission bits of
// a Unix file are the ones the kernel checks.
func checkPermissions(perm fs.FileMode, rules FileRules) error {
	if perm&0o004 != 0 && !rules.AllowOtherRead {
		return ErrWorldReadable
	}
	if perm&0o002 != 0 {
		return ErrWorldWritable
	}
	return nil
}
