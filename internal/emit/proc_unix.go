//go:build unix

package emit

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
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
