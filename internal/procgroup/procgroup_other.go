//go:build !unix

package procgroup

import (
	"os"
	"os/exec"
)

// Isolate is a no-op on targets that do not have process groups.
func Isolate(cmd *exec.Cmd) {}

// Interrupt sends os.Interrupt to the direct child.
func Interrupt(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(os.Interrupt)
}

// Kill kills the direct child.
func Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
