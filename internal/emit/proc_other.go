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

// endedByKill says whether the emitter's exit code is the one Process.Kill
// leaves (1, as TerminateProcess is given). Kill reaches only a process not
// yet waited for, so the kill is not sent once Wait has seen the emitter
// exit; an emitter that exits 1 by itself as the kill is sent cannot be told
// from one the kill ended.
func endedByKill(state *os.ProcessState) bool {
	return state.ExitCode() == 1
}

// endProcessGroup does nothing: the emitter has exited, and what it started
// cannot be found without process groups.
func endProcessGroup(*os.Process) {}

// waitProcessGroup does nothing: there is no group to wait for.
func waitProcessGroup(*os.Process, time.Duration) {}

// markInherited has nothing to do here: on Windows Go passes the child an
// explicit list of handles.
var markInherited = func() {}
