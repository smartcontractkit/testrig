package cmd

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/hooks"
	"github.com/smartcontractkit/testrig/internal/output"
	"github.com/smartcontractkit/testrig/internal/runner"
)

func newDiagnoseCmd(runnerOpts hooks.RunOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diagnose [--diagnose flags] [-- go test flags]",
		Short: "Run tests multiple times to hunt down flakes, races, timeouts, and more",
		Long: `Runs tests multiple times to hunt down flakes, races, timeouts, and more.

Pass every flag and package pattern you want forwarded to go test after "--". The harness
prepends "go test -json" (duplicate -json in your arguments is ignored) and adds "-count=1"
per iteration. Do not pass -count in go test flags; use diagnose --iterations for repetition.
Do not pass go test -trace; use diagnose --trace instead. With --shuffle-seed, a per-iteration
-shuffle=<seed> is appended and recorded in report.json.`,
		Example: "",
		Args:    cobra.MinimumNArgs(1),
		RunE: withGlobalHooks(runnerOpts, func(cmd *cobra.Command, args []string) (err error) {
			conf, err := config.Load(cmd)
			if err != nil {
				return err
			}
			out := output.NewFromApp(conf)

			if err = config.ValidateDiagnose(conf); err != nil {
				return err
			}

			if err = runner.WarnDiagnoseGoTestCount(args); err != nil {
				return err
			}
			if err = runner.WarnDiagnoseGoTestTrace(args); err != nil {
				return err
			}

			resources, cleanup, err := provisionResources(
				cmd.Context(),
				runnerOpts,
				runner.EffectiveParallelIterations(conf),
			)
			if err != nil {
				return err
			}
			defer func() { finishResourceCleanup(cmd, &err, cleanup) }()

			shell := conf.ShellCommand
			iterSetup := hooks.BuildIterationHook(runnerOpts, shell, hooks.PhaseSetup)
			iterTeardown := hooks.BuildIterationHook(runnerOpts, shell, hooks.PhaseTeardown)

			return runner.Diagnose(cmd.Context(), conf, out, args, resources, iterSetup, iterTeardown)
		}),
	}

	cmd.Flags().Int("iterations", 1, "number of full test runs")
	cmd.Flags().Int("parallel-iterations", 1, "maximum number of diagnose iterations to run concurrently")
	cmd.Flags().
		Duration("slow-threshold", 30*time.Second, "tests whose max Elapsed exceeds this are flagged slow")
	cmd.Flags().Bool("fail-fast", false, "stop this diagnose run immediately if any iteration fails")
	cmd.Flags().
		StringSlice("fail-fast-on", nil, `stop this diagnose run immediately when an iteration matches one or more categories: "failure", "timeout", "slow", or "any"`)
	cmd.Flags().
		Bool("shuffle-seed", false, "randomize test order each iteration; a unique seed is generated per iteration and recorded in report.json for reproduction")
	cmd.Flags().
		Bool("trace", false, "visualize the test execution traces")

	return cmd
}
