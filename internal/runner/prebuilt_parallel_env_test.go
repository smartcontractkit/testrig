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
	"github.com/smartcontractkit/testrig/internal/hooks"
	"github.com/smartcontractkit/testrig/internal/output"
)

// TestPrebuiltParallelIterationsUseWorkerEnv runs three iterations on three
// workers, each with a distinct environment. Every prebuilt binary must see its
// own worker's environment, never another worker's.
func TestPrebuiltParallelIterationsUseWorkerEnv(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("builds and runs a fixture module")
	}

	root := t.TempDir()
	require.NoError(t, os.CopyFS(
		root,
		os.DirFS(filepath.Join("testdata", "prebuilt", "basic")),
	))
	envDir := t.TempDir()

	workerEnv := func(worker string) []string {
		return []string{
			"TESTRIG_WORKER=" + worker,
			"TESTRIG_WORKER_DIR=" + envDir,
			"TESTRIG_WORKER_EXPECT=3",
		}
	}
	resources := []hooks.Resource{
		{Env: workerEnv("w0")},
		{Env: workerEnv("w1")},
		{Env: workerEnv("w2")},
	}

	out := output.NewForTest(true, io.Discard, io.Discard, false)
	conf := &config.App{
		RepoRoot:           root,
		BuildOnce:          true,
		Iterations:         3,
		ParallelIterations: 3,
		SlowThreshold:      30 * time.Second,
	}

	state, _, _, err := RunIterations(
		context.Background(),
		conf,
		out,
		[]string{"-p=3", "./worker"},
		resources,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.NotNil(t, state.Build)
	assert.Equal(t, buildModePrebuilt, state.Build.Mode)
	assert.Equal(t, 3, state.Completed())

	data, err := os.ReadFile(filepath.Join(envDir, "workers")) //nolint:gosec // G304: fixture output path
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"w0", "w1", "w2"}, strings.Fields(string(data)))
}
