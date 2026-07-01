package runner

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/smartcontractkit/testrig/internal/output"
	"github.com/smartcontractkit/testrig/internal/termstyle"
)

// DiagnoseTableOpts configures the streaming diagnose iteration table.
type DiagnoseTableOpts struct {
	RaceEnabled bool
}

// Fixed column widths for streaming rows (each row is formatted independently).
const (
	diagnoseColIter       = 5
	diagnoseColResult     = 8
	diagnoseColResultRace = 12
	diagnoseColTests      = 8
	diagnoseColCount      = 8
	diagnoseColRuntime    = 10
)

func printDiagnoseIterationTableHeader(out *output.Printer, opts DiagnoseTableOpts) {
	if out.AIOutput() {
		return
	}
	header := diagnoseTableHeaderPlain(opts)
	out.HumanStderr(termstyle.Muted.Render(header))
	out.HumanStderr(termstyle.Muted.Render(strings.Repeat("─", len(header))))
}

func diagnoseTableHeaderPlain(opts DiagnoseTableOpts) string {
	if opts.RaceEnabled {
		return fmt.Sprintf("%5s  %-12s  %8s  %8s  %8s  %8s  %8s  %8s  %10s",
			"Iter", "Result", "Tests", "Skipped", "Failures", "Races", "Timeouts", "Slow", "Runtime")
	}
	return fmt.Sprintf("%5s  %-8s  %8s  %8s  %8s  %8s  %8s  %10s",
		"Iter", "Result", "Tests", "Skipped", "Failures", "Timeouts", "Slow", "Runtime")
}

func formatDiagnoseIterationTableRow(iter int, d IterationDigest, dur time.Duration, opts DiagnoseTableOpts) string {
	resultWidth := diagnoseColResult
	if opts.RaceEnabled {
		resultWidth = diagnoseColResultRace
	}
	iterCol := lipgloss.PlaceHorizontal(diagnoseColIter, lipgloss.Right, termstyle.Label.Render(strconv.Itoa(iter)))
	resCol := lipgloss.PlaceHorizontal(resultWidth, lipgloss.Left, renderIterationResultHuman(d.Result))
	testsCol := lipgloss.PlaceHorizontal(
		diagnoseColTests,
		lipgloss.Right,
		termstyle.Muted.Render(strconv.Itoa(d.RanTests)),
	)
	skipCol := lipgloss.PlaceHorizontal(diagnoseColCount, lipgloss.Right, diagnoseTableCountStyled(d.SkipTests, "skip"))
	failCol := lipgloss.PlaceHorizontal(diagnoseColCount, lipgloss.Right, diagnoseTableCountStyled(d.FailTests, "fail"))
	gap := "  "
	parts := []string{iterCol, gap, resCol, gap, testsCol, gap, skipCol, gap, failCol}
	if opts.RaceEnabled {
		raceCol := lipgloss.PlaceHorizontal(diagnoseColCount, lipgloss.Right, diagnoseTableCountStyled(d.Races, "race"))
		parts = append(parts, gap, raceCol)
	}
	toCol := lipgloss.PlaceHorizontal(
		diagnoseColCount,
		lipgloss.Right,
		diagnoseTableCountStyled(d.TimeoutTests, "timeout"),
	)
	slowCol := lipgloss.PlaceHorizontal(diagnoseColCount, lipgloss.Right, diagnoseTableCountStyled(d.SlowTests, "slow"))
	rt := termstyle.Muted.Render(dur.Round(time.Second).String())
	rtCol := lipgloss.PlaceHorizontal(diagnoseColRuntime, lipgloss.Right, rt)
	parts = append(parts, gap, toCol, gap, slowCol, gap, rtCol)
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

func diagnoseTableCountStyled(n int, kind string) string {
	s := strconv.Itoa(n)
	switch kind {
	case "fail", "timeout":
		if n == 0 {
			return termstyle.OK.Render(s)
		}
		return termstyle.Bad.Render(s)
	case "race":
		if n == 0 {
			return termstyle.OK.Render(s)
		}
		return termstyle.Flaky.Render(s)
	case "slow":
		if n == 0 {
			return termstyle.OK.Render(s)
		}
		return termstyle.Flaky.Render(s)
	case "skip":
		return termstyle.Muted.Render(s)
	default:
		return termstyle.Muted.Render(s)
	}
}

// formatDiagnoseProblemTestsSuffix appends " (TestA, TestB, +N)" after a table row,
// fitting within cols when combined with baseRow. renderName styles each test name (e.g. red).
func formatDiagnoseProblemTestsSuffix(
	names []string,
	cols int,
	baseRow string,
	renderName func(...string) string,
) string {
	if len(names) == 0 {
		return ""
	}
	if cols < 1 {
		cols = 80
	}
	budget := cols - ansi.StringWidth(baseRow)
	if budget <= 0 {
		return ""
	}

	for shown := len(names); shown >= 0; shown-- {
		hidden := len(names) - shown
		suffix := buildDiagnoseProblemTestsSuffix(names[:shown], hidden, renderName)
		if suffix == "" {
			continue
		}
		if ansi.StringWidth(suffix) <= budget {
			return suffix
		}
		if shown == 1 {
			if truncated := truncateDiagnoseProblemTestsSuffix(names[0], hidden, budget, renderName); truncated != "" {
				return truncated
			}
		}
	}
	return ""
}

func buildDiagnoseProblemTestsSuffix(shown []string, hidden int, renderName func(...string) string) string {
	if len(shown) == 0 && hidden == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(termstyle.Muted.Render(" ("))
	for i, name := range shown {
		if i > 0 {
			sb.WriteString(termstyle.Muted.Render(", "))
		}
		sb.WriteString(renderName(name))
	}
	if hidden > 0 {
		if len(shown) > 0 {
			sb.WriteString(termstyle.Muted.Render(", "))
		}
		sb.WriteString(termstyle.Muted.Render(fmt.Sprintf("+%d", hidden)))
	}
	sb.WriteString(termstyle.Muted.Render(")"))
	return sb.String()
}

func truncateDiagnoseProblemTestsSuffix(
	name string,
	hidden int,
	budget int,
	renderName func(...string) string,
) string {
	open := termstyle.Muted.Render(" (")
	closeParen := termstyle.Muted.Render(")")
	others := ""
	if hidden > 0 {
		others = termstyle.Muted.Render(", ") + termstyle.Muted.Render(fmt.Sprintf("+%d", hidden))
	}
	fixed := ansi.StringWidth(open) + ansi.StringWidth(others) + ansi.StringWidth(closeParen)
	avail := budget - fixed
	if avail <= 0 {
		return ""
	}
	truncated := ansi.Truncate(renderName(name), avail, "…")
	if ansi.StringWidth(truncated) == 0 {
		return ""
	}
	return open + truncated + others + closeParen
}
