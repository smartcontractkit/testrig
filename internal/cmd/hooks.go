package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/smartcontractkit/testrig/internal/hooks"
)

// withGlobalHooks wraps a RunE so --global-setup runs before fn and
// --global-teardown runs after, even when fn returns an error. Setup errors
// short-circuit (no teardown). Teardown errors are joined with fn's error.
func withGlobalHooks(fn func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) (err error) {
		ctx := cmd.Context()
		if runnerOpts.GlobalSetup != nil {
			if err := runnerOpts.GlobalSetup(ctx); err != nil {
				return fmt.Errorf("global setup (native): %w", err)
			}
		}
		if setup, _ := cmd.Flags().GetString("global-setup"); setup != "" {
			if setupErr := hooks.RunGlobalSetup(ctx, hooks.NewShellHook(setup)); setupErr != nil {
				return fmt.Errorf("global setup: %w", setupErr)
			}
		}
		defer func() {
			teardown, _ := cmd.Flags().GetString("global-teardown")
			if teardown != "" {
				if tdErr := hooks.RunGlobalTeardown(ctx, hooks.NewShellHook(teardown)); tdErr != nil {
					err = errors.Join(err, fmt.Errorf("global teardown: %w", tdErr))
				}
			}
			if runnerOpts.GlobalTeardown != nil {
				if tdErr := runnerOpts.GlobalTeardown(ctx); tdErr != nil {
					err = errors.Join(err, fmt.Errorf("global teardown (native): %w", tdErr))
				}
			}
		}()
		return fn(cmd, args)
	}
}
