package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// touchCmd is a shell snippet that creates a marker file at p, so a test can
// assert that the hook actually ran.
func touchCmd(p string) string { return "touch " + p }

func newCmdWithHookFlags(setup, teardown string) *cobra.Command {
	c := &cobra.Command{Use: "x"}
	c.Flags().String("global-setup", setup, "")
	c.Flags().String("global-teardown", teardown, "")
	c.SetContext(context.Background())
	return c
}

func TestWithGlobalHooksTeardownRunsOnSuccess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teardownMarker := filepath.Join(dir, "teardown")

	c := newCmdWithHookFlags("", touchCmd(teardownMarker))
	err := withGlobalHooks(func(*cobra.Command, []string) error { return nil })(c, nil)

	require.NoError(t, err)
	assert.FileExists(t, teardownMarker)
}

func TestWithGlobalHooksTeardownRunsOnRunError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teardownMarker := filepath.Join(dir, "teardown")

	wantErr := errors.New("run failed")
	c := newCmdWithHookFlags("", touchCmd(teardownMarker))
	err := withGlobalHooks(func(*cobra.Command, []string) error { return wantErr })(c, nil)

	require.ErrorIs(t, err, wantErr)
	assert.FileExists(t, teardownMarker, "teardown must run even when RunE errored")
}

func TestWithGlobalHooksTeardownErrorJoinsRunError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("run failed")
	c := newCmdWithHookFlags("", "exit 7")
	err := withGlobalHooks(func(*cobra.Command, []string) error { return wantErr })(c, nil)

	require.Error(t, err)
	require.ErrorIs(t, err, wantErr, "run error preserved")
	assert.Contains(t, err.Error(), "global teardown", "teardown failure surfaced")
}

func TestWithGlobalHooksSetupErrorSkipsRunAndTeardown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teardownMarker := filepath.Join(dir, "teardown")

	ran := false
	c := newCmdWithHookFlags("exit 9", touchCmd(teardownMarker))
	err := withGlobalHooks(func(*cobra.Command, []string) error {
		ran = true
		return nil
	})(c, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "global setup")
	assert.False(t, ran, "RunE must not be called when setup fails")
	_, statErr := os.Stat(teardownMarker)
	assert.True(t, os.IsNotExist(statErr), "teardown must not run when setup fails")
}
