package runner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/output"
)

func goldenBuildMetrics() *BuildMetrics {
	return &BuildMetrics{
		Mode:           buildModePrebuilt,
		Cache:          "cold",
		CompileWall:    41200 * time.Millisecond,
		CompileActions: 1910,
		CompileCached:  78,
		LinkWall:       78400 * time.Millisecond,
		VetTotal:       4 * time.Second,
		Binaries:       175,
		ExternalLink:   12,
		Stripped:       true,
		Floor:          78400 * time.Millisecond,
		SavedEstimate:  24*time.Minute + 50*time.Second,
	}
}

// requireBuildGolden compares got with testdata/build/<name>.golden. Set
// UPDATE_GOLDEN=1 to rewrite the file while iterating on the output.
func requireBuildGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "build", name+".golden")
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

func TestBuildOutputGoldenLive(t *testing.T) {
	t.Parallel()

	metrics := goldenBuildMetrics()
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	times := []time.Time{t0, t0.Add(12 * time.Second), t0, t0.Add(31 * time.Second)}
	next := 0

	var stderr strings.Builder
	out := output.NewForTest(false, io.Discard, &stderr, true)
	progress := newBuildProgress(out)
	progress.now = func() time.Time {
		at := times[next]
		next++
		return at
	}
	progress.tick = 0

	progress.begin()
	finish := progress.start("compiling", "compile started", nil)
	finish()
	printBuildCompiled(out, metrics)
	finish = progress.start(
		"linking",
		fmt.Sprintf("link started · %d test binaries", metrics.Binaries),
		func() (int, int) { return 37, metrics.Binaries },
	)
	finish()
	printBuildLinked(out, metrics)
	printBuildFloor(out, metrics, 20)

	requireBuildGolden(t, "live", stderr.String())
}

func TestBuildOutputGoldenNonTTY(t *testing.T) {
	t.Parallel()

	metrics := goldenBuildMetrics()

	var stderr strings.Builder
	out := output.NewForTest(false, io.Discard, &stderr, false)
	progress := newBuildProgress(out)

	progress.begin()
	finish := progress.start("compiling", "compile started", nil)
	finish()
	printBuildCompiled(out, metrics)
	finish = progress.start(
		"linking",
		fmt.Sprintf("link started · %d test binaries", metrics.Binaries),
		func() (int, int) { return 0, metrics.Binaries },
	)
	finish()
	printBuildLinked(out, metrics)
	printBuildFloor(out, metrics, 20)

	requireBuildGolden(t, "nontty", stderr.String())
}

func TestBuildOutputGoldenAI(t *testing.T) {
	t.Parallel()

	metrics := goldenBuildMetrics()

	var stderr strings.Builder
	out := output.NewForTest(true, io.Discard, &stderr, false)
	progress := newBuildProgress(out)

	progress.begin()
	finish := progress.start("compiling", "compile started", nil)
	finish()
	printBuildCompiled(out, metrics)
	finish = progress.start(
		"linking",
		fmt.Sprintf("link started · %d test binaries", metrics.Binaries),
		func() (int, int) { return 0, metrics.Binaries },
	)
	finish()
	printBuildLinked(out, metrics)
	printBuildFloor(out, metrics, 20)

	requireBuildGolden(t, "ai", stderr.String())
}

func TestBuildOutputGoldenSummary(t *testing.T) {
	t.Parallel()

	var summary strings.Builder
	printOverallStats(&summary, &Report{Build: goldenBuildMetrics()})
	requireBuildGolden(t, "summary", summary.String())
}
