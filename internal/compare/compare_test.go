package compare

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/runner"
)

func TestResolve(t *testing.T) {
	t.Parallel()

	t.Run("literal path", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		writeTestReport(t, dir)
		got, err := Resolve(t.TempDir(), dir)
		require.NoError(t, err)
		assert.Equal(t, dir, got)
	})

	t.Run("named fallback", func(t *testing.T) {
		t.Parallel()
		wd := t.TempDir()
		dir := filepath.Join(wd, "diagnose-before")
		require.NoError(t, os.Mkdir(dir, 0700))
		writeTestReport(t, dir)
		got, err := Resolve(wd, "before")
		require.NoError(t, err)
		assert.Equal(t, dir, got)
	})

	t.Run("relative path resolves from wd", func(t *testing.T) {
		t.Parallel()
		wd := t.TempDir()
		dir := filepath.Join(wd, "sub")
		require.NoError(t, os.Mkdir(dir, 0700))
		writeTestReport(t, dir)
		got, err := Resolve(wd, "sub")
		require.NoError(t, err)
		assert.Equal(t, dir, got)
	})

	t.Run("missing names both paths", func(t *testing.T) {
		t.Parallel()
		wd := t.TempDir()
		_, err := Resolve(wd, "before")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "before")
		assert.Contains(t, err.Error(), filepath.Join(wd, "diagnose-before"))
	})
}

func TestWriteVerdictsAndSummary(t *testing.T) {
	t.Parallel()
	out := renderComparison(t, verdictBeforeReport(), verdictAfterReport())

	assert.Contains(t, out, "# testrig compare: before-race → after-race")
	assert.NotContains(t, out, "[!WARNING]")

	newIdx := strings.Index(out, "### New")
	worseIdx := strings.Index(out, "### Worse")
	noChangeIdx := strings.Index(out, "### No clear change")
	betterIdx := strings.Index(out, "### Better")
	unflaggedIdx := strings.Index(out, "### No longer flagged")
	for _, idx := range []int{newIdx, worseIdx, noChangeIdx, betterIdx, unflaggedIdx} {
		require.NotEqual(t, -1, idx, "missing verdict section in:\n%s", out)
	}
	assert.Less(t, newIdx, worseIdx)
	assert.Less(t, worseIdx, noChangeIdx)
	assert.Less(t, noChangeIdx, betterIdx)
	assert.Less(t, betterIdx, unflaggedIdx)

	assert.Contains(t, out, "| TestNew | pkg/a | ok | failure |")
	assert.Contains(t, out, "| TestFixed | pkg/a | failure | ok |")
	assert.Contains(t, out, "| TestWorse | pkg/a | failure | failure |")
	assert.Contains(t, out, "| TestBetter | pkg/a | failure | flake |")
	assert.Contains(t, out, "| TestSame | pkg/a | failure | failure |")
	assert.Contains(t, out, "| TestRace | pkg/a | race | race |")
	assert.Contains(t, out, "| TestCategory | pkg/a | slow | failure |")

	assert.Contains(t, out, "| Races |")
	assert.Contains(t, out, "| Broken tests | 4 | 4 | 0 |")
	assert.Contains(t, out, "| Slow tests | 2 | 1 | -1 ✅ |")
	assert.Contains(t, out, "| Flaky iterations | 3/30 (10.0%) | 1/30 (3.3%) | -6.7 pp ✅ |")
	assert.NotContains(t, out, "Passing iterations")
	assert.Contains(t, out, "| Iteration wall time p50 |")
	assert.Contains(t, out, "`diagnose-before`")
	assert.Contains(t, out, "`diagnose-after`")
}

func TestWriteHidesRacesWithoutRaceFlag(t *testing.T) {
	t.Parallel()
	out := renderComparison(t, &runner.Report{}, &runner.Report{})
	assert.NotContains(t, out, "| Races |")
	assert.NotContains(t, out, "### Races by package")
}

