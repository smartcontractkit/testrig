package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/runner"
)

var runCmd = &cobra.Command{
	Use:                "run [go test flags]",
	DisableFlagParsing: true,
	Short:              "Run go test; all flags and args are passed through",
	Long: `Runs go test from the repo root.

Because this subcommand does not parse flags, global options (--ai-output) must appear
on the root command before run, for example:
  testrig --ai-output run -v -count=1 ./...`,
	Example: `  testrig run -v -count=1 -p 4 ./...
  testrig run -count=1 ./...`,
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
		return runner.GoTest(cmd.Context(), conf, args, env)
	}),
}
