package cmd

import (
	"bytes"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/smartcontractkit/testrig/internal/compare"
	"github.com/smartcontractkit/testrig/internal/runner"
)

func newCompareCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compare <before> <after>",
		Short: "Compare two diagnose runs and write a markdown report",
		Long: `Compares two diagnose runs and writes a markdown report to stdout or --output.

Each argument is either a results folder holding report.json, or a run name from
"diagnose --name", which resolves to diagnose-<name> in the current directory.
The report shows a before/after summary, per-test verdicts, and slow-test timings.`,
		Example: `
# Name two runs, then compare them
testrig diagnose -n before-race --iterations 30 -- -race ./...
testrig diagnose -n after-race --iterations 30 -- -race ./...
testrig compare before-race after-race -o race-compare.md
`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			wd := "."
			if val, ok := cmd.Context().Value(wdKey).(string); ok {
				wd = val
			}
			before, err := loadCompareRun(wd, args[0])
			if err != nil {
				return err
			}
			after, err := loadCompareRun(wd, args[1])
			if err != nil {
				return err
			}
			outputPath, err := cmd.Flags().GetString("output")
			if err != nil {
				return err
			}
			return writeCompareMarkdown(cmd, outputPath, before, after)
		},
	}
	cmd.Flags().StringP("output", "o", "", "write markdown to this file instead of stdout")
	return cmd
}

func loadCompareRun(wd, arg string) (compare.Run, error) {
	dir, err := compare.Resolve(wd, arg)
	if err != nil {
		return compare.Run{}, err
	}
	report, err := runner.ReadReport(dir)
	if err != nil {
		return compare.Run{}, fmt.Errorf("reading report from %s: %w", dir, err)
	}
	return compare.Run{Report: report}, nil
}

func writeCompareMarkdown(cmd *cobra.Command, outputPath string, before, after compare.Run) error {
	var buf bytes.Buffer
	if err := compare.Write(&buf, before, after); err != nil {
		return err
	}
	if outputPath != "" {
		//nolint:gosec // G306: user-chosen report path is meant to be shared
		if err := os.WriteFile(outputPath, buf.Bytes(), 0644); err != nil {
			return err
		}
		_, err := fmt.Fprintf(cmd.ErrOrStderr(), "wrote %s\n", outputPath)
		return err
	}
	_, err := cmd.OutOrStdout().Write(buf.Bytes())
	return err
}