func TestWritePackageRaces(t *testing.T) {
	t.Parallel()
	before := &runner.Report{
		Run: &runner.RunMeta{Race: true},
		Races: []runner.TestEntry{
			testEntry("pkg/a", "TestRace", 1, 10),
			testEntry("pkg/b", "TestStableRace", 2, 10),
		},
	}
	after := &runner.Report{
		Run: &runner.RunMeta{Race: true},
		Races: []runner.TestEntry{
			testEntry("pkg/a", "TestRace", 3, 10),
			testEntry("pkg/a", "TestNewRace", 1, 10),
			testEntry("pkg/b", "TestStableRace", 2, 10),
		},
	}
	out := renderComparison(t, before, after)

	assert.Contains(t, out, "### Races by package")
	assert.Contains(t, out, "| pkg/a | 1 | 2 | 1 | 4 | +3 failures ❌ |")
	assert.Contains(t, out, "<summary>Packages with no race change (1)</summary>")
	assert.Contains(t, out, "| pkg/b | 1 | 1 | 2 | 2 |  |")
}

func TestWriteHidesPackageRacesUnlessBothRunsUseRace(t *testing.T) {
	t.Parallel()
	raceRun := &runner.Report{
		Run:   &runner.RunMeta{Race: true},
		Races: []runner.TestEntry{testEntry("pkg/a", "TestRace", 1, 10)},
	}
	plainRun := &runner.Report{Run: &runner.RunMeta{Race: false}}

	assert.NotContains(t, renderComparison(t, raceRun, plainRun), "### Races by package")
	assert.NotContains(t, renderComparison(t, plainRun, raceRun), "### Races by package")
}

func TestWriteWarnsWhenRunsDiffer(t *testing.T) {
	t.Parallel()
	before := &runner.Report{Run: &runner.RunMeta{
		GoTestArgs:    []string{"./a"},
		SlowThreshold: 30 * time.Second,
	}}
	after := &runner.Report{Run: &runner.RunMeta{
		GoTestArgs:    []string{"./b"},
		SlowThreshold: time.Minute,
	}}
	out := renderComparison(t, before, after)
	assert.Contains(t, out, "[!WARNING]")
	assert.Contains(t, out, "go test arguments differ")
	assert.Contains(t, out, "slow thresholds differ")
	assert.Contains(t, out, "Results may not be comparable")
}

func TestWriteEscapesPipesInTestNames(t *testing.T) {
	t.Parallel()
	before := &runner.Report{Failures: []runner.TestEntry{testEntry("pkg/a", "TestA|B", 3, 30)}}
	after := &runner.Report{Failures: []runner.TestEntry{testEntry("pkg/a", "TestA|B", 3, 30)}}
	out := renderComparison(t, before, after)
	assert.Contains(t, out, `TestA\|B`)
	assert.NotContains(t, out, "| TestA|B |")
}

func TestWriteEmptyReports(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := Write(&buf, Run{}, Run{})
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "# testrig compare: before → after")
	assert.Contains(t, out, "| Tests run | — | — |")
	assert.Contains(t, out, "_No problem tests in either run._")
	assert.NotContains(t, out, "## Slow tests")
	assert.NotContains(t, out, "| Races |")
}

func TestWriteComparesRawCountsWhenIterationsDiffer(t *testing.T) {
	t.Parallel()
	// Distinct flagged tests do not grow with iteration count, so counts stay raw
	// instead of being divided by iterations.
	before := &runner.Report{
		Iterations: 10,
		Run:        &runner.RunMeta{DiagnoseIterations: 10},
		Summary:    &runner.ReportSummary{DistinctNamedTests: 20},
		Failures:   []runner.TestEntry{testEntry("pkg/a", "TestBroken", 10, 10)},
	}
	after := &runner.Report{
		Iterations: 30,
		Run:        &runner.RunMeta{DiagnoseIterations: 30},
		Summary:    &runner.ReportSummary{DistinctNamedTests: 20},
		Failures:   []runner.TestEntry{testEntry("pkg/a", "TestBroken", 30, 30)},
	}
	out := renderComparison(t, before, after)

	assert.Contains(t, out, "| Broken tests | 1 | 1 | 0 | — |")
	assert.Contains(t, out, "[!WARNING]")
	assert.Contains(t, out, "iteration counts differ")
}

