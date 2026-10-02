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
// A wait a signal interrupts is asked again, and what the call answered is
// then read by lockOutcome.
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
	return lockOutcome(lockErr)
}

// lockOutcome reads what flock answered: nil when the lock is held, errNoLock
// when this file can never be locked here, and the error itself for anything
// else, which fails the append.
//
// Only an answer that says the lock is not supported, and will not be on a
// later attempt, is errNoLock, because the caller then appends without it, and
// an append without the lock while another writer holds it can break that
// writer's chain:
//
//   - EOPNOTSUPP, and ENOTSUP where it is a different number (Darwin): the
//     descriptor's file system does not support flock, as a BSD or Darwin
//     mount without locking answers.
//   - ENOSYS: the system call is not implemented at all, as some sandboxes and
//     emulators answer.
//
// ENOLCK is not among them. Linux answers it when the kernel has run out of
// lock records, and an NFS client when the lock service does not answer: both
// can pass, and neither says the file cannot be locked, so the append is
// refused rather than made without the lock.
func lockOutcome(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.EOPNOTSUPP) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.ENOSYS) {
		return errNoLock
	}
	return err
}

// platformLockShared takes flock(2)'s shared lock, waiting for any writer that
// holds the exclusive one, and reads the answer as platformLockExclusive does.
// Shared holders exclude writers and not one another.
func platformLockShared(file *os.File) error {
	conn, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var lockErr error
	if err := conn.Control(func(fd uintptr) {
		for {
			lockErr = syscall.Flock(int(fd), syscall.LOCK_SH)
			if !errors.Is(lockErr, syscall.EINTR) {
				return
			}
		}
	}); err != nil {
		return err
	}
	return lockOutcome(lockErr)
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
