//go:build windows

package conformance

import (
	"errors"
	"syscall"
)

// errorPrivilegeNotHeld is ERROR_PRIVILEGE_NOT_HELD: a symbolic link was
// refused because the user lacks the privilege to make one.
const errorPrivilegeNotHeld = syscall.Errno(1314)

// linkUnprivileged says whether os.Symlink failed only because the system
// does not let the user make a link, the one failure that gets the fallback.
func linkUnprivileged(err error) bool { return errors.Is(err, errorPrivilegeNotHeld) }
