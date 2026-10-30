// Package compare renders a markdown comparison of two diagnose runs.
package compare

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/runner"
)

// Run is one side of a comparison. Label is the run name (RunMeta.Name) or the
// results folder basename; Write falls back to the report's own metadata when
// Label is empty.
type Run struct {
	Label  string
	Report *runner.Report
}

// Resolve returns the results folder for arg: arg itself if it holds
// report.json, else diagnose-<arg> in wd. Relative paths resolve from wd so both
// candidates share one base. The error names both paths it tried.
func Resolve(wd, arg string) (string, error) {
	direct := arg
	if !filepath.IsAbs(direct) {
		direct = filepath.Join(wd, arg)
	}
	named := filepath.Join(wd, config.DiagnoseResultsNamePrefix+arg)
	for _, dir := range []string{direct, named} {
		if hasReport(dir) {
			return dir, nil
		}
	}
	return "", fmt.Errorf("no %s found in %s or %s", runner.ReportFilename, direct, named)
}

func hasReport(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, runner.ReportFilename))
	return err == nil && !info.IsDir()
}

// Write renders a markdown comparison of two diagnose reports to w.
func Write(w io.Writer, before, after Run) error {
	if w == nil {
		return fmt.Errorf("compare: writer is required")
	}
	doc := diff(before, after)
	_, err := io.WriteString(w, render(doc))
	return err
}
