//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package fssecure

import (
	"errors"
	"syscall"
	"testing"
)

// What flock answers decides what the append does: only an answer that says
// the file can never be locked here lets it go ahead without the lock. Running
// out of lock records, a lock service that does not answer, and every other
// failure refuse the append instead.
func TestLockOutcomeFallsBackOnlyWhereNoLockCanExist(t *testing.T) {
	if lockOutcome(nil) != nil {
		t.Fatal("a held lock is no failure")
	}
	for _, unsupported := range []syscall.Errno{syscall.EOPNOTSUPP, syscall.ENOTSUP, syscall.ENOSYS} {
		if !errors.Is(lockOutcome(unsupported), errNoLock) {
			t.Fatalf("%v says the file cannot be locked here", unsupported)
		}
	}
	for _, failure := range []syscall.Errno{syscall.ENOLCK, syscall.EACCES, syscall.EBADF, syscall.EINVAL, syscall.EIO} {
		outcome := lockOutcome(failure)
		if errors.Is(outcome, errNoLock) || !errors.Is(outcome, failure) {
			t.Fatalf("%v fails the append rather than letting it go ahead unlocked: %v", failure, outcome)
		}
	}
}
