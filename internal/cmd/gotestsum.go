package cmd

import (
	"github.com/spf13/cobra"

	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/runner"
)

var gotestsumCmd = &cobra.Command{
	Use:                "gotestsum [gotestsum flags] [-- go test flags]",
	DisableFlagParsing: true,
	Short:              "Run tests with gotestsum",
	Long:               "",
	Example:            "",
	Args:               cobra.ArbitraryArgs,
	RunE: withGlobalHooks(func(cmd *cobra.Command, args []string) (err error) {
		conf, err := config.Load(cmd)
		if err != nil {
			return err
		}
		env, cleanup, err := resourceEnv(cmd.Context(), runnerOpts)
		if err != nil {
			return err
		}
		defer func() { finishResourceCleanup(&err, cleanup) }()
		return runner.Gotestsum(cmd.Context(), conf, args, env)
	}),
}
