package hooks

import (
	"context"
	"errors"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunGlobalSetupCallsHook(t *testing.T) {
	t.Parallel()
	called := false
	err := RunGlobalSetup(context.Background(), func(_ context.Context) error {
		called = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
}

func TestRunGlobalSetupPropagatesError(t *testing.T) {
	t.Parallel()
	want := errors.New("setup failed")
	err := RunGlobalSetup(context.Background(), func(_ context.Context) error { return want })
	assert.ErrorIs(t, err, want)
}

func TestRunGlobalTeardownCallsHook(t *testing.T) {
	t.Parallel()
	called := false
	err := RunGlobalTeardown(context.Background(), func(_ context.Context) error {
		called = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
}

func TestRunGlobalTeardownPropagatesError(t *testing.T) {
	t.Parallel()
	want := errors.New("teardown failed")
	err := RunGlobalTeardown(context.Background(), func(_ context.Context) error { return want })
	assert.ErrorIs(t, err, want)
}

func TestNilHookIsNoOp(t *testing.T) {
	t.Parallel()
	assert.NoError(t, RunGlobalSetup(context.Background(), nil))
	assert.NoError(t, RunGlobalTeardown(context.Background(), nil))
}

func TestNewShellHookRunsCommand(t *testing.T) {
	t.Parallel()
	h := NewShellHook("true")
	require.NoError(t, h(context.Background()))
}

func TestNewShellHookPropagatesExitError(t *testing.T) {
	t.Parallel()
	h := NewShellHook("false")
	require.Error(t, h(context.Background()))
}

func TestNewShellHookErrorIncludesStderr(t *testing.T) {
	t.Parallel()
	h := NewShellHook("echo boom 1>&2; exit 1")
	err := h(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestNewShellHookErrorIncludesStdout(t *testing.T) {
	t.Parallel()
	h := NewShellHook("echo loud-on-stdout; exit 2")
	err := h(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loud-on-stdout")
}

func TestNewShellHookSuccessReturnsNoError(t *testing.T) {
	t.Parallel()
	h := NewShellHook("echo hello")
	require.NoError(t, h(context.Background()))
}

func TestRunIterationSetupCallsHook(t *testing.T) {
	t.Parallel()
	called := false
	err := RunIterationSetup(context.Background(), func(_ context.Context) error {
		called = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
}

func TestRunIterationSetupPropagatesError(t *testing.T) {
	t.Parallel()
	want := errors.New("iter setup failed")
	err := RunIterationSetup(context.Background(), func(_ context.Context) error { return want })
	assert.ErrorIs(t, err, want)
}

func TestRunIterationTeardownCallsHook(t *testing.T) {
	t.Parallel()
	called := false
	err := RunIterationTeardown(context.Background(), func(_ context.Context) error {
		called = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
}

func TestRunIterationTeardownPropagatesError(t *testing.T) {
	t.Parallel()
	want := errors.New("iter teardown failed")
	err := RunIterationTeardown(context.Background(), func(_ context.Context) error { return want })
	assert.ErrorIs(t, err, want)
}

func TestNilIterationHookIsNoOp(t *testing.T) {
	t.Parallel()
	assert.NoError(t, RunIterationSetup(context.Background(), nil))
	assert.NoError(t, RunIterationTeardown(context.Background(), nil))
}

func TestBuildOptionsRegistersHooks(t *testing.T) {
	t.Parallel()
	setup := func(context.Context) error { return nil }
	opts := BuildOptions(
		GlobalSetup(setup),
		GlobalTeardown(setup),
		IterationSetup(setup),
		IterationTeardown(setup),
	)
	require.NotNil(t, opts.GlobalSetup)
	require.NotNil(t, opts.GlobalTeardown)
	require.NotNil(t, opts.IterationSetup)
	require.NotNil(t, opts.IterationTeardown)
}

func TestBuildOptionsRegistersResourceProvider(t *testing.T) {
	t.Parallel()
	provider := func(context.Context, int) ([]Resource, error) { return nil, nil }
	opts := BuildOptions(WithResources(provider))
	require.NotNil(t, opts.ResourceProvider)
}

func TestBuildOptionsRegistersCommands(t *testing.T) {
	t.Parallel()
	opts := BuildOptions(
		WithCommand(&cobra.Command{Use: "one"}),
		WithCommand(&cobra.Command{Use: "two"}),
	)
	require.Len(t, opts.Commands, 2)
	assert.Equal(t, "one", opts.Commands[0].Use)
	assert.Equal(t, "two", opts.Commands[1].Use)
}

func TestBuildOptionsWithCommandIgnoresNil(t *testing.T) {
	t.Parallel()
	opts := BuildOptions(
		WithCommand(nil),
		WithCommand(&cobra.Command{Use: "one"}),
	)
	require.Len(t, opts.Commands, 1)
	assert.Equal(t, "one", opts.Commands[0].Use)
}

func TestBuildOptionsRegistersRootFlags(t *testing.T) {
	t.Parallel()
	opts := BuildOptions(WithRootFlags(func(fs *pflag.FlagSet) {
		fs.String("database-url", "", "")
	}))
	require.NotNil(t, opts.RootFlags)

	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	opts.RootFlags(fs)
	assert.NotNil(t, fs.Lookup("database-url"))
}
