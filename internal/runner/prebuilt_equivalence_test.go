package runner

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/output"
)

type prebuiltEquivalenceScenario struct {
	name    string
	fixture string
	args    []string
	// ordered compares the order of test run events. -shuffle needs this;
	// other scenarios only need the event multiset.
	ordered bool
}

var prebuiltEquivalenceScenarios = []prebuiltEquivalenceScenario{
	{
		name:    "basic",
		fixture: "basic",
		args: []string{
			"./pass", "./fail", "./panic", "./exit0", "./exitmain",
			"./testdata", "./nomatch", "./empty", "./collision/...",
		},
	},
	{
		name:    "no tests matching run",
		fixture: "basic",
		args:    []string{"-run", "TestDoesNotExist", "./nomatch"},
	},
	{
		name:    "args after args",
		fixture: "basic",
		args:    []string{"./args", "-args", "hello"},
	},
	{
		name:    "shuffle fixed seed",
		fixture: "basic",
		args:    []string{"-shuffle=1234", "./shuffle"},
		ordered: true,
	},
	{
		name:    "verbose",
		fixture: "basic",
		args:    []string{"-v", "./pass", "./fail"},
	},
	{
		name:    "p limits scheduling",
		fixture: "basic",
		args:    []string{"-p=1", "./pass", "./fail"},
	},
	{
		name:    "timeout",
		fixture: "timeout",
		args:    []string{"-timeout=1s", "./..."},
	},
	{
		name:    "race",
		fixture: "race",
		args:    []string{"-race", "./..."},
	},
}

// TestPrebuiltEquivalence runs each fixture through the go-test path and the
// prebuilt path. Both runs must produce the same go test -json events
// (ignoring timestamps, elapsed times, and timing text) and the same report
// (ignoring durations).
func TestPrebuiltEquivalence(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds and runs fixture modules")
	}

	for _, scenario := range prebuiltEquivalenceScenarios {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			require.NoError(t, os.CopyFS(
				root,
				os.DirFS(filepath.Join("testdata", "prebuilt", scenario.fixture)),
			))

			goTestEvents, goTestOrder, goTestReport := runPrebuiltEquivalenceCase(t, root, scenario.args, false)
			prebuiltEvents, prebuiltOrder, prebuiltReport := runPrebuiltEquivalenceCase(t, root, scenario.args, true)

			assert.Equal(t, goTestEvents, prebuiltEvents, "event sets differ")
			if scenario.ordered {
				assert.Equal(t, goTestOrder, prebuiltOrder, "test run order differs")
			}
			assert.Equal(t, goTestReport, prebuiltReport, "reports differ")
		})
	}
}

func runPrebuiltEquivalenceCase(
	t *testing.T,
	root string,
	args []string,
	buildOnce bool,
) (events, order []string, report *Report) {
	t.Helper()

	out := output.NewForTest(true, io.Discard, io.Discard, false)
	conf := &config.App{
		RepoRoot:           root,
		BuildOnce:          buildOnce,
		Iterations:         1,
		ParallelIterations: 1,
		SlowThreshold:      30 * time.Second,
	}
	state, start, resultsDir, err := RunIterations(
		context.Background(),
		conf,
		out,
		args,
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, state)
	if buildOnce {
		require.NotNil(t, state.Build)
		require.Equal(t, buildModePrebuilt, state.Build.Mode)
	}
	require.NoError(t, FinishDiagnoseAnalysis(
		context.Background(),
		conf,
		out,
		args,
		state,
		start,
		resultsDir,
	))

	report, err = ReadReport(resultsDir)
	require.NoError(t, err)
	events, order = normalizePrebuiltEvents(t, filepath.Join(resultsDir, "iteration-0.log.jsonl"))
	return events, order, normalizePrebuiltReport(report)
}

var (
	prebuiltDurationText = regexp.MustCompile(`\b\d+(?:\.\d+)?(?:ns|µs|ms|s)\b`)
	prebuiltHexAddress   = regexp.MustCompile(`0x[0-9a-f]+`)
	prebuiltGoroutineID  = regexp.MustCompile(`goroutine \d+`)
	prebuiltGoroutineRun = regexp.MustCompile(`Goroutine \d+ \((?:running|finished)\)`)
)

// normalizePrebuiltEvents turns an iteration log into sorted, timing-free
// event strings so both execution paths compare as multisets. It also returns
// test run order, which -shuffle changes.
func normalizePrebuiltEvents(t *testing.T, path string) (events, order []string) {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // G304: results dir from Runner
	require.NoError(t, err)

	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || line[0] != '{' {
			continue
		}
		var event TestEvent
		require.NoError(t, json.Unmarshal([]byte(line), &event), "log line: %s", line)
		if event.Action == "run" && event.Test != "" {
			order = append(order, event.Test)
		}
		events = append(events, normalizePrebuiltEvent(event))
	}
	sort.Strings(events)
	return events, order
}

func normalizePrebuiltEvent(event TestEvent) string {
	output := prebuiltDurationText.ReplaceAllString(event.Output, "DURATION")
	output = prebuiltHexAddress.ReplaceAllString(output, "0xADDR")
	output = prebuiltGoroutineID.ReplaceAllString(output, "goroutine N")
	output = prebuiltGoroutineRun.ReplaceAllString(output, "Goroutine N (state)")
	return strings.Join([]string{
		event.Action,
		event.Package,
		event.Test,
		output,
		event.FailedBuild,
	}, "\x00")
}

// normalizePrebuiltReport removes run metadata, build metrics, and every
// timing value so only classification decisions remain.
func normalizePrebuiltReport(report *Report) *Report {
	if report == nil {
		return nil
	}
	report.Run = nil
	report.Build = nil
	report.SlowThreshold = 0
	if report.Summary != nil {
		report.Summary.IterationDurationMin = 0
		report.Summary.IterationDurationMax = 0
		report.Summary.IterationDurationP50 = 0
	}
	for i := range report.IterationSummaries {
		report.IterationSummaries[i].Duration = 0
		report.IterationSummaries[i].ShuffleSeed = 0
		slices.Sort(report.IterationSummaries[i].FailingTests)
		slices.Sort(report.IterationSummaries[i].RacingTests)
	}
	for _, group := range [][]TestEntry{
		report.Flakes,
		report.Failures,
		report.Races,
		report.Timeouts,
		report.Slow,
		report.SlowestPackages,
	} {
		for i := range group {
			group[i].MinElapsed = 0
			group[i].MaxElapsed = 0
			group[i].P50Elapsed = 0
		}
		sortTestEntriesIfPresent(group)
	}
	sort.Slice(report.SlowestPackages, func(i, j int) bool {
		return report.SlowestPackages[i].Package < report.SlowestPackages[j].Package ||
			(report.SlowestPackages[i].Package == report.SlowestPackages[j].Package &&
				report.SlowestPackages[i].Test < report.SlowestPackages[j].Test)
	})
	return report
}

func sortTestEntriesIfPresent(entries []TestEntry) {
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Package < entries[j].Package ||
			(entries[i].Package == entries[j].Package && entries[i].Test < entries[j].Test)
	})
}
