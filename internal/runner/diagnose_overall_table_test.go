package runner

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/termstyle"
)

func TestRenderOverallRatesTable_allClear(t *testing.T) {
	t.Parallel()
	rep, _, err := Analyze(
		readers(`{"Action":"pass","Package":"p","Test":"T","Elapsed":0.01}`),
		30*time.Second,
	)
	require.NoError(t, err)
	plain := stripANSI(renderOverallRatesTable(rep))
	assert.Contains(t, plain, "Count")
	assert.Contains(t, plain, "Rate")
	assert.Contains(t, plain, "Broken Tests")
	assert.Contains(t, plain, "0.0%")
}

func TestRenderOverallRatesTable_flakyRow(t *testing.T) {
	t.Parallel()
	rep, _, err := Analyze(readers(
		`{"Action":"fail","Package":"pkg/foo","Test":"TestX","Elapsed":0.5}`,
		`{"Action":"pass","Package":"pkg/foo","Test":"TestX","Elapsed":0.4}`,
	), 30*time.Second)
	require.NoError(t, err)

	plain := stripANSI(renderOverallRatesTable(rep))
	assert.Contains(t, plain, "Broken Tests")
	assert.Contains(t, plain, "Flaky Tests")
	assert.Contains(t, plain, "1")
	assert.Contains(t, plain, "100.0%")
	assert.NotContains(t, plain, "(all clear)")
}

func TestOverallScopeAndWallLine(t *testing.T) {
	t.Parallel()
	rep, _, err := Analyze(
		readers(`{"Action":"pass","Package":"p","Test":"T","Elapsed":0.01}`),
		30*time.Second,
	)
	require.NoError(t, err)
	require.NotNil(t, rep.Summary)
	rep.IterationSummaries[0].Duration = 5 * time.Second
	fillIterationRuntimeSummary(rep)

	line := overallScopeAndWallLine(rep)
	assert.Contains(t, line, "1 tests")
	assert.Contains(t, line, "wall min 5s")
}

func TestPrintOverallRatesTable_slowestPackagesFootnote(t *testing.T) {
	t.Parallel()
	slowPrev := 0.0
	rep := &Report{
		SlowThreshold: time.Second,
		Summary: &ReportSummary{
			DistinctNamedTests: 10,
			SlowCount:          0,
			SlowPrevalence:     &slowPrev,
		},
		SlowestPackages: []TestEntry{
			{Package: "pkg/a", MaxElapsed: 2 * time.Second},
		},
	}
	var buf strings.Builder
	printOverallRatesTable(&buf, rep)
	out := stripANSI(buf.String())
	assert.Contains(t, out, "Slow Tests")
	assert.Contains(t, out, "1 slowest packages")
}

func TestBuildOverallRateRows_flakyIterationsLast(t *testing.T) {
	t.Parallel()
	slowPrev := 0.0
	rep := &Report{
		Iterations:    2,
		SlowThreshold: time.Second,
		Summary: &ReportSummary{
			DistinctNamedTests:     1,
			FlakeIterationTotal:    2,
			FlakeFailingIterations: 0,
			SlowCount:              0,
			SlowPrevalence:         &slowPrev,
		},
	}
	flakeIterRate := 0.0
	rep.Summary.FlakeIterationFailRate = &flakeIterRate

	rows := buildOverallRateRows(rep)
	require.Len(t, rows, 4)
	assert.Equal(t, "Slow Tests", rows[2].label)
	assert.Equal(t, "Flaky Iterations", rows[3].label)
}

func TestFormatOverallFlakyIterRate_CIColoredByGap(t *testing.T) {
	t.Parallel()
	rep, _, err := Analyze(readers(
		`{"Action":"pass","Package":"pkg/foo","Test":"TestX","Elapsed":0.5}`,
		`{"Action":"pass","Package":"pkg/foo","Test":"TestX","Elapsed":0.4}`,
	), 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, rep.Summary)

	gap := *rep.Summary.FlakeIterationFailRateUpper - *rep.Summary.FlakeIterationFailRateLower
	require.Greater(t, gap, 0.30)

	rate := formatOverallFlakyIterRate(rep, 0)
	ciText := fmt.Sprintf(
		"CI %.1f%%–%.1f%%",
		*rep.Summary.FlakeIterationFailRateLower*100,
		*rep.Summary.FlakeIterationFailRateUpper*100,
	)
	assert.Contains(t, rate, ciStyleForGap(gap).Render(ciText))
}

func TestOverallRateCells_useSeverityColors(t *testing.T) {
	t.Parallel()
	flaky := overallRateRow{
		label: "Flaky Tests", count: "1", rate: "100.0%", num: 1, kind: overallRateFlaky,
	}
	ok := overallRateRow{
		label: "Broken Tests", count: "0", rate: "  0.0%", num: 0, kind: overallRateBroken,
	}
	slow := overallRateRow{
		label: "Slow Tests", count: "2", rate: "  1.0%", num: 2, kind: overallRateSlow,
	}
	assert.Equal(t, termstyle.Bad.Render("1"), overallRateCountCell(flaky))
	assert.Equal(t, termstyle.OK.Render("0"), overallRateCountCell(ok))
	assert.Equal(t, termstyle.Flaky.Render("2"), overallRateCountCell(slow))
	assert.Equal(t, termstyle.Bad.Render("100.0%"), overallRateRateCell(overallRateRow{
		label: "x", rate: "100.0%", num: 1, kind: overallRateFlaky,
	}))
	assert.Equal(t, termstyle.OK.Render("  0.0%"), overallRateRateCell(ok))
	assert.Equal(t, termstyle.Flaky.Render("  1.0%"), overallRateRateCell(slow))
}
