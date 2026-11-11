//go:build unix

package procgroup

import (
	"os/exec"
	"syscall"
)

// Isolate puts cmd in its own process group so a terminal SIGINT stops testrig
// only, not in-flight child work. Hard cancel still signals the group
// explicitly through Interrupt or Kill.
func Isolate(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// Interrupt sends SIGINT to cmd's whole process group, so a child and its
// descendants get the chance to flush output.
func Interrupt(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// Negative pid signals the whole process group, including grandchildren.
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
}

// Kill force-kills cmd's whole process group.
func Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
