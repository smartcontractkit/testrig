package runner

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/output"
)

func TestMarshalAIDiagnoseComplete(t *testing.T) {
	t.Parallel()
	rep, _, _, err := Analyze(readers(
		`{"Action":"fail","Package":"pkg/foo","Test":"TestX","Elapsed":0.5}`,
		`{"Action":"pass","Package":"pkg/foo","Test":"TestX","Elapsed":0.4}`,
	), 30*time.Second)
	require.NoError(t, err)

	resultsDir := t.TempDir()
	reportPath := filepath.Join(resultsDir, "report.json")
	tracePath := filepath.Join(resultsDir, "trace.json")
	raw, err := marshalAIDiagnoseComplete(resultsDir, reportPath, tracePath, rep)
	require.NoError(t, err)

	var ev aiDiagnoseComplete
	require.NoError(t, json.Unmarshal(raw, &ev))
	assert.Equal(t, "complete", ev.Event)
	assert.Equal(t, resultsDir, ev.Results)
	assert.Equal(t, reportPath, ev.Report)
	assert.Equal(t, tracePath, ev.Trace)
	require.NotNil(t, ev.Summary)
	require.Len(t, ev.Findings.Flaky, 1)
	assert.Equal(t, "pkg/foo", ev.Findings.Flaky[0].Package)
	assert.Equal(t, "TestX", ev.Findings.Flaky[0].Test)
}

func TestMarshalAIDiagnoseComplete_capsFindings(t *testing.T) {
	t.Parallel()
	var flakes []TestEntry
	for i := range 25 {
		flakes = append(flakes, TestEntry{
			Package: "pkg",
			Test:    "Test" + string(rune('A'+i)),
			Runs:    10,
			Fails:   1,
		})
	}
	rep := &Report{Flakes: flakes}
	raw, err := marshalAIDiagnoseComplete(t.TempDir(), "report.json", "trace.json", rep)
	require.NoError(t, err)

	var ev aiDiagnoseComplete
	require.NoError(t, json.Unmarshal(raw, &ev))
	assert.Len(t, ev.Findings.Flaky, aiFindingsCap)
}

func TestFormatSummaryFlatLine(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		entry    TestEntry
		statsFor func(TestEntry) string
		want     string
	}{
		{
			name:     "broken_named",
			entry:    TestEntry{Package: "pkg/foo", Test: "TestX"},
			statsFor: formatBrokenStats,
			want:     "pkg/foo  TestX",
		},
		{
			name:     "flake_named",
			entry:    TestEntry{Package: "pkg/foo", Test: "TestX", Runs: 10, Fails: 2},
			statsFor: formatFlakyStats,
			want:     "pkg/foo  TestX  (2/10) 20.0%",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := stripANSI(formatSummaryFlatLine(tc.entry, tc.statsFor))
			assert.Contains(t, got, tc.want)
		})
	}
}

func TestPrintSummaryVerdict_noIssues(t *testing.T) {
	t.Parallel()
	rep, _, _, err := Analyze(
		readers(`{"Action":"pass","Package":"p","Test":"T","Elapsed":0.01}`),
		30*time.Second,
	)
	require.NoError(t, err)
	var buf strings.Builder
	PrintSummary(&buf, rep)
	plain := stripANSI(buf.String())
	assert.Contains(t, plain, "Broken Tests")
	assert.Contains(t, plain, "0.0%")
}

func TestPrintSummaryVerdict_slowPackagesOnly(t *testing.T) {
	t.Parallel()
	slowPrev := 0.0
	rep := &Report{
		Iterations:    3,
		SlowThreshold: time.Second,
		Summary: &ReportSummary{
			DistinctNamedTests: 10,
			SlowCount:          0,
			SlowPrevalence:     &slowPrev,
		},
		SlowestPackages: []TestEntry{
			{Package: "pkg/a", MaxElapsed: 2 * time.Second},
			{Package: "pkg/b", MaxElapsed: 3 * time.Second},
		},
	}
	var buf strings.Builder
	PrintSummary(&buf, rep)
	out := buf.String()
	assert.Contains(t, out, "slowest packages (≥ threshold)")
	assert.NotContains(t, out, "slow tests")
	assert.Contains(t, stripANSI(out), "Slow Tests")
	assert.Contains(t, stripANSI(out), "0")
	assert.Contains(t, stripANSI(out), "2 slowest packages")
	assert.Contains(t, out, "Slowest Packages (2)")
}

func TestPrintSummaryVerdict_counts(t *testing.T) {
	t.Parallel()
	rep := &Report{
		Failures: []TestEntry{{Package: "p", Test: "T1"}},
		Flakes:   []TestEntry{{Package: "p", Test: "T2", Runs: 5, Fails: 1}},
		Summary:  &ReportSummary{DistinctNamedTests: 2},
	}
	var buf strings.Builder
	PrintSummary(&buf, rep)
	out := buf.String()
	assert.Contains(t, stripANSI(out), "Broken Tests")
	assert.Contains(t, stripANSI(out), "Flaky Tests")
	assert.Contains(t, stripANSI(out), "50.0%")
}

func TestRenderDiagnoseArtifactsTable(t *testing.T) {
	t.Parallel()
	results := "/tmp/diagnose-out"
	report := results + "/report.json"
	csv := results + "/report.csv"
	trace := results + "/trace.json"

	plain := stripANSI(renderDiagnoseArtifactsTable(results, report, csv, trace))
	assert.Contains(t, plain, "Path")
	assert.Contains(t, plain, "results")
	assert.Contains(t, plain, "report")
	assert.Contains(t, plain, "csv")
	assert.Contains(t, plain, "trace")
	assert.Contains(t, plain, results)
	assert.Contains(t, plain, report)
	assert.Contains(t, plain, csv)
	assert.Contains(t, plain, "run \"")

	wd, err := os.Getwd()
	require.NoError(t, err)
	rel, err := filepath.Rel(wd, trace)
	require.NoError(t, err)
	assert.Contains(t, plain, "trace "+rel)
	assert.Equal(t, 0, strings.Count(plain, "file://"), "file:// should not be present in the table")
}

func TestRenderDiagnoseArtifactsTable_noCSV(t *testing.T) {
	t.Parallel()
	plain := stripANSI(renderDiagnoseArtifactsTable("/tmp/out", "/tmp/out/report.json", "", "/tmp/out/trace.json"))
	assert.Contains(t, plain, "results")
	assert.Contains(t, plain, "report")
	assert.Contains(t, plain, "trace")
	assert.NotContains(t, plain, "csv")
}

func TestPrintDiagnoseArtifactsFooter_table(t *testing.T) {
	t.Parallel()
	var stderr strings.Builder
	out := output.NewForTest(false, io.Discard, &stderr, false)
	printDiagnoseArtifactsFooter(out, "/tmp/out", "/tmp/out/report.json", "/tmp/out/report.csv", "/tmp/out/trace.json")
	s := stripANSI(stderr.String())
	assert.Contains(t, s, "Path")
	assert.Contains(t, s, "/tmp/out/report.json")
	assert.NotContains(t, s, "results:")
}
