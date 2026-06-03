package cmd

import (
	"github.com/spf13/cobra"

	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/hooks"
	"github.com/smartcontractkit/testrig/internal/runner"
)

func newGotestsumCmd(runnerOpts hooks.RunOptions) *cobra.Command {
	return &cobra.Command{
		Use:                "gotestsum [gotestsum flags] [-- go test flags]",
		DisableFlagParsing: true,
		Short:              "Run tests with gotestsum",
		Long:               "",
		Example:            "",
		Args:               cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			return runRootAfterParsing(cmd, args, runnerOpts, func(gotestsumArgs []string) error {
				conf, err := config.Load(cmd)
				if err != nil {
					return err
				}
				env, cleanup, err := resourceEnv(cmd.Context(), runnerOpts)
				if err != nil {
					return err
				}
				defer func() { finishResourceCleanup(cmd, &err, cleanup) }()
				return runner.Gotestsum(cmd.Context(), conf, gotestsumArgs, env)
			})
		},
	}
}
