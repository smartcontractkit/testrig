package cmd

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/smartcontractkit/testrig/internal/output"
	"github.com/smartcontractkit/testrig/internal/runner"
)

func newDiagnoseAnalyzeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analyze <results-dir>",
		Short: "Re-run analysis on an existing diagnose results directory",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resultsDir := args[0]
			snap, err := runner.ReadRunState(resultsDir)
			if err != nil {
				return fmt.Errorf("reading run state from %s: %w", resultsDir, err)
			}
			if snap == nil || snap.Conf == nil || snap.State == nil {
				return errors.New("invalid run state snapshot")
			}

			out := output.NewFromApp(snap.Conf)

			return runner.FinishDiagnoseAnalysis(
				cmd.Context(),
				snap.Conf,
				out,
				snap.GoTestArgs,
				snap.State,
				snap.Start,
				resultsDir,
			)
		},
	}

	return cmd
}
