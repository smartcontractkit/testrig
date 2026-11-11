//go:build !unix

package gobuild

import (
	"os"
	"os/exec"
)

func isolateProcessGroup(cmd *exec.Cmd) {}

func interruptProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(os.Interrupt)
}
