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

	"github.com/smartcontractkit/testrig/internal/hooks"
)

// touchCmd is a shell snippet that creates a marker file at p, so a test can
// assert that the hook actually ran.
func touchCmd(p string) string { return "touch " + p }

func newCmdWithHookFlags(setup, teardown string) *cobra.Command {
	root := &cobra.Command{Use: "testrig"}
	hooks.RegisterPersistentFlags(root.PersistentFlags())
	_ = root.PersistentFlags().Set("global-setup", setup)
	_ = root.PersistentFlags().Set("global-teardown", teardown)
	sub := &cobra.Command{Use: "x"}
	root.AddCommand(sub)
	sub.SetContext(context.Background())
	return sub
}

func TestWithGlobalHooksTeardownRunsOnSuccess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teardownMarker := filepath.Join(dir, "teardown")

	c := newCmdWithHookFlags("", touchCmd(teardownMarker))
	err := withGlobalHooks(hooks.RunOptions{}, func(*cobra.Command, []string) error { return nil })(c, nil)

	require.NoError(t, err)
	assert.FileExists(t, teardownMarker)
}

func TestWithGlobalHooksTeardownRunsOnRunError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teardownMarker := filepath.Join(dir, "teardown")

	wantErr := errors.New("run failed")
	c := newCmdWithHookFlags("", touchCmd(teardownMarker))
	err := withGlobalHooks(hooks.RunOptions{}, func(*cobra.Command, []string) error { return wantErr })(c, nil)

	require.ErrorIs(t, err, wantErr)
	assert.FileExists(t, teardownMarker, "teardown must run even when RunE errored")
}

func TestWithGlobalHooksTeardownErrorJoinsRunError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("run failed")
	c := newCmdWithHookFlags("", "exit 7")
	err := withGlobalHooks(hooks.RunOptions{}, func(*cobra.Command, []string) error { return wantErr })(c, nil)

	require.Error(t, err)
	require.ErrorIs(t, err, wantErr, "run error preserved")
	assert.Contains(t, err.Error(), "global teardown", "teardown failure surfaced")
}

func TestWithGlobalHooksRunsProgrammaticGlobalSetup(t *testing.T) {
	t.Parallel()
	ran := false
	opts := hooks.RunOptions{
		GlobalSetup: func(context.Context) error {
			ran = true
			return nil
		},
	}
	c := newCmdWithHookFlags("", "")
	err := withGlobalHooks(opts, func(*cobra.Command, []string) error { return nil })(c, nil)
	require.NoError(t, err)
	assert.True(t, ran)
}

func TestWithGlobalHooksSetupErrorSkipsRunAndTeardown(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	teardownMarker := filepath.Join(dir, "teardown")

	ran := false
	c := newCmdWithHookFlags("exit 9", touchCmd(teardownMarker))
	err := withGlobalHooks(hooks.RunOptions{}, func(*cobra.Command, []string) error {
		ran = true
		return nil
	})(c, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "global setup")
	assert.False(t, ran, "RunE must not be called when setup fails")
	_, statErr := os.Stat(teardownMarker)
	assert.True(t, os.IsNotExist(statErr), "teardown must not run when setup fails")
}
