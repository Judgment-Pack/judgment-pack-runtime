//go:build unix

package audit

import (
	"os"
	"syscall"
)

// keyAccessCheck holds an opened signing key to its owner alone: owned by the
// user this runtime runs as, and neither readable nor writable by its group or
// by others. A key another user owns is one that user can read whatever its
// mode says, and a key others can read is one they can sign with.
func keyAccessCheck(info os.FileInfo) error {
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || int(stat.Uid) != os.Geteuid() {
		return ErrKeyNotOwned
	}
	if info.Mode().Perm()&0o077 != 0 {
		return ErrKeyTooOpen
	}
	return nil
}
