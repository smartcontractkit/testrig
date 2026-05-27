package cmd

import (
	"context"
	"errors"
	"time"

	"github.com/spf13/cobra"

	testrig "github.com/smartcontractkit/testrig"
	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/output"
	"github.com/smartcontractkit/testrig/internal/runner"
)

var diagnoseCmd = &cobra.Command{
	Use:   "diagnose [--diagnose flags] [-- go test flags]",
	Short: "Run tests multiple times to hunt down flakes, races, timeouts, and more",
	Long: `Runs tests multiple times to hunt down flakes, races, timeouts, and more.

Pass every flag and package pattern you want forwarded to go test after "--". The harness
prepends "go test -json" (duplicate -json in your arguments is ignored) and adds "-count=1"
when you omit -count or use -count=1. Prefer diagnose --iterations for repetition; you may
use -count>1 to repeat inside one go test invocation. With --shuffle-seed, a per-iteration
-shuffle=<seed> is appended.`,
	Example: `# Run the full test suite 10 times.
testrig diagnose --iterations 10 -- ./...`,
	Args: cobra.MinimumNArgs(1),
	RunE: withGlobalHooks(func(cmd *cobra.Command, args []string) error {
		conf, err := config.Load(cmd)
		if err != nil {
			return err
		}
		out := output.NewFromApp(conf)

		if err = config.ValidateDiagnose(conf); err != nil {
			return err
		}

		if err = runner.WarnDiagnoseGoTestCount(out.HumanStderrWriter(), args); err != nil {
			return err
		}

		var iterSetup, iterTeardown testrig.Hook

		if runnerOpts.IterationSetup != nil || conf.IterationSetup != "" {
			iterSetup = func(ctx context.Context) error {
				if runnerOpts.IterationSetup != nil {
					if err := runnerOpts.IterationSetup(ctx); err != nil {
						return err
					}
				}
				if conf.IterationSetup != "" {
					return testrig.NewShellHook(conf.IterationSetup)(ctx)
				}
				return nil
			}
		}

		if runnerOpts.IterationTeardown != nil || conf.IterationTeardown != "" {
			iterTeardown = func(ctx context.Context) error {
				var err error
				if conf.IterationTeardown != "" {
					if tdErr := testrig.NewShellHook(conf.IterationTeardown)(ctx); tdErr != nil {
						err = errors.Join(err, tdErr)
					}
				}
				if runnerOpts.IterationTeardown != nil {
					if tdErr := runnerOpts.IterationTeardown(ctx); tdErr != nil {
						err = errors.Join(err, tdErr)
					}
				}
				return err
			}
		}

		return runner.Diagnose(cmd.Context(), conf, out, args, iterSetup, iterTeardown)
	}),
}

func init() {
	diagnoseCmd.Flags().Int("iterations", 1, "number of full test runs")
	diagnoseCmd.Flags().Int("parallel-iterations", 1, "maximum number of diagnose iterations to run concurrently")
	diagnoseCmd.Flags().
		Duration("slow-threshold", 30*time.Second, "tests whose max Elapsed exceeds this are flagged slow")
	diagnoseCmd.Flags().Bool("fail-fast", false, "stop this diagnose run immediately if any iteration fails")
	diagnoseCmd.Flags().
		StringSlice("fail-fast-on", nil, `stop this diagnose run immediately when an iteration matches one or more categories: "failure", "timeout", "slow", or "any"`)
	diagnoseCmd.Flags().
		Bool("shuffle-seed", false, "randomize test order each iteration; a unique seed is generated per iteration and recorded in report.json for reproduction")
}
