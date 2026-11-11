//go:build unix

package runner

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/hooks"
	"github.com/smartcontractkit/testrig/internal/output"
)

// TestPrebuiltCancelMidIteration cancels while one package is still running.
// The log must keep the package that already finished, the canceled package's
// process must be reaped, and the temp binary dir must be removed.
func TestPrebuiltCancelMidIteration(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs a fixture module")
	}

	root := t.TempDir()
	markerDir := t.TempDir()
	binRoot := t.TempDir()

	require.NoError(t, os.CopyFS(
		root,
		os.DirFS(filepath.Join("testdata", "prebuilt", "cancel")),
	))

	// os.MkdirTemp("", "testrig-bin-") uses TMPDIR, so this run's binary dir
	// lands in binRoot and the assertion below can prove it was removed.
	t.Setenv("TMPDIR", binRoot)

	resources := []hooks.Resource{{Env: []string{"TESTRIG_CANCEL_DIR=" + markerDir}}}
	out := output.NewForTest(true, io.Discard, io.Discard, false)
	conf := &config.App{
		RepoRoot:           root,
		BuildOnce:          true,
		Iterations:         1,
		ParallelIterations: 1,
		SlowThreshold:      30 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watchCtx, stopWatch := context.WithCancel(context.Background())
	var watch sync.WaitGroup
	watch.Go(func() {
		deadline := time.NewTimer(20 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-watchCtx.Done():
				return
			case <-deadline.C:
				t.Error("timed out waiting for the fixture to start")
				cancel()
				return
			case <-tick.C:
				if fileExists(filepath.Join(markerDir, "quick.done")) &&
					fileExists(filepath.Join(markerDir, "slow.pid")) {
					cancel()
					return
				}
			}
		}
	})
	t.Cleanup(func() {
		stopWatch()
		watch.Wait()
	})

	state, _, resultsDir, err := RunIterations(
		ctx,
		conf,
		out,
		[]string{"-p=2", "./quick", "./slow"},
		resources,
		nil,
		nil,
	)
	stopWatch()
	watch.Wait()
	require.NoError(t, err)
	require.NotNil(t, state)
	require.NotNil(t, state.Build)
	assert.Equal(t, buildModePrebuilt, state.Build.Mode)

	pidBytes, err := os.ReadFile(filepath.Join(markerDir, "slow.pid")) //nolint:gosec // G304: fixture output path
	require.NoError(t, err)
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	require.NoError(t, err)
	assert.Equal(t, syscall.ESRCH, syscall.Kill(pid, 0), "canceled test process %d is still running", pid)

	entries, err := os.ReadDir(binRoot)
	require.NoError(t, err)
	for _, entry := range entries {
		assert.False(t, strings.HasPrefix(entry.Name(), "testrig-bin-"), "binary dir left behind: %s", entry.Name())
	}

	logBytes, err := os.ReadFile(filepath.Join(resultsDir, "iteration-0.log.jsonl")) //nolint:gosec // G304: results dir
	require.NoError(t, err)
	events := decodePrebuiltEvents(t, string(logBytes))
	assert.True(t, hasTestEvent(events, "pass", "TestQuick"), "finished package is missing from the log")
	assert.True(t, hasTestEvent(events, "run", "TestSlow"), "canceled package never started")
	assert.False(t, hasTestEvent(events, "pass", "TestSlow"), "canceled package should not pass")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func hasTestEvent(events []TestEvent, action, test string) bool {
	for _, event := range events {
		if event.Action == action && event.Test == test {
			return true
		}
	}
	return false
}
