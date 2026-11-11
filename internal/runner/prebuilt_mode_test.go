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

// TestGoTestModeReportsFallback checks the whole go-test path: the report
// records why the run fell back, the accent notice explains it, and the
// summary build line says iterations still build.
func TestGoTestModeReportsFallback(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("runs a fixture module")
	}

	root := t.TempDir()
	require.NoError(t, os.CopyFS(
		root,
		os.DirFS(filepath.Join("testdata", "prebuilt", "basic")),
	))

	var stderr strings.Builder
	out := output.NewForTest(false, io.Discard, &stderr, false)
	conf := &config.App{
		RepoRoot:           root,
		BuildOnce:          false,
		Iterations:         1,
		ParallelIterations: 1,
		SlowThreshold:      30 * time.Second,
	}

	state, start, resultsDir, err := RunIterations(
		context.Background(),
		conf,
		out,
		[]string{"./pass"},
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.NotNil(t, state.Build)
	assert.Equal(t, buildModeGoTest, state.Build.Mode)
	assert.Equal(t, "--build-once=false", state.Build.FallbackReason)

	require.NoError(t, FinishDiagnoseAnalysis(
		context.Background(),
		conf,
		out,
		[]string{"./pass"},
		state,
		start,
		resultsDir,
	))

	plain := stripANSI(stderr.String())
	assert.Contains(
		t,
		plain,
		"building per iteration: --build-once=false · iteration times include compile and link",
	)
	assert.Contains(t, plain, "build per iteration (--build-once=false)")

	report, err := ReadReport(resultsDir)
	require.NoError(t, err)
	require.NotNil(t, report.Build)
	assert.Equal(t, buildModeGoTest, report.Build.Mode)
	assert.Equal(t, "--build-once=false", report.Build.FallbackReason)
}
