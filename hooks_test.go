package testrig

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGlobalSetupCallsHook(t *testing.T) {
	t.Parallel()
	called := false
	err := GlobalSetup(context.Background(), func(_ context.Context) error {
		called = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
}

func TestGlobalSetupPropagatesError(t *testing.T) {
	t.Parallel()
	want := errors.New("setup failed")
	err := GlobalSetup(context.Background(), func(_ context.Context) error { return want })
	assert.ErrorIs(t, err, want)
}

func TestGlobalTeardownCallsHook(t *testing.T) {
	t.Parallel()
	called := false
	err := GlobalTeardown(context.Background(), func(_ context.Context) error {
		called = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
}

func TestGlobalTeardownPropagatesError(t *testing.T) {
	t.Parallel()
	want := errors.New("teardown failed")
	err := GlobalTeardown(context.Background(), func(_ context.Context) error { return want })
	assert.ErrorIs(t, err, want)
}

func TestNilHookIsNoOp(t *testing.T) {
	t.Parallel()
	assert.NoError(t, GlobalSetup(context.Background(), nil))
	assert.NoError(t, GlobalTeardown(context.Background(), nil))
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

func TestIterationSetupCallsHook(t *testing.T) {
	t.Parallel()
	called := false
	err := IterationSetup(context.Background(), func(_ context.Context) error {
		called = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
}

func TestIterationSetupPropagatesError(t *testing.T) {
	t.Parallel()
	want := errors.New("iter setup failed")
	err := IterationSetup(context.Background(), func(_ context.Context) error { return want })
	assert.ErrorIs(t, err, want)
}

func TestIterationTeardownCallsHook(t *testing.T) {
	t.Parallel()
	called := false
	err := IterationTeardown(context.Background(), func(_ context.Context) error {
		called = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
}

func TestIterationTeardownPropagatesError(t *testing.T) {
	t.Parallel()
	want := errors.New("iter teardown failed")
	err := IterationTeardown(context.Background(), func(_ context.Context) error { return want })
	assert.ErrorIs(t, err, want)
}

func TestNilIterationHookIsNoOp(t *testing.T) {
	t.Parallel()
	assert.NoError(t, IterationSetup(context.Background(), nil))
	assert.NoError(t, IterationTeardown(context.Background(), nil))
}
