// Package testrig is a Go test harness for hunting flaky tests. It exposes
// lifecycle hooks (GlobalSetup/Teardown, IterationSetup/Teardown) that callers
// wire into TestMain so the same hooks work whether the test suite is invoked
// directly via go test or under the testrig diagnose CLI.
package testrig

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// Hook is a lifecycle callback. The context carries cancellation from the
// test runner — hooks should respect it for long-running operations.
type Hook func(context.Context) error

// GlobalSetup runs hook before tests execute. A nil hook is a no-op.
func GlobalSetup(ctx context.Context, hook Hook) error {
	if hook == nil {
		return nil
	}
	return hook(ctx)
}

// GlobalTeardown runs hook after tests execute. A nil hook is a no-op.
func GlobalTeardown(ctx context.Context, hook Hook) error {
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

// IterationSetup runs hook before a single diagnose iteration. A nil hook is a no-op.
func IterationSetup(ctx context.Context, hook Hook) error {
	if hook == nil {
		return nil
	}
	return hook(ctx)
}

// IterationTeardown runs hook after a single diagnose iteration. A nil hook is a no-op.
func IterationTeardown(ctx context.Context, hook Hook) error {
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
}

// Option configures the testrig CLI runner.
type Option func(*runnerOptions)

// WithGlobalSetup registers a hook to run once before any tests.
func WithGlobalSetup(h Hook) Option {
	return func(o *runnerOptions) { o.globalSetup = h }
}

// WithGlobalTeardown registers a hook to run once after all tests finish.
func WithGlobalTeardown(h Hook) Option {
	return func(o *runnerOptions) { o.globalTeardown = h }
}

// WithIterationSetup registers a hook to run before each diagnose iteration.
func WithIterationSetup(h Hook) Option {
	return func(o *runnerOptions) { o.iterationSetup = h }
}

// WithIterationTeardown registers a hook to run after each diagnose iteration.
func WithIterationTeardown(h Hook) Option {
	return func(o *runnerOptions) { o.iterationTeardown = h }
}

// RunOptions contains the evaluated configuration for the testrig CLI.
// It is exported for internal use by the CLI engine.
type RunOptions struct {
	GlobalSetup       Hook
	GlobalTeardown    Hook
	IterationSetup    Hook
	IterationTeardown Hook
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
	}
}
