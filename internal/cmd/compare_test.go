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
	"github.com/smartcontractkit/testrig/internal/runner"
)

func TestCompareCommandWritesToStdout(t *testing.T) {
	t.Parallel()
	beforeDir := writeCompareReport(t, "diagnose-before", "before-race")
	afterDir := writeCompareReport(t, "diagnose-after", "after-race")

	rootCmd := NewRootCommand(hooks.RunOptions{})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"compare", beforeDir, afterDir})

	err := rootCmd.ExecuteContext(context.Background())
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "# testrig compare: before-race → after-race")
	assert.Contains(t, out, "## Summary")
}

func TestCompareCommandResolvesNamedRuns(t *testing.T) {
	t.Parallel()
	wd := t.TempDir()
	for _, name := range []string{"before", "after"} {
		dir := filepath.Join(wd, "diagnose-"+name)
		require.NoError(t, os.Mkdir(dir, 0700))
		require.NoError(t, runner.WriteReport(dir, &runner.Report{
			Iterations: 1,
			Run:        &runner.RunMeta{Name: name, ResultsDirBasename: "diagnose-" + name},
		}))
	}

	rootCmd := NewRootCommand(hooks.RunOptions{})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"compare", "before", "after"})

	ctx := context.WithValue(context.Background(), wdKey, wd)
	err := rootCmd.ExecuteContext(ctx)
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "# testrig compare: before → after")
}

func TestCompareCommandWritesOutputFile(t *testing.T) {
	t.Parallel()
	beforeDir := writeCompareReport(t, "diagnose-before", "before-race")
	afterDir := writeCompareReport(t, "diagnose-after", "after-race")
	outputPath := filepath.Join(t.TempDir(), "compare.md")

	rootCmd := NewRootCommand(hooks.RunOptions{})
	var stdout, stderr bytes.Buffer
	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)
	rootCmd.SetArgs([]string{"compare", beforeDir, afterDir, "-o", outputPath})

	err := rootCmd.ExecuteContext(context.Background())
	require.NoError(t, err)
	assert.Empty(t, stdout.String())
	assert.Equal(t, "wrote "+outputPath+"\n", stderr.String())

	b, err := os.ReadFile(outputPath) //nolint:gosec // G304: test-owned path
	require.NoError(t, err)
	assert.Contains(t, string(b), "# testrig compare: before-race → after-race")
}

func TestCompareCommandMissingReport(t *testing.T) {
	t.Parallel()
	rootCmd := NewRootCommand(hooks.RunOptions{})
	rootCmd.SetOut(&bytes.Buffer{})
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs([]string{"compare", t.TempDir(), t.TempDir()})

	err := rootCmd.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no report.json found")
}

func writeCompareReport(t *testing.T, dirName, runName string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), dirName)
	require.NoError(t, os.Mkdir(dir, 0700))
	require.NoError(t, runner.WriteReport(dir, &runner.Report{
		Iterations: 2,
		Run: &runner.RunMeta{
			ResultsDirBasename: dirName,
			Name:               runName,
			GoTestArgs:         []string{"./..."},
			DiagnoseIterations: 2,
		},
	}))
	return dir
}
