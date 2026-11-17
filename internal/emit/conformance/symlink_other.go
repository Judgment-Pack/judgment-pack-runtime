//go:build !windows

package conformance

// linkUnprivileged says whether os.Symlink failed only because the system
// does not let the user make a link, the one failure that gets the fallback.
// Outside Windows no failure is that one.
func linkUnprivileged(error) bool { return false }
