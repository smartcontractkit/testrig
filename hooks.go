// Package testrig is a Go test harness for hunting flaky tests. It exposes
// lifecycle hooks (GlobalSetup/Teardown, IterationSetup/Teardown) that callers
// wire into TestMain so the same hooks work whether the test suite is invoked
// directly via go test or under the testrig diagnose CLI.
package testrig

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

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

// Resource is one prepared, isolated piece of infrastructure (e.g. a database)
// supplied by a ResourceProvider. See hooks.Resource for field semantics.
type Resource = hooks.Resource

// ResourceProvider supplies isolated resources for a run. See WithResources.
type ResourceProvider = hooks.ResourceProvider

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

// WithResources registers a provider that supplies isolated infrastructure
// (e.g. databases) for the run. The provider is called once with the number of
// resources needed: the effective parallel-iteration count for diagnose, or 1
// for run/gotestsum. Each resource's Env is applied to the child go test
// process; Reset, DumpDiagnostics, and Cleanup are invoked over its lifecycle.
func WithResources(p ResourceProvider) Option {
	return hooks.WithResources(p)
}

// WithCommand registers an additional subcommand on the testrig root command,
// for project-specific utilities (e.g. persistent database management).
func WithCommand(cmd *cobra.Command) Option {
	return hooks.WithCommand(cmd)
}

// WithRootFlags registers persistent flags on the root command, available to
// every subcommand. Use it to add consumer flags (e.g. --database-url) that a
// resource provider or custom command reads.
func WithRootFlags(register func(*pflag.FlagSet)) Option {
	return hooks.WithRootFlags(register)
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
