//go:build unix

package emit

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// ownProcessGroup starts the emitter as the leader of a new process group,
// so that what it starts is ended with it.
func ownProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGKILL to the emitter's whole process group.
func killProcessGroup(process *os.Process) error {
	if process == nil {
		return nil
	}
	err := syscall.Kill(-process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

// endProcessGroup kills what is left of the emitter's process group after
// the emitter has exited and been waited for. The group outlives its leader
// while a member is left, so its id is not reused until then. When no member
// is left, the id is free once the emitter is reaped, and a process started
// between the wait and this kill could take it and lead a group of that id,
// which the kill would then reach. The window is the few instructions
// between the two calls. Closing it means waiting without reaping (waitid
// with WNOWAIT), which the syscall package does not offer on every Unix, so
// it is left open.
func endProcessGroup(process *os.Process) {
	_ = killProcessGroup(process)
}

// groupPoll is the pause between two looks at whether the group is gone.
const groupPoll = 10 * time.Millisecond

// waitProcessGroup waits, up to bound, until no process of the emitter's
// group is left: a SIGKILL is delivered asynchronously, and a killed member
// is a group member until it is reaped. Signal 0 tells; ESRCH is gone, and
// any other answer, EPERM included, is still present. When the bound is
// over the wait ends, and the run goes on as it would have.
func waitProcessGroup(process *os.Process, bound time.Duration) {
	if process == nil {
		return
	}
	deadline := time.Now().Add(bound)
	for !errors.Is(syscall.Kill(-process.Pid, 0), syscall.ESRCH) && time.Now().Before(deadline) {
		time.Sleep(groupPoll)
	}
}