func TestWriteRaceSummaryWhenOneRunLacksRaceFlag(t *testing.T) {
	t.Parallel()
	raceRun := &runner.Report{
		Iterations: 10,
		Run:        &runner.RunMeta{Race: true, DiagnoseIterations: 10},
		Summary:    &runner.ReportSummary{DistinctNamedTests: 10, RaceNamedCount: 2},
		Races:      []runner.TestEntry{testEntry("pkg/a", "TestRace", 2, 10)},
	}
	plainRun := &runner.Report{
		Iterations: 10,
		Run:        &runner.RunMeta{DiagnoseIterations: 10},
		Summary:    &runner.ReportSummary{DistinctNamedTests: 10},
	}

	raceFirst := renderComparison(t, raceRun, plainRun)
	assert.Contains(t, raceFirst, "| Races | 2 | — |")
	assert.Contains(t, raceFirst, "only one run used -race")
	assert.NotContains(t, raceFirst, "-2 ✅")
	assert.NotContains(t, raceFirst, "| TestRace |")

	plainFirst := renderComparison(t, plainRun, raceRun)
	assert.Contains(t, plainFirst, "| Races | — | 2 |")
	assert.Contains(t, plainFirst, "only one run used -race")
	assert.NotContains(t, plainFirst, "+2 ❌")
	assert.NotContains(t, plainFirst, "| TestRace |")
}

func TestWriteSummaryCountsNamedTimeoutsOnly(t *testing.T) {
	t.Parallel()
	// A package-level timeout (Test == "") is not part of the distinct-named-test
	// denominator, so the summary counts only named timeouts.
	before := &runner.Report{
		Iterations: 10,
		Run:        &runner.RunMeta{DiagnoseIterations: 10},
		Summary:    &runner.ReportSummary{DistinctNamedTests: 1},
		Timeouts: []runner.TestEntry{
			{Package: "pkg/a"},
			testEntry("pkg/a", "TestTimeout", 1, 10),
		},
	}
	after := &runner.Report{
		Iterations: 10,
		Run:        &runner.RunMeta{DiagnoseIterations: 10},
		Summary:    &runner.ReportSummary{DistinctNamedTests: 1},
	}
	out := renderComparison(t, before, after)
	assert.Contains(t, out, "| Timeouts | 1 | 0 | -1 ✅ |")
}

func TestWriteTimingDeltas(t *testing.T) {
	t.Parallel()
	out := renderComparison(t, verdictBeforeReport(), verdictAfterReport())
	assert.Contains(t, out, "## Package breakdown")
	assert.Contains(t, out, "### Flakes by package")
	assert.Contains(t, out, "### Speed by package")
	assert.Contains(t, out, "## Slow tests")
	assert.Contains(t, out, "| TestSlow | pkg/a | 1s | 2s |")
	assert.Contains(t, out, "+1s (+100%)")
	assert.Contains(t, out, "| pkg/a | 1s | 2s |")
}

func TestWritePackageFlakes(t *testing.T) {
	t.Parallel()
	before := &runner.Report{Flakes: []runner.TestEntry{
		testEntry("pkg/a", "TestA", 3, 30),
		testEntry("pkg/b", "TestB", 2, 30),
		testEntry("pkg/c", "TestC", 1, 30),
	}}
	after := &runner.Report{Flakes: []runner.TestEntry{
		testEntry("pkg/a", "TestA", 5, 30),
		testEntry("pkg/b", "TestB", 2, 30),
	}}
	out := renderComparison(t, before, after)

	assert.Contains(t, out, "| pkg/a | 1 | 1 | 3 | 5 | +2 failures ❌ |")
	assert.Contains(t, out, "| pkg/c | 1 | 0 | 1 | 0 | -1 failure ✅ |")
	assert.Contains(t, out, "<summary>Packages with no flake change (1)</summary>")
	assert.Contains(t, out, "| pkg/b | 1 | 1 | 2 | 2 |  |")

	detailsIdx := strings.Index(out, "<details>")
	require.NotEqual(t, -1, detailsIdx)
	flakesIdx := strings.Index(out, "### Flakes by package")
	require.NotEqual(t, -1, flakesIdx)
	assert.NotContains(t, out[flakesIdx:detailsIdx], "| pkg/b |")
}

