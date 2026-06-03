package cmd

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/smartcontractkit/testrig/internal/hooks"
)

// withGlobalHooks wraps a RunE so catalog global hooks run before fn and after.
// Setup errors short-circuit (no teardown). Teardown errors are joined with fn's error.
func withGlobalHooks(
	runnerOpts hooks.RunOptions,
	fn func(*cobra.Command, []string) error,
) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) (err error) {
		ctx := cmd.Context()
		flags := cmd.Root().PersistentFlags()
		if err := hooks.RunGlobalSetups(ctx, flags, runnerOpts); err != nil {
			return err
		}
		defer func() {
			if tdErr := hooks.RunGlobalTeardowns(ctx, flags, runnerOpts); tdErr != nil {
				err = errors.Join(err, tdErr)
			}
		}()
		return fn(cmd, args)
	}
}
