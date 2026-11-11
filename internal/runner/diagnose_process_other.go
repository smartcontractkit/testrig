//go:build !unix

package runner

import (
	"os"
	"os/exec"
)

func isolateDiagnoseChildProcessGroup(cmd *exec.Cmd) {}

func interruptDiagnoseChildProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(os.Interrupt)
}

func killDiagnoseChildProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
