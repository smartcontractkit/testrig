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
		Short: "Open Perfetto trace visualization",
		Long:  `Open the Perfetto trace visualization for a 'diagnose' run.`,
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
					if filepath.Base(path) == "trace.json" {
						resultsDir = filepath.Dir(path)
					} else {
						return fmt.Errorf("trace file must be named trace.json: %s", path)
					}
				}
			}

			traceJSON := filepath.Join(resultsDir, "trace.json")
			if _, err := os.Stat(traceJSON); err != nil {
				return fmt.Errorf("trace.json not found in %s", resultsDir)
			}
			return runner.ServeTrace(cmd.Context(), resultsDir, nil, runner.TraceServeOptions{})
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
