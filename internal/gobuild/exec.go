package gobuild

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"
)

// runGo runs a go command with the caller's environment. Cancellation sends an
// interrupt to the child process group, then escalates after WaitDelay.
func runGo(ctx context.Context, dir string, args []string, stdout, stderr io.Writer) error {
	//nolint:gosec // G204: go command arguments come from the user's go test invocation
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = os.Environ()
	isolateProcessGroup(cmd)
	cmd.Cancel = func() error { return interruptProcessGroup(cmd) }
	cmd.WaitDelay = 5 * time.Second
	return cmd.Run()
}
