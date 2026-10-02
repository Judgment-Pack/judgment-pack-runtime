//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package fssecure

import (
	"errors"
	"os"
	"syscall"
)

// platformLockExclusive takes flock(2)'s exclusive lock on an open file,
// waiting for it. The lock belongs to the open file description, not to the
// process, so two opens of one trail exclude each other whether they are in two
// processes or in one: that is why it is flock and not a POSIX record lock,
// which a process holds once however many descriptors it has and drops when it
// closes any of them. It is advisory, which is what "cooperative" means here:
// it excludes every writer that asks for it and no one that does not.
//
// A file system that offers no such lock answers ENOLCK, EOPNOTSUPP or ENOSYS,
// and that is reported as errNoLock rather than as a failure, so the caller can
// do what it does where no lock exists at all. A wait a signal interrupts is
// asked again.
func platformLockExclusive(file *os.File) error {
	conn, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var lockErr error
	if err := conn.Control(func(fd uintptr) {
		for {
			lockErr = syscall.Flock(int(fd), syscall.LOCK_EX)
			if !errors.Is(lockErr, syscall.EINTR) {
				return
			}
		}
	}); err != nil {
		return err
	}
	if errors.Is(lockErr, syscall.ENOLCK) || errors.Is(lockErr, syscall.EOPNOTSUPP) || errors.Is(lockErr, syscall.ENOSYS) {
		return errNoLock
	}
	return lockErr
}

// platformUnlock releases the lock platformLockExclusive took. Closing the file
// releases it too; this says so before the close rather than relying on it.
func platformUnlock(file *os.File) error {
	conn, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var unlockErr error
	if err := conn.Control(func(fd uintptr) {
		unlockErr = syscall.Flock(int(fd), syscall.LOCK_UN)
	}); err != nil {
		return err
	}
	return unlockErr
}
