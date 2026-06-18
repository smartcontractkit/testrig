//go:build unix

package runner

import (
	"context"
	"errors"
	"os/exec"
	"syscall"
)

// isInterruptedIteration reports whether go test exited due to SIGINT/SIGTERM
// rather than a compile/build failure. Interrupt output is often misclassified
// as a build failure when RanTests is zero.
func isInterruptedIteration(iterErr error) bool {
	if iterErr == nil {
		return false
	}
	if errors.Is(iterErr, context.Canceled) {
		return true
	}
	var exitErr *exec.ExitError
	if !errors.As(iterErr, &exitErr) {
		return false
	}
	st, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !st.Signaled() {
		return false
	}
	sig := st.Signal()
	return sig == syscall.SIGINT || sig == syscall.SIGTERM
}
