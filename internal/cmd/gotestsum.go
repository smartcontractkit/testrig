package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/runner"
)

var gotestsumCmd = &cobra.Command{
	Use:                "gotestsum [gotestsum flags] [-- go test flags]",
	DisableFlagParsing: true,
	Short:              "Run tests with gotestsum",
	Long: `Runs gotestsum from the repo root.

Because this subcommand does not parse flags, global options (--ai-output) must appear
on the root command before gotestsum, for example:
  testrig --ai-output gotestsum --format=testname -- -count=1 ./...`,
	Example: `testrig gotestsum --format=dots -- -count=1 ./...
testrig --ai-output gotestsum --format=testname -- -count=1 ./...`,
	Args: cobra.ArbitraryArgs,
	RunE: withGlobalHooks(func(cmd *cobra.Command, args []string) error {
		conf, err := config.Load(cmd)
		if err != nil {
			return err
		}
		env, cleanup, err := resourceEnv(cmd.Context(), runnerOpts)
		if err != nil {
			return err
		}
		defer func() {
			if cerr := cleanup(); cerr != nil {
				fmt.Fprintf(os.Stderr, "testrig: resource cleanup failed: %v\n", cerr)
			}
		}()
		return runner.Gotestsum(cmd.Context(), conf, args, env)
	}),
}
