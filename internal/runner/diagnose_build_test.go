package runner

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/output"
)

func TestRunDiagnoseBuildPhaseFallbacks(t *testing.T) {
	t.Parallel()

	out := output.NewForTest(true, io.Discard, io.Discard, false)
	result, err := runDiagnoseBuildPhase(
		context.Background(),
		&config.App{BuildOnce: false},
		out,
		t.TempDir(),
		t.TempDir(),
		[]string{"./pkg"},
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, result.Metrics)
	assert.Equal(t, buildModeGoTest, result.Metrics.Mode)
	assert.Equal(t, "--build-once=false", result.Metrics.FallbackReason)
	assert.Empty(t, result.BinDir)
	assert.Nil(t, result.Executor)

	result, err = runDiagnoseBuildPhase(
		context.Background(),
		&config.App{BuildOnce: true},
		out,
		t.TempDir(),
		t.TempDir(),
		[]string{"-coverprofile=cover.out", "./pkg"},
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, result.Metrics)
	assert.Equal(t, buildModeGoTest, result.Metrics.Mode)
	assert.Contains(t, result.Metrics.FallbackReason, "-coverprofile needs go test")
	assert.Empty(t, result.BinDir)
	assert.Nil(t, result.Executor)
}

func TestPrintBuildLines(t *testing.T) {
	t.Parallel()

	metrics := &BuildMetrics{
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

	var stderr strings.Builder
	out := output.NewForTest(false, io.Discard, &stderr, false)
	printBuildCompiled(out, metrics)
	printBuildLinked(out, metrics)
	printBuildFloor(out, metrics, 20)
	plain := stripANSI(stderr.String())
	assert.Contains(t, plain, "compiled [41.20s] · 1910 packages · 78 cached · cold cache")
	assert.Contains(
		t,
		plain,
		"linked [1m18.40s] · 175 test binaries · 12 likely external link (cgo) · stripped · vet 4s",
	)
	assert.Contains(t, plain, "build floor 1m18.40s per iteration · saves ~24m50s over 20 iterations")

	stderr.Reset()
	out = output.NewForTest(true, io.Discard, &stderr, false)
	printBuildCompiled(out, metrics)
	printBuildLinked(out, metrics)
	ai := stderr.String()
	assert.Contains(t, ai, "build_compile s=41 cache=cold pkgs=1910 cached=78")
	assert.Contains(t, ai, "build_link s=78 bins=175 ext=12 strip=1 floor_s=78")
}

func TestCountTestBinaries(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, sub := range []string{"g0", "g1"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0o750))
	}
	for _, name := range []string{"g0/a.test", "g0/b.test.exe", "g1/c.test", "g1/not-a-binary"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600))
	}
	assert.Equal(t, 3, countTestBinaries(dir))
	assert.Equal(t, 0, countTestBinaries(""))
}

func TestOverallBuildLine(t *testing.T) {
	t.Parallel()

	prebuilt := &Report{Build: &BuildMetrics{
		Mode:          buildModePrebuilt,
		Cache:         "cold",
		CompileWall:   41 * time.Second,
		LinkWall:      78 * time.Second,
		Floor:         78 * time.Second,
		SavedEstimate: 24*time.Minute + 50*time.Second,
	}}
	assert.Equal(
		t,
		"build compile 41s (cold) · link 1m18s · floor 1m18s/iter · saved ~24m50s", overallBuildLine(prebuilt),
	)

	goTest := &Report{Build: &BuildMetrics{
		Mode:           buildModeGoTest,
		FallbackReason: "-coverprofile needs go test",
	}}
	assert.Equal(t, "build per iteration (-coverprofile needs go test)", overallBuildLine(goTest))

	assert.Empty(t, overallBuildLine(&Report{}))
}
