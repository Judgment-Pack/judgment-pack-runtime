//go:build !unix

package emit

import (
	"os"
	"os/exec"
	"time"
)

// ownProcessGroup does nothing where process groups are not portable; the
// emitter itself is killed at the time limit, and what it started may not be.
func ownProcessGroup(*exec.Cmd) {}

// killProcessGroup kills the emitter's own process.
func killProcessGroup(process *os.Process) error {
	if process == nil {
		return nil
	}
	return process.Kill()
}

// endProcessGroup does nothing: the emitter has exited, and what it started
// cannot be found without process groups.
func endProcessGroup(*os.Process) {}

// waitProcessGroup does nothing: there is no group to wait for.
func waitProcessGroup(*os.Process, time.Duration) {}
