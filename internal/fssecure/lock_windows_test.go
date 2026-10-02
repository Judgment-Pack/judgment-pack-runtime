//go:build windows

package fssecure

import (
	"errors"
	"syscall"
	"testing"
)

// What LockFileEx answers decides what the append does: only a file system
// that implements no byte-range lock lets it go ahead without one.
func TestLockOutcomeFallsBackOnlyWhereNoLockCanExist(t *testing.T) {
	if lockOutcome(nil) != nil {
		t.Fatal("a held lock is no failure")
	}
	for _, unsupported := range []syscall.Errno{errorNotSupported, errorInvalidFunction} {
		if !errors.Is(lockOutcome(unsupported), errNoLock) {
			t.Fatalf("%v says the file cannot be locked here", unsupported)
		}
	}
	// ERROR_NOT_ENOUGH_MEMORY, ERROR_NO_SYSTEM_RESOURCES, ERROR_ACCESS_DENIED,
	// ERROR_LOCK_VIOLATION.
	for _, failure := range []syscall.Errno{8, 1450, 5, 33} {
		outcome := lockOutcome(failure)
		if errors.Is(outcome, errNoLock) || !errors.Is(outcome, failure) {
			t.Fatalf("%v fails the append rather than letting it go ahead unlocked: %v", failure, outcome)
		}
	}
}
