//go:build unix

package runner

import (
	"os/exec"
	"syscall"
)

// isolateDiagnoseChildProcessGroup puts each go test child in its own process
// group so a terminal SIGINT (first Ctrl+C) stops testrig only, not in-flight
// test runs. Hard cancel still signals the child explicitly via cmd.Cancel.
func isolateDiagnoseChildProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}
