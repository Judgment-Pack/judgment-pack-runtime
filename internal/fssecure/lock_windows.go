//go:build windows

package fssecure

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// kernel32 is a KnownDLL, so loading it by name always loads the system's own.
var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

const lockfileExclusiveLock = 0x00000002

// The one byte every cooperating writer locks. A Windows byte-range lock is
// mandatory for the range it covers: another handle cannot read or write those
// bytes while it is held. So the lock is taken on a byte no trail reaches, at
// the far end of the offset range, and a reader of the trail is never refused
// by a writer holding it. Locking past the end of a file is not an error.
const (
	lockOffsetLow  = 0xFFFFFFFE
	lockOffsetHigh = 0x7FFFFFFF
)

// ERROR_NOT_SUPPORTED and ERROR_INVALID_FUNCTION are what a file system that
// implements no byte-range lock answers, on every attempt.
const (
	errorInvalidFunction syscall.Errno = 1
	errorNotSupported    syscall.Errno = 50
)

// platformLockExclusive takes LockFileEx's exclusive lock on the agreed byte,
// waiting for it. The lock belongs to the handle, so two opens of one trail
// exclude each other whether they are in two processes or in one. Only a
// writer that asks for it is excluded, which is what "cooperative" means here.
func platformLockExclusive(file *os.File) error {
	conn, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var lockErr error
	if err := conn.Control(func(fd uintptr) {
		overlapped := &syscall.Overlapped{Offset: lockOffsetLow, OffsetHigh: lockOffsetHigh}
		r1, _, e1 := procLockFileEx.Call(fd, lockfileExclusiveLock, 0, 1, 0, uintptr(unsafe.Pointer(overlapped)))
		if r1 == 0 {
			lockErr = e1
		}
	}); err != nil {
		return err
	}
	return lockOutcome(lockErr)
}

// lockOutcome reads what LockFileEx answered: nil when the lock is held,
// errNoLock when the file system implements no byte-range lock (it answers
// ERROR_NOT_SUPPORTED or ERROR_INVALID_FUNCTION, and will on every attempt),
// and the error itself for anything else, which fails the append: an answer
// that can pass, such as running out of memory or resources, must not let a
// writer append without the lock while another holds it.
func lockOutcome(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errorNotSupported) || errors.Is(err, errorInvalidFunction) {
		return errNoLock
	}
	return err
}

// platformLockShared takes LockFileEx's shared lock on the same byte, waiting
// for any writer that holds it exclusively. Shared holders exclude writers and
// not one another.
func platformLockShared(file *os.File) error {
	conn, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var lockErr error
	if err := conn.Control(func(fd uintptr) {
		overlapped := &syscall.Overlapped{Offset: lockOffsetLow, OffsetHigh: lockOffsetHigh}
		r1, _, e1 := procLockFileEx.Call(fd, 0, 0, 1, 0, uintptr(unsafe.Pointer(overlapped)))
		if r1 == 0 {
			lockErr = e1
		}
	}); err != nil {
		return err
	}
	return lockOutcome(lockErr)
}

// platformUnlock releases the byte platformLockExclusive locked. Windows
// releases a closed handle's locks in its own time, so it is said explicitly.
func platformUnlock(file *os.File) error {
	conn, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var unlockErr error
	if err := conn.Control(func(fd uintptr) {
		overlapped := &syscall.Overlapped{Offset: lockOffsetLow, OffsetHigh: lockOffsetHigh}
		r1, _, e1 := procUnlockFileEx.Call(fd, 0, 1, 0, uintptr(unsafe.Pointer(overlapped)))
		if r1 == 0 {
			unlockErr = e1
		}
	}); err != nil {
		return err
	}
	return unlockErr
}
