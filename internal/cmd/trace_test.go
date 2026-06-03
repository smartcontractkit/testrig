package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/hooks"
)

func TestTraceCommand_NoArgsNoDir(t *testing.T) {
	t.Parallel()

	// 1. Arrange: Create an empty temp directory as the working directory
	tmpDir := t.TempDir()

	// 2. Act: Construct root command and run "trace" subcommand
	rootCmd := NewRootCommand(hooks.RunOptions{})

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"trace"})

	ctx := context.WithValue(context.Background(), wdKey, tmpDir)
	err := rootCmd.ExecuteContext(ctx)

	// 3. Assert: Since there's no trace.json or diagnose directory, it should fail
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no diagnose results directory found")
}

func TestFindLatestResultsDir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create dummy diagnose directories
	// d1 is lexicographically larger ("z") but older.
	// d2 is lexicographically smaller ("a") but newer.
	d1 := filepath.Join(tmpDir, "diagnose-z-20260603120000")
	d2 := filepath.Join(tmpDir, "diagnose-a-20260603130000")
	d3 := filepath.Join(tmpDir, "not-diagnose-dir")

	require.NoError(t, os.Mkdir(d1, 0700))
	require.NoError(t, os.Mkdir(d2, 0700))
	require.NoError(t, os.Mkdir(d3, 0700))

	now := time.Now()
	require.NoError(t, os.Chtimes(d1, now.Add(-time.Hour), now.Add(-time.Hour)))
	require.NoError(t, os.Chtimes(d2, now, now))

	// Find latest
	latest, err := findLatestResultsDir(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, d2, latest)
}

func TestTraceCommand_Flags(t *testing.T) {
	t.Parallel()
	rootCmd := NewRootCommand(hooks.RunOptions{})
	traceCmd, _, err := rootCmd.Find([]string{"trace"})
	require.NoError(t, err)
	require.NotNil(t, traceCmd.Flag("trace-addr"))
	assert.Equal(t, "127.0.0.1:9001", traceCmd.Flag("trace-addr").DefValue)
}
