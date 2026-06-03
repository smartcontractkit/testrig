package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"sort"

	"github.com/spf13/cobra"

	"github.com/smartcontractkit/testrig/internal/runner"
)

type wdKeyType struct{}

var wdKey = wdKeyType{}

func newTraceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trace [results-dir]",
		Short: "Serve and open local trace files in Perfetto UI automatically",
		RunE: func(cmd *cobra.Command, args []string) error {
			var resultsDir string
			var err error
			if len(args) > 0 {
				path := args[0]
				info, err := os.Stat(path)
				if err == nil && !info.IsDir() {
					resultsDir = filepath.Dir(path)
				} else {
					resultsDir = path
				}
			} else {
				wd := "."
				if val, ok := cmd.Context().Value(wdKey).(string); ok {
					wd = val
				}
				resultsDir, err = findLatestResultsDir(wd)
				if err != nil {
					return err
				}
			}

			return runner.ServeTrace(cmd.Context(), resultsDir, nil, "", nil)
		},
	}
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
