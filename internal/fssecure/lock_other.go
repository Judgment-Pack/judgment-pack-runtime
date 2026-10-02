//go:build !(darwin || dragonfly || freebsd || linux || netbsd || openbsd || windows)

package fssecure

import "os"

// platformLockExclusive has no lock to take on a platform this package makes no
// assumption about, so it says so and the caller does what it does where no
// lock exists.
func platformLockExclusive(*os.File) error { return errNoLock }

// platformLockShared has no lock to take either.
func platformLockShared(*os.File) error { return errNoLock }

// platformUnlock has nothing to release.
func platformUnlock(*os.File) error { return nil }