func TestWritePackageSpeedCollapsesSmallChanges(t *testing.T) {
	t.Parallel()
	before := &runner.Report{SlowestPackages: []runner.TestEntry{
		{Package: "pkg/a", P50Elapsed: time.Second, MaxElapsed: 2 * time.Second},
		{Package: "pkg/b", P50Elapsed: time.Second, MaxElapsed: 2 * time.Second},
	}}
	after := &runner.Report{SlowestPackages: []runner.TestEntry{
		{Package: "pkg/a", P50Elapsed: 2 * time.Second, MaxElapsed: 3 * time.Second},
		{Package: "pkg/b", P50Elapsed: 1020 * time.Millisecond, MaxElapsed: 2010 * time.Millisecond},
	}}
	out := renderComparison(t, before, after)

	detailsIdx := strings.Index(out, "<details>")
	require.NotEqual(t, -1, detailsIdx)
	speedIdx := strings.Index(out, "### Speed by package")
	require.NotEqual(t, -1, speedIdx)
	main := out[speedIdx:detailsIdx]
	assert.Contains(t, main, "| pkg/a | 1s | 2s |")
	assert.NotContains(t, main, "| pkg/b |")
	assert.Contains(t, out, "<summary>Packages with little or no speed change (1)</summary>")
	assert.Contains(
		t,
		out[detailsIdx:],
		"| pkg/b | 1s | 1.02s | +20ms (+2%) | 2s | 2.01s | +10ms (+0%) |",
	)
}

func TestWriteSummaryEvidence(t *testing.T) {
	t.Parallel()
	before := &runner.Report{
		Iterations: 10,
		Summary: &runner.ReportSummary{
			DistinctNamedTests:     100,
			FlakeNamedCount:        10,
			FlakeFailingIterations: 5,
			FlakeIterationTotal:    10,
			IterationDurationP50:   3 * time.Second,
			IterationDurationMax:   5 * time.Second,
		},
		IterationSummaries: []runner.IterationSummary{
			{Duration: time.Second},
			{Duration: 2 * time.Second},
			{Duration: 3 * time.Second},
			{Duration: 4 * time.Second},
			{Duration: 5 * time.Second},
		},
	}
	after := &runner.Report{
		Iterations: 10,
		Summary: &runner.ReportSummary{
			DistinctNamedTests:     100,
			FlakeNamedCount:        1,
			FlakeFailingIterations: 1,
			FlakeIterationTotal:    10,
			IterationDurationP50:   4 * time.Second,
			IterationDurationMax:   6 * time.Second,
		},
		IterationSummaries: []runner.IterationSummary{
			{Duration: 2 * time.Second},
			{Duration: 3 * time.Second},
			{Duration: 4 * time.Second},
			{Duration: 5 * time.Second},
			{Duration: 6 * time.Second},
		},
	}
	out := renderComparison(t, before, after)

	assert.Contains(t, out, "| Metric | before-race | after-race | Change | Evidence |")
	assert.Contains(t, out, "| Tests run | 100 | 100 | 0 | — |")
	assert.Contains(t, out, "| Broken tests | 0 | 0 | 0 | — |")
	assert.Regexp(t, `\| Flaky tests \| 10 \| 1 \| -9 ✅ \| clear \|`, out)
	assert.Regexp(t, `\| Flaky iterations \| 5/10 \(50\.0%\) \| 1/10 \(10\.0%\) \| -40\.0 pp ✅ \| unclear \|`, out)
	assert.Regexp(t, `\| Iteration wall time p50 \| 3s \| 4s \| \+1s \(\+33%\) ❌ \| unclear \|`, out)
	assert.NotContains(t, out, "Fisher")
	assert.NotContains(t, out, "p:")
}

func renderComparison(t *testing.T, before, after *runner.Report) string {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, Write(
		&buf,
		Run{Label: "before-race", Report: before},
		Run{Label: "after-race", Report: after},
	))
	return buf.String()
}

