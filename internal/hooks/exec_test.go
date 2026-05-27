package hooks

import (
	"context"
	"errors"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunEntryNativeThenShell(t *testing.T) {
	t.Parallel()

	e, ok := EntryByName("GlobalSetup")
	require.True(t, ok)

	nativeRan := false
	opts := RunOptions{
		GlobalSetup: func(context.Context) error {
			nativeRan = true
			return nil
		},
	}
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String(e.Flag, "", "")
	require.NoError(t, fs.Set(e.Flag, "true"))

	require.NoError(t, RunEntry(context.Background(), fs, opts, "", e, NativeThenShell))
	assert.True(t, nativeRan)
}

func TestRunEntryShellThenNative(t *testing.T) {
	t.Parallel()

	e, ok := EntryByName("GlobalTeardown")
	require.True(t, ok)

	nativeRan := false
	opts := RunOptions{
		GlobalTeardown: func(context.Context) error {
			nativeRan = true
			return nil
		},
	}
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	fs.String(e.Flag, "", "")
	require.NoError(t, fs.Set(e.Flag, "true"))

	require.NoError(t, RunEntry(context.Background(), fs, opts, "", e, ShellThenNative))
	assert.True(t, nativeRan)
}

func TestRunGlobalSetupsPropagatesError(t *testing.T) {
	t.Parallel()

	want := errors.New("setup failed")
	opts := RunOptions{
		GlobalSetup: func(context.Context) error { return want },
	}
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	RegisterPersistentFlags(fs)

	err := RunGlobalSetups(context.Background(), fs, opts)
	assert.ErrorIs(t, err, want)
}
