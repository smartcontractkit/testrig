// Package testrig is a Go test harness for hunting flaky tests. It exposes
// lifecycle hooks (GlobalSetup/Teardown, IterationSetup/Teardown) that callers
// wire into TestMain so the same hooks work whether the test suite is invoked
// directly via go test or under the testrig diagnose CLI.
package testrig

import (
	"github.com/smartcontractkit/testrig/internal/cmd"
	"github.com/smartcontractkit/testrig/internal/hooks"
)

// Hook is a lifecycle callback. The context carries cancellation from the
// test runner — hooks should respect it for long-running operations.
type Hook = hooks.Hook

// Option configures the testrig CLI runner.
type Option = hooks.Option

// RunOptions contains the evaluated configuration for the testrig CLI.
// It is exported for internal use by the CLI engine.
type RunOptions = hooks.RunOptions

// GlobalSetup registers a hook to run once before any tests.
func GlobalSetup(h Hook) Option {
	return hooks.GlobalSetup(h)
}

// GlobalTeardown registers a hook to run once after all tests finish.
func GlobalTeardown(h Hook) Option {
	return hooks.GlobalTeardown(h)
}

// NewShellHook returns a Hook that runs cmdStr via the system shell (sh -c).
// The hook respects context cancellation. On non-zero exit, the combined
// stdout+stderr is included in the returned error so failing setup commands
// are diagnosable.
func NewShellHook(cmdStr string) Hook {
	return hooks.NewShellHook(cmdStr)
}

// IterationSetup registers a hook to run before each diagnose iteration.
func IterationSetup(h Hook) Option {
	return hooks.IterationSetup(h)
}

// IterationTeardown registers a hook to run after each diagnose iteration.
func IterationTeardown(h Hook) Option {
	return hooks.IterationTeardown(h)
}

// BuildOptions evaluates the functional options and returns the internal struct.
// It is exported for internal use by the CLI engine.
func BuildOptions(opts ...Option) RunOptions {
	return hooks.BuildOptions(opts...)
}

// Run is the entry point for running the testrig test suite from a test binary.
// Calling Run executes the testrig CLI engine and will call os.Exit upon completion.
func Run(opts ...Option) {
	cmd.Execute(opts...)
}