func writeTestReport(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, runner.WriteReport(dir, &runner.Report{Iterations: 1}))
}

func testEntry(pkg, test string, fails, runs int) runner.TestEntry {
	return runner.TestEntry{
		Package:   pkg,
		Test:      test,
		Runs:      runs,
		Fails:     fails,
		Successes: runs - fails,
	}
}

func verdictBeforeReport() *runner.Report {
	return &runner.Report{
		Iterations:    30,
		SlowThreshold: 30 * time.Second,
		Run: &runner.RunMeta{
			ResultsDirBasename: "diagnose-before",
			Name:               "before-race",
			GoTestArgs:         []string{"-race", "./..."},
			DiagnoseIterations: 30,
			SlowThreshold:      30 * time.Second,
			Race:               true,
		},
		Summary: &runner.ReportSummary{
			DistinctNamedTests:     10,
			FlakeNamedCount:        1,
			SlowCount:              2,
			RaceNamedCount:         1,
			FlakeFailingIterations: 3,
			FlakeIterationTotal:    30,
			IterationDurationP50:   10 * time.Second,
			IterationDurationMax:   20 * time.Second,
		},
		IterationSummaries: []runner.IterationSummary{{Result: "fail"}, {Result: "pass"}},
		Races:              []runner.TestEntry{testEntry("pkg/a", "TestRace", 10, 30)},
		Failures: []runner.TestEntry{
			testEntry("pkg/a", "TestFixed", 10, 30),
			testEntry("pkg/a", "TestWorse", 3, 30),
			testEntry("pkg/a", "TestSame", 3, 30),
			testEntry("pkg/a", "TestBetter", 20, 30),
		},
		Slow: []runner.TestEntry{
			{
				Package:    "pkg/a",
				Test:       "TestSlow",
				Runs:       30,
				P50Elapsed: time.Second,
				MaxElapsed: 2 * time.Second,
			},
			{
				Package:    "pkg/a",
				Test:       "TestCategory",
				Runs:       30,
				P50Elapsed: time.Second,
				MaxElapsed: 2 * time.Second,
			},
		},
		SlowestPackages: []runner.TestEntry{
			{Package: "pkg/a", P50Elapsed: time.Second, MaxElapsed: 2 * time.Second},
		},
	}
}

func verdictAfterReport() *runner.Report {
	return &runner.Report{
		Iterations:    30,
		SlowThreshold: 30 * time.Second,
		Run: &runner.RunMeta{
			ResultsDirBasename: "diagnose-after",
			Name:               "after-race",
			GoTestArgs:         []string{"-race", "./..."},
			DiagnoseIterations: 30,
			SlowThreshold:      30 * time.Second,
			Race:               true,
		},
		Summary: &runner.ReportSummary{
			DistinctNamedTests:     9,
			FlakeNamedCount:        1,
			SlowCount:              1,
			RaceNamedCount:         1,
			FlakeFailingIterations: 1,
			FlakeIterationTotal:    30,
			IterationDurationP50:   8 * time.Second,
			IterationDurationMax:   16 * time.Second,
		},
		IterationSummaries: []runner.IterationSummary{{Result: "pass"}, {Result: "pass"}},
		Races:              []runner.TestEntry{testEntry("pkg/a", "TestRace", 20, 30)},
		Failures: []runner.TestEntry{
			testEntry("pkg/a", "TestNew", 5, 30),
			testEntry("pkg/a", "TestWorse", 28, 30),
			testEntry("pkg/a", "TestSame", 3, 30),
			testEntry("pkg/a", "TestCategory", 10, 30),
		},
		Flakes: []runner.TestEntry{testEntry("pkg/a", "TestBetter", 1, 30)},
		Slow: []runner.TestEntry{
			{
				Package:    "pkg/a",
				Test:       "TestSlow",
				Runs:       30,
				P50Elapsed: 2 * time.Second,
				MaxElapsed: 3 * time.Second,
			},
		},
		SlowestPackages: []runner.TestEntry{
			{Package: "pkg/a", P50Elapsed: 2 * time.Second, MaxElapsed: 3 * time.Second},
		},
	}
}
