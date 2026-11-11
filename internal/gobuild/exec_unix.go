//go:build unix

package gobuild

import (
	"os/exec"
	"syscall"
)

func isolateProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

func interruptProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// Negative pid signals the whole process group, including compiler children.
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
}
