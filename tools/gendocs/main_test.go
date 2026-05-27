package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testHooksGo = `package testrig

type Hook func()
type Option func()

// GlobalSetup registers a hook to run once before any tests.
func GlobalSetup(h Hook) Option { return nil }

// GlobalTeardown registers a hook to run once after all tests finish.
func GlobalTeardown(h Hook) Option { return nil }

// IterationSetup registers a hook to run before each diagnose iteration.
func IterationSetup(h Hook) Option { return nil }

// IterationTeardown registers a hook to run after each diagnose iteration.
func IterationTeardown(h Hook) Option { return nil }
`

func TestWhenItRuns(t *testing.T) {
	t.Parallel()

	got, err := whenItRuns("GlobalSetup registers a hook to run once before any tests.")
	require.NoError(t, err)
	assert.Equal(t, "Run once before any tests", got)
}

func TestWhenItRuns_badDoc(t *testing.T) {
	t.Parallel()

	_, err := whenItRuns("GlobalSetup does something else.")
	require.Error(t, err)
}

func TestBuildHooksTable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "hooks.go")
	require.NoError(t, os.WriteFile(path, []byte(testHooksGo), 0o600))

	table, err := buildHooksTable(path)
	require.NoError(t, err)
	assert.Contains(t, table, "`testrig.GlobalSetup`")
	assert.Contains(t, table, "Run once before any tests")
	assert.Contains(t, table, "`--global-setup`")
	assert.Contains(t, table, "Run before each diagnose iteration")
}

func TestSplice_replacesTable(t *testing.T) {
	t.Parallel()

	const before = "head\n" + tableStart + "\n| old |\n" + tableEnd + "\ntail\n"
	const table = "| new | col |\n| --- | --- |"

	out, err := splice(before, table)
	require.NoError(t, err)
	assert.Contains(t, out, "| new | col |")
	assert.NotContains(t, out, "| old |")
}

func TestRun_generatesFromHooksGo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/test\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "hooks.go"), []byte(testHooksGo), 0o600))

	hooksMD := "# Hooks\n\n" + tableStart + "\n| stale |\n" + tableEnd + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "HOOKS.md"), []byte(hooksMD), 0o600))

	readme := "intro\n\n" + tableStart + "\n| stale |\n" + tableEnd + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte(readme), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tools/test"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tools/test/README.md"), []byte(readme), 0o600))

	wd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(root))
	t.Cleanup(func() { _ = os.Chdir(wd) })

	require.NoError(t, run())

	got, err := os.ReadFile(filepath.Join(root, "HOOKS.md")) //nolint:gosec // G304: test fixture
	require.NoError(t, err)
	body := string(got)
	assert.Contains(t, body, "Run once before any tests")
	assert.NotContains(t, body, "| stale |")
}
