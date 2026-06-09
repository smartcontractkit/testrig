package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/smartcontractkit/testrig/internal/runner"
)

type wdKeyType struct{}

var wdKey = wdKeyType{}

func newTraceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trace [results-dir-or-file]",
		Short: "Open test trace visualization",
		Example: `
# Open the trace visualization for the latest 'diagnose' run
testrig trace

# Open the trace visualization for a specific 'diagnose' run
testrig trace diagnose-20260604T120000Z
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var resultsDir string
			if len(args) == 0 {
				wd := "."
				if val, ok := cmd.Context().Value(wdKey).(string); ok {
					wd = val
				}
				var err error
				resultsDir, err = findLatestResultsDir(wd)
				if err != nil {
					return err
				}
			} else {
				path := args[0]
				info, err := os.Stat(path)
				if err != nil {
					return err
				}
				if info.IsDir() {
					resultsDir = path
				} else {
					return fmt.Errorf("trace path must be a directory: %s", path)
				}
			}

			matches, err := filepath.Glob(filepath.Join(resultsDir, "trace-*.json"))
			if err != nil || len(matches) == 0 {
				return fmt.Errorf("no trace-*.json files found in %s", resultsDir)
			}

			var traceFiles []string
			for _, m := range matches {
				traceFiles = append(traceFiles, filepath.Base(m))
			}

			return runner.ServeTrace(cmd.Context(), resultsDir, traceFiles, nil, runner.TraceServeOptions{})
		},
	}
	return cmd
}

func findLatestResultsDir(wd string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(wd, "diagnose-*"))
	if err != nil {
		return "", err
	}

	var dirs []string
	for _, m := range matches {
		info, err := os.Stat(m)
		if err == nil && info.IsDir() {
			dirs = append(dirs, m)
		}
	}

	if len(dirs) == 0 {
		return "", errors.New("no diagnose results directory found")
	}

	sort.Slice(dirs, func(i, j int) bool {
		infoI, errI := os.Stat(dirs[i])
		infoJ, errJ := os.Stat(dirs[j])
		if errI != nil || errJ != nil {
			return dirs[i] < dirs[j]
		}
		return infoI.ModTime().Before(infoJ.ModTime())
	})
	return dirs[len(dirs)-1], nil
}
