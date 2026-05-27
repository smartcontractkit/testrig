package hooks

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/pflag"
)

// RunOrder controls native vs shell hook execution for one catalog entry.
type RunOrder uint8

const (
	// NativeThenShell runs the Go hook before the shell flag command.
	NativeThenShell RunOrder = iota
	// ShellThenNative runs the shell flag command before the Go hook.
	ShellThenNative
)

// RunEntry runs native and/or shell hooks for one catalog entry.
func RunEntry(
	ctx context.Context,
	flags *pflag.FlagSet,
	opts RunOptions,
	shellCmd string,
	e Entry,
	order RunOrder,
) error {
	runNative := func() error {
		h := opts.Hook(e.Name)
		if h == nil {
			return nil
		}
		if err := h(ctx); err != nil {
			return fmt.Errorf("%s (native): %w", e.Label(), err)
		}
		return nil
	}
	runShell := func() error {
		cmd := shellCmd
		if cmd == "" && flags != nil {
			var err error
			cmd, err = flags.GetString(e.Flag)
			if err != nil {
				return err
			}
		}
		if cmd == "" {
			return nil
		}
		if err := NewShellHook(cmd)(ctx); err != nil {
			return fmt.Errorf("%s: %w", e.Label(), err)
		}
		return nil
	}

	switch order {
	case NativeThenShell:
		if err := runNative(); err != nil {
			return err
		}
		return runShell()
	case ShellThenNative:
		if err := runShell(); err != nil {
			return err
		}
		return runNative()
	default:
		return fmt.Errorf("unknown run order %d", order)
	}
}

// RunGlobalSetups runs all global setup hooks (native then shell per entry).
func RunGlobalSetups(ctx context.Context, flags *pflag.FlagSet, opts RunOptions) error {
	for _, e := range Entries(ScopeGlobal, PhaseSetup) {
		if err := RunEntry(ctx, flags, opts, "", e, NativeThenShell); err != nil {
			return err
		}
	}
	return nil
}

// RunGlobalTeardowns runs all global teardown hooks (shell then native per entry).
func RunGlobalTeardowns(ctx context.Context, flags *pflag.FlagSet, opts RunOptions) error {
	var err error
	for _, e := range Entries(ScopeGlobal, PhaseTeardown) {
		if tdErr := RunEntry(ctx, flags, opts, "", e, ShellThenNative); tdErr != nil {
			err = errors.Join(err, tdErr)
		}
	}
	return err
}

// IterationShellCommand resolves a shell command for an iteration-scoped entry from flag values.
type IterationShellCommand func(flag string) string

// BuildIterationHook composes iteration setup or teardown hooks for diagnose.
// Returns nil when no native or shell hooks are configured for that phase.
func BuildIterationHook(opts RunOptions, shell IterationShellCommand, phase Phase) Hook {
	entries := Entries(ScopeIteration, phase)
	if !iterationPhaseActive(opts, shell, entries) {
		return nil
	}
	order := NativeThenShell
	if phase == PhaseTeardown {
		order = ShellThenNative
	}
	return func(ctx context.Context) error {
		for _, e := range entries {
			if err := RunEntry(ctx, nil, opts, shell(e.Flag), e, order); err != nil {
				return err
			}
		}
		return nil
	}
}

func iterationPhaseActive(opts RunOptions, shell IterationShellCommand, entries []Entry) bool {
	for _, e := range entries {
		if opts.Hook(e.Name) != nil {
			return true
		}
		if shell != nil && shell(e.Flag) != "" {
			return true
		}
	}
	return false
}
