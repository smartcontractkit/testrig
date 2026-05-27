package cmd

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/smartcontractkit/testrig/internal/hooks"
)

// withGlobalHooks wraps a RunE so catalog global hooks run before fn and after.
// Setup errors short-circuit (no teardown). Teardown errors are joined with fn's error.
func withGlobalHooks(fn func(*cobra.Command, []string) error) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) (err error) {
		ctx := cmd.Context()
		if err := hooks.RunGlobalSetups(ctx, cmd.Flags(), runnerOpts); err != nil {
			return err
		}
		defer func() {
			if tdErr := hooks.RunGlobalTeardowns(ctx, cmd.Flags(), runnerOpts); tdErr != nil {
				err = errors.Join(err, tdErr)
			}
		}()
		return fn(cmd, args)
	}
}
