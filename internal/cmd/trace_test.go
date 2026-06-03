package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/hooks"
)

func TestTraceCommand_NoArgsNoDir(t *testing.T) {
	t.Parallel()

	// 1. Arrange: Create an empty temp directory as the working directory
	tmpDir := t.TempDir()

	// Change working directory to the empty temp directory
	oldWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() {
		_ = os.Chdir(oldWd)
	}()

	// 2. Act: Construct root command and run "trace" subcommand
	rootCmd := NewRootCommand(hooks.RunOptions{})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"trace"})

	err = rootCmd.ExecuteContext(context.Background())

	// 3. Assert: Since there's no trace.json or diagnose directory, it should fail
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no diagnose results directory found")
}

func TestFindLatestResultsDir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create dummy diagnose directories
	d1 := filepath.Join(tmpDir, "diagnose-20260603120000")
	d2 := filepath.Join(tmpDir, "diagnose-20260603130000")
	d3 := filepath.Join(tmpDir, "not-diagnose-dir")

	require.NoError(t, os.Mkdir(d1, 0700))
	require.NoError(t, os.Mkdir(d2, 0700))
	require.NoError(t, os.Mkdir(d3, 0700))

	// Find latest
	latest, err := findLatestResultsDir(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, d2, latest)
}
