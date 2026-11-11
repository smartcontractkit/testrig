package compare

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/runner"
)

func TestCompareBuildGoldenPrebuiltVsPrebuilt(t *testing.T) {
	t.Parallel()

	before := buildReport("before-race", buildMetrics("warm", 58*time.Second, 12*time.Second), 33*time.Second)
	after := buildReport("after-race", buildMetrics("cold", 78*time.Second, 41*time.Second), 34*time.Second)
	requireCompareBuildGolden(t, "prebuilt-prebuilt", renderComparison(t, before, after))
}

func TestCompareBuildGoldenPrebuiltVsGoTest(t *testing.T) {
	t.Parallel()

	before := buildReport("before-race", buildMetrics("cold", 78*time.Second, 41*time.Second), 33*time.Second)
	after := buildReport("after-race", &runner.BuildMetrics{
		Mode:           buildModeGoTest,
		FallbackReason: "-coverprofile needs go test",
	}, 112*time.Second)
	requireCompareBuildGolden(t, "prebuilt-go-test", renderComparison(t, before, after))
}

func TestCompareBuildGoldenOldReportWithoutBuild(t *testing.T) {
	t.Parallel()

	before := buildReport("before-race", nil, 33*time.Second)
	after := buildReport("after-race", nil, 34*time.Second)
	requireCompareBuildGolden(t, "no-build", renderComparison(t, before, after))
}

func TestCompareBuildTableMarksMissingSide(t *testing.T) {
	t.Parallel()

	before := buildReport("before-race", buildMetrics("cold", 78*time.Second, 41*time.Second), 33*time.Second)
	after := buildReport("after-race", nil, 34*time.Second)
	out := renderComparison(t, before, after)

	require.Contains(t, out, "## Build")
	require.Contains(t, out, "| Mode | prebuilt | — |")
	require.Contains(t, out, "| Cache | cold | — |")
	require.Contains(t, out, "| Compile wall | 41s | — |")
	require.Contains(t, out, "| Link wall | 1m18s | — |")
	require.Contains(t, out, "| Floor | 1m18s | — |")
	require.Contains(t, out, "| Binaries | 175 | — |")
	require.Contains(t, out, "| External links | 12 | — |")
	require.Contains(t, out, "| Stripped | yes | — |")
	require.NotContains(t, out, "[!NOTE]")
}

func TestCompareBuildSpeedNoteOnlyForMixedModes(t *testing.T) {
	t.Parallel()

	prebuilt := buildReport("before-race", buildMetrics("cold", 78*time.Second, 41*time.Second), 33*time.Second)
	goTest := buildReport("after-race", &runner.BuildMetrics{Mode: buildModeGoTest}, 112*time.Second)
	otherPrebuilt := buildReport("after-race", buildMetrics("warm", 58*time.Second, 12*time.Second), 34*time.Second)

	mixed := renderComparison(t, prebuilt, goTest)
	require.Contains(t, mixed, "> [!NOTE]")
	require.Contains(t, mixed, "Iteration wall times are not comparable across build modes")

	same := renderComparison(t, prebuilt, otherPrebuilt)
	require.NotContains(t, same, "[!NOTE]")

	old := buildReport("after-race", nil, 34*time.Second)
	require.NotContains(t, renderComparison(t, prebuilt, old), "[!NOTE]")
}

func buildReport(name string, build *runner.BuildMetrics, wall time.Duration) *runner.Report {
	return &runner.Report{
		Iterations:    5,
		SlowThreshold: 30 * time.Second,
		Run: &runner.RunMeta{
			Name:               name,
			ResultsDirBasename: "diagnose-" + name,
			GoTestArgs:         []string{"./..."},
			DiagnoseIterations: 5,
			SlowThreshold:      30 * time.Second,
		},
		Build: build,
		Summary: &runner.ReportSummary{
			DistinctNamedTests:   1432,
			IterationDurationP50: wall,
			IterationDurationMax: wall + 3*time.Second,
		},
	}
}

func buildMetrics(cache string, link, compile time.Duration) *runner.BuildMetrics {
	return &runner.BuildMetrics{
		Mode:           "prebuilt",
		Cache:          cache,
		CompileActions: 1910,
		CompileCached:  78,
		CompileWall:    compile,
		CompileTotal:   39 * time.Second,
		LinkWall:       link,
		LinkTotal:      75 * time.Second,
		VetTotal:       4 * time.Second,
		Binaries:       175,
		ExternalLink:   12,
		Stripped:       true,
		Floor:          link,
		SavedEstimate:  24*time.Minute + 50*time.Second,
	}
}

// requireCompareBuildGolden compares got with testdata/build-compare/<name>.golden.
// Set UPDATE_GOLDEN=1 to rewrite the files while iterating on the output.
func requireCompareBuildGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "build-compare", name+".golden")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
		return
	}
	//nolint:gosec // G304: golden paths are fixed testdata files.
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden file %s; run with UPDATE_GOLDEN=1", path)
	require.Equal(t, string(want), got)
}
