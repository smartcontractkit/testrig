package runner

import (
	"fmt"
	"io"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/smartcontractkit/testrig/internal/termstyle"
)

type overallRateKind uint8

const (
	overallRateBroken overallRateKind = iota
	overallRateFlaky
	overallRateSlow
	overallRateRace
)

type overallRateRow struct {
	label         string
	count         string
	rate          string
	num           int // rate numerator; 0 → green rate
	kind          overallRateKind
	ratePreStyled bool // rate already includes per-part styling (e.g. CI gap colors)
}

func printOverallScope(w io.Writer, rep *Report) {
	if line := overallScopeAndWallLine(rep); line != "" {
		_, _ = fmt.Fprintln(w, termstyle.Muted.Render("  "+line))
	}
}

func overallScopeAndWallLine(rep *Report) string {
	if rep == nil || rep.Summary == nil || rep.Summary.DistinctNamedTests == 0 {
		return ""
	}
	s := rep.Summary
	var parts []string
	parts = append(parts, fmt.Sprintf("%d tests", s.DistinctNamedTests))
	if rep.Iterations > 0 {
		parts = append(parts, fmt.Sprintf("%d iters", rep.Iterations))
	}
	if rep.SlowThreshold > 0 {
		parts = append(parts, "slow ≥ "+rep.SlowThreshold.Round(time.Millisecond).String())
	}
	if s.IterationDurationMin > 0 || s.IterationDurationP50 > 0 || s.IterationDurationMax > 0 {
		parts = append(parts, fmt.Sprintf(
			"wall min %s  p50 %s  max %s",
			s.IterationDurationMin.Round(time.Millisecond),
			s.IterationDurationP50.Round(time.Millisecond),
			s.IterationDurationMax.Round(time.Millisecond),
		))
	}
	return strings.Join(parts, " · ")
}

func buildOverallRateRows(rep *Report) []overallRateRow {
	if rep == nil || rep.Summary == nil || rep.Summary.DistinctNamedTests == 0 {
		return nil
	}
	s := rep.Summary
	denom := s.DistinctNamedTests
	var rows []overallRateRow

	brokenN := countBrokenNamedTests(rep)
	rows = append(rows, overallRateRow{
		label: "Broken Tests",
		count: fmt.Sprintf("%d", brokenN),
		rate:  formatOverallRatePct(brokenN, denom),
		num:   brokenN,
		kind:  overallRateBroken,
	})
	rows = append(rows, overallRateRow{
		label: "Flaky Tests",
		count: fmt.Sprintf("%d", s.FlakeNamedCount),
		rate:  formatOverallRatePct(s.FlakeNamedCount, denom),
		num:   s.FlakeNamedCount,
		kind:  overallRateFlaky,
	})
	if rep.Run != nil && rep.Run.Race && s.RaceNamedCount > 0 {
		rows = append(rows, overallRateRow{
			label: "Races",
			count: fmt.Sprintf("%d", s.RaceNamedCount),
			rate:  formatOverallRatePct(s.RaceNamedCount, denom),
			num:   s.RaceNamedCount,
			kind:  overallRateRace,
		})
	}
	if rep.SlowThreshold > 0 && s.SlowPrevalence != nil {
		rows = append(rows, overallRateRow{
			label: "Slow Tests",
			count: fmt.Sprintf("%d", s.SlowCount),
			rate:  formatOverallRatePct(s.SlowCount, denom),
			num:   s.SlowCount,
			kind:  overallRateSlow,
		})
	}
	if s.FlakeIterationTotal > 0 && s.FlakeIterationFailRate != nil {
		n := s.FlakeFailingIterations
		rows = append(rows, overallRateRow{
			label:         "Flaky Iterations",
			count:         fmt.Sprintf("%d/%d", n, s.FlakeIterationTotal),
			rate:          formatOverallFlakyIterRate(rep, n),
			num:           n,
			kind:          overallRateFlaky,
			ratePreStyled: true,
		})
	}
	return rows
}

func formatOverallRatePct(num, denom int) string {
	if denom == 0 {
		return "—"
	}
	pct := float64(num) / float64(denom) * 100
	return fmt.Sprintf("%5.1f%%", pct)
}

func formatOverallFlakyIterRate(rep *Report, failingIters int) string {
	s := rep.Summary
	if s == nil || s.FlakeIterationTotal == 0 {
		return "—"
	}
	pct := formatOverallRatePct(failingIters, s.FlakeIterationTotal)
	pct = overallRatePctStyle(overallRateFlaky, failingIters).Render(pct)
	ci := overallFlakyIterationCI(s)
	if ci == "" {
		return pct
	}
	return pct + "  " + ci
}

func overallRatePctStyle(kind overallRateKind, num int) lipgloss.Style {
	if num == 0 {
		return termstyle.OK
	}
	if kind == overallRateSlow {
		return termstyle.Flaky
	}
	if kind == overallRateRace {
		return termstyle.Flaky
	}
	return termstyle.Bad
}

func renderOverallRatesTable(rep *Report) string {
	rows := buildOverallRateRows(rep)
	if len(rows) == 0 {
		return ""
	}

	tbl := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("240"))).
		StyleFunc(overallRatesTableStyle).
		Headers("", "Count", "Rate")

	for _, r := range rows {
		tbl.Row(r.label, overallRateCountCell(r), overallRateRateCell(r))
	}
	return indentBlock(tbl.String(), "  ")
}

func overallRatesTableStyle(row, _ int) lipgloss.Style {
	if row == table.HeaderRow {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Bold(true).
			Padding(0, 1)
	}
	return lipgloss.NewStyle().Padding(0, 1)
}

func overallRateCountCell(r overallRateRow) string {
	if r.num > 0 {
		switch r.kind {
		case overallRateSlow, overallRateRace:
			return termstyle.Flaky.Render(r.count)
		default:
			return termstyle.Bad.Render(r.count)
		}
	}
	return termstyle.OK.Render(r.count)
}

func overallRateRateCell(r overallRateRow) string {
	if r.ratePreStyled {
		return r.rate
	}
	return overallRatePctStyle(r.kind, r.num).Render(r.rate)
}

func printOverallRatesTable(w io.Writer, rep *Report) {
	rendered := renderOverallRatesTable(rep)
	if rendered == "" {
		return
	}
	_, _ = fmt.Fprintln(w, rendered)
	if n := slowPackageCount(rep); n > 0 {
		_, _ = fmt.Fprintln(w, termstyle.Muted.Render(fmt.Sprintf("  %d slowest packages (≥ threshold)", n)))
	}
}

func indentBlock(s, prefix string) string {
	if s == "" {
		return ""
	}
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}
