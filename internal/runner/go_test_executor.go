package runner

import (
	"context"
	"io"
	"os"
	"os/exec"
	"time"
)

// goTestExecutor runs one `go test -json` invocation. It is the default
// executor and the fallback whenever a run cannot use prebuilt binaries.
type goTestExecutor struct {
	moduleDir  string
	goTestArgs []string
}

func (e *goTestExecutor) Run(ctx context.Context, it iterationRun, w io.Writer) error {
	args, err := buildDiagnoseArgs(e.goTestArgs, it.ShuffleSeed)
	if err != nil {
		return err
	}
	//nolint:gosec // G204: user-controlled go test args by design
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = e.moduleDir
	cmd.Stdin = os.Stdin
	cmd.Env = append(os.Environ(), it.Env...)
	isolateDiagnoseChildProcessGroup(cmd)
	// Soft-cancel on ctx cancellation so `go test -json` gets a chance to flush
	// its final events before we escalate to SIGKILL after WaitDelay.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}
