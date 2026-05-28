package runner

import (
	"encoding/json"
	"path/filepath"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/smartcontractkit/testrig/internal/output"
	"github.com/smartcontractkit/testrig/internal/termstyle"
)

const aiFindingsCap = 20

type aiDiagnoseComplete struct {
	Event    string         `json:"event"`
	Report   string         `json:"report"`
	Results  string         `json:"results"`
	Summary  *ReportSummary `json:"summary"`
	Findings aiFindings     `json:"findings"`
}

type aiFindings struct {
	Broken   []aiFinding `json:"broken,omitempty"`
	Flaky    []aiFinding `json:"flaky,omitempty"`
	Timeouts []aiFinding `json:"timeouts,omitempty"`
	Slow     []aiFinding `json:"slow,omitempty"`
}

type aiFinding struct {
	Package    string `json:"package"`
	Test       string `json:"test,omitempty"`
	Fails      int    `json:"fails,omitempty"`
	Runs       int    `json:"runs,omitempty"`
	MaxElapsed string `json:"max_elapsed,omitempty"`
}

func marshalAIDiagnoseComplete(resultsDir, reportPath string, rep *Report) ([]byte, error) {
	ev := aiDiagnoseComplete{
		Event:   "complete",
		Report:  reportPath,
		Results: resultsDir,
	}
	if rep != nil {
		ev.Summary = rep.Summary
		ev.Findings = aiFindingsFromReport(rep)
	}
	return json.Marshal(ev)
}

func aiFindingsFromReport(rep *Report) aiFindings {
	if rep == nil {
		return aiFindings{}
	}
	return aiFindings{
		Broken:   aiFindingsFromEntries(rep.Failures, aiFindingFromEntry),
		Flaky:    aiFindingsFromEntries(rep.Flakes, aiFindingFromEntry),
		Timeouts: aiFindingsFromEntries(rep.Timeouts, aiFindingFromEntry),
		Slow:     aiFindingsFromEntries(rep.Slow, aiFindingFromSlowEntry),
	}
}

func aiFindingsFromEntries(entries []TestEntry, conv func(TestEntry) aiFinding) []aiFinding {
	if len(entries) == 0 {
		return nil
	}
	n := min(len(entries), aiFindingsCap)
	out := make([]aiFinding, n)
	for i := range n {
		out[i] = conv(entries[i])
	}
	return out
}

func aiFindingFromEntry(e TestEntry) aiFinding {
	runs := e.Runs
	if runs < 1 {
		runs = e.Successes + e.Fails
	}
	return aiFinding{
		Package: e.Package,
		Test:    e.Test,
		Fails:   e.Fails,
		Runs:    runs,
	}
}

func aiFindingFromSlowEntry(e TestEntry) aiFinding {
	f := aiFindingFromEntry(e)
	if e.MaxElapsed > 0 {
		f.MaxElapsed = e.MaxElapsed.Round(time.Millisecond).String()
	}
	return f
}

func renderDiagnoseArtifactsTable(resultsDir, reportPath, csvPath string) string {
	tbl := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("240"))).
		StyleFunc(diagnoseArtifactsTableStyle).
		Headers("", "Path")

	tbl.Row(termstyle.Muted.Render("results"), termstyle.Label.Render(resultsDir))
	tbl.Row(termstyle.Muted.Render("report"), termstyle.Label.Render(reportPath))
	if csvPath != "" {
		tbl.Row(termstyle.Muted.Render("csv"), termstyle.Label.Render(csvPath))
	}
	return indentBlock(tbl.String(), "  ")
}

func diagnoseArtifactsTableStyle(row, _ int) lipgloss.Style {
	if row == table.HeaderRow {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Bold(true).
			Padding(0, 1)
	}
	return lipgloss.NewStyle().Padding(0, 1)
}

func printDiagnoseArtifactsFooter(out *output.Printer, resultsDir, reportPath, csvPath string) {
	if out == nil || out.AIOutput() {
		return
	}
	rendered := renderDiagnoseArtifactsTable(resultsDir, reportPath, csvPath)
	if rendered == "" {
		return
	}
	out.HumanStderr(rendered)
}

// diagnoseCSVPath returns the CSV path when report is non-nil (WriteCSV always
// writes report.csv for a non-nil report).
func diagnoseCSVPath(resultsDir string, rep *Report) string {
	if rep == nil {
		return ""
	}
	return filepath.Join(resultsDir, "report.csv")
}
