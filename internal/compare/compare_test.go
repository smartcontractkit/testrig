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
	fixedIdx := strings.Index(out, "### Fixed")
	for _, idx := range []int{newIdx, worseIdx, noChangeIdx, betterIdx, fixedIdx} {
		require.NotEqual(t, -1, idx, "missing verdict section in:\n%s", out)
	}
	assert.Less(t, newIdx, worseIdx)
	assert.Less(t, worseIdx, noChangeIdx)
	assert.Less(t, noChangeIdx, betterIdx)
	assert.Less(t, betterIdx, fixedIdx)

	assert.Contains(t, out, "| TestNew | pkg/a | ok | failure |")
	assert.Contains(t, out, "| TestFixed | pkg/a | failure | ok |")
	assert.Contains(t, out, "| TestWorse | pkg/a | failure | failure |")
	assert.Contains(t, out, "| TestBetter | pkg/a | failure | flake |")
	assert.Contains(t, out, "| TestSame | pkg/a | failure | failure |")
	assert.Contains(t, out, "| TestRace | pkg/a | race | race |")
	assert.Contains(t, out, "| TestCategory | pkg/a | slow | failure |")

	assert.Contains(t, out, "| Races |")
	assert.Contains(t, out, "| Flaky iterations | 3/30 (10.0%)")
	assert.Contains(t, out, "| Passing iterations | 1/2 (50.0%)")
	assert.Contains(t, out, "| Iteration wall time p50 |")
	assert.Contains(t, out, "`diagnose-before`")
	assert.Contains(t, out, "`diagnose-after`")
}

func TestWriteHidesRacesWithoutRaceFlag(t *testing.T) {
	t.Parallel()
	out := renderComparison(t, &runner.Report{}, &runner.Report{})
	assert.NotContains(t, out, "| Races |")
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
	assert.Contains(t, out, "_No problem tests in either run._")
	assert.NotContains(t, out, "## Slow tests")
	assert.NotContains(t, out, "| Races |")
}

func TestWriteComparesCountsAsRatesWhenIterationsDiffer(t *testing.T) {
	t.Parallel()
	before := &runner.Report{
		Iterations: 10,
		Failures:   []runner.TestEntry{testEntry("pkg/a", "TestBroken", 1, 10)},
	}
	after := &runner.Report{
		Iterations: 20,
		Failures:   []runner.TestEntry{testEntry("pkg/a", "TestBroken", 2, 20)},
	}
	out := renderComparison(t, before, after)
	assert.Contains(t, out, "| Broken tests | 1 (10.0%) | 1 (5.0%) |")
}

func TestWriteTimingDeltas(t *testing.T) {
	t.Parallel()
	out := renderComparison(t, verdictBeforeReport(), verdictAfterReport())
	assert.Contains(t, out, "## Slow tests")
	assert.Contains(t, out, "| TestSlow | pkg/a | 1s | 2s |")
	assert.Contains(t, out, "+1s (+100%)")
	assert.Contains(t, out, "## Slowest packages")
	assert.Contains(t, out, "| pkg/a | 1s | 2s |")
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
