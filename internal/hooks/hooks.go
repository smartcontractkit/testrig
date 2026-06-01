// Package hooks defines the testrig lifecycle hook interfaces and configuration.
package hooks

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Hook is a lifecycle callback. The context carries cancellation from the
// test runner — hooks should respect it for long-running operations.
type Hook func(context.Context) error

// RunGlobalSetup runs hook before tests execute. A nil hook is a no-op.
func RunGlobalSetup(ctx context.Context, hook Hook) error {
	if hook == nil {
		return nil
	}
	return hook(ctx)
}

// RunGlobalTeardown runs hook after tests execute. A nil hook is a no-op.
func RunGlobalTeardown(ctx context.Context, hook Hook) error {
	if hook == nil {
		return nil
	}
	return hook(ctx)
}

// NewShellHook returns a Hook that runs cmd via the system shell (sh -c).
// The hook respects context cancellation. On non-zero exit, the combined
// stdout+stderr is included in the returned error so failing setup commands
// are diagnosable.
func NewShellHook(cmd string) Hook {
	return func(ctx context.Context) error {
		//nolint:gosec // G204: user-provided shell command by design
		out, err := exec.CommandContext(ctx, "sh", "-c", cmd).CombinedOutput()
		if err == nil {
			return nil
		}
		if trimmed := bytes.TrimSpace(out); len(trimmed) > 0 {
			return fmt.Errorf("%w: %s", err, trimmed)
		}
		return err
	}
}

// RunIterationSetup runs hook before a single diagnose iteration. A nil hook is a no-op.
func RunIterationSetup(ctx context.Context, hook Hook) error {
	if hook == nil {
		return nil
	}
	return hook(ctx)
}

// RunIterationTeardown runs hook after a single diagnose iteration. A nil hook is a no-op.
func RunIterationTeardown(ctx context.Context, hook Hook) error {
	if hook == nil {
		return nil
	}
	return hook(ctx)
}

type runnerOptions struct {
	globalSetup       Hook
	globalTeardown    Hook
	iterationSetup    Hook
	iterationTeardown Hook
	resourceProvider  ResourceProvider
	commands          []*cobra.Command
	rootFlags         func(*pflag.FlagSet)
}

// Option configures the testrig CLI runner.
type Option func(*runnerOptions)

// GlobalSetup registers a hook to run once before any tests.
func GlobalSetup(h Hook) Option {
	return func(o *runnerOptions) { o.globalSetup = h }
}

// GlobalTeardown registers a hook to run once after all tests finish.
func GlobalTeardown(h Hook) Option {
	return func(o *runnerOptions) { o.globalTeardown = h }
}

// IterationSetup registers a hook to run before each diagnose iteration.
func IterationSetup(h Hook) Option {
	return func(o *runnerOptions) { o.iterationSetup = h }
}

// IterationTeardown registers a hook to run after each diagnose iteration.
func IterationTeardown(h Hook) Option {
	return func(o *runnerOptions) { o.iterationTeardown = h }
}

// WithResources registers a provider that supplies isolated infrastructure
// (e.g. databases) for the run. The provider is called once with the number of
// resources needed: the effective parallel-iteration count for diagnose, or 1
// for run/gotestsum.
func WithResources(p ResourceProvider) Option {
	return func(o *runnerOptions) { o.resourceProvider = p }
}

// WithCommand registers an additional subcommand on the testrig root command,
// for project-specific utilities (e.g. persistent database management). May be
// passed multiple times.
func WithCommand(cmd *cobra.Command) Option {
	return func(o *runnerOptions) { o.commands = append(o.commands, cmd) }
}

// WithRootFlags registers persistent flags on the root command, available to
// every subcommand. Use it to add consumer flags (e.g. --database-url) that a
// resource provider or custom command reads.
func WithRootFlags(register func(*pflag.FlagSet)) Option {
	return func(o *runnerOptions) { o.rootFlags = register }
}

// RunOptions contains the evaluated configuration for the testrig CLI.
// It is exported for internal use by the CLI engine.
type RunOptions struct {
	GlobalSetup       Hook
	GlobalTeardown    Hook
	IterationSetup    Hook
	IterationTeardown Hook
	ResourceProvider  ResourceProvider
	Commands          []*cobra.Command
	RootFlags         func(*pflag.FlagSet)
}

// BuildOptions evaluates the functional options and returns the internal struct.
// It is exported for internal use by the CLI engine.
func BuildOptions(opts ...Option) RunOptions {
	var o runnerOptions
	for _, opt := range opts {
		opt(&o)
	}
	return RunOptions{
		GlobalSetup:       o.globalSetup,
		GlobalTeardown:    o.globalTeardown,
		IterationSetup:    o.iterationSetup,
		IterationTeardown: o.iterationTeardown,
		ResourceProvider:  o.resourceProvider,
		Commands:          o.commands,
		RootFlags:         o.rootFlags,
	}
}
