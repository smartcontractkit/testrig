package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/hooks"
)

func TestResourceEnvReturnsProviderEnv(t *testing.T) {
	t.Parallel()
	want := []string{"TESTRIG_TEST_ENV=1"}
	opts := hooks.RunOptions{
		ResourceProvider: func(context.Context, int) ([]hooks.Resource, error) {
			return []hooks.Resource{{Env: want}}, nil
		},
	}
	env, cleanup, err := resourceEnv(context.Background(), opts)
	require.NoError(t, err)
	defer func() { require.NoError(t, cleanup()) }()
	assert.Equal(t, want, env)
}

func testRootWithSubcommand(t *testing.T, rootUse string) (*cobra.Command, *cobra.Command) {
	t.Helper()
	root := &cobra.Command{Use: rootUse}
	sub := &cobra.Command{Use: "sub"}
	root.AddCommand(sub)
	return root, sub
}

func TestFinishResourceCleanup(t *testing.T) {
	t.Parallel()

	t.Run("sets err when run succeeded", func(t *testing.T) {
		t.Parallel()
		_, sub := testRootWithSubcommand(t, "testrig")
		var err error
		finishResourceCleanup(sub, &err, func() error { return errors.New("cleanup") })
		require.Error(t, err)
	})

	t.Run("preserves run error", func(t *testing.T) {
		t.Parallel()
		_, sub := testRootWithSubcommand(t, "testrig")
		runErr := errors.New("run")
		err := runErr
		finishResourceCleanup(sub, &err, func() error { return errors.New("cleanup") })
		require.ErrorIs(t, err, runErr)
	})
}

func TestFinishResourceCleanupStderrUsesRootCLIName(t *testing.T) {
	t.Parallel()
	rootCmd := NewRootCommand(hooks.RunOptions{RootCommand: "cltest"})
	var sub *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "gotestsum" {
			sub = c
			break
		}
	}
	require.NotNil(t, sub)

	var buf bytes.Buffer
	sub.SetErr(&buf)

	var runErr error
	finishResourceCleanup(sub, &runErr, func() error { return errors.New("cleanup failed") })
	assert.Contains(t, buf.String(), "cltest: resource cleanup failed:")
}

func TestProvisionResourcesNilProviderIsNoop(t *testing.T) {
	t.Parallel()
	res, cleanup, err := provisionResources(context.Background(), hooks.RunOptions{}, 1)
	require.NoError(t, err)
	assert.Empty(t, res)
	require.NotNil(t, cleanup)
	err = cleanup() // must not panic
	require.NoError(t, err)
}

func TestProvisionResourcesCallsProviderWithCount(t *testing.T) {
	t.Parallel()
	var gotCount int
	provider := func(_ context.Context, count int) ([]hooks.Resource, error) {
		gotCount = count
		return make([]hooks.Resource, count), nil
	}
	res, cleanup, err := provisionResources(
		context.Background(),
		hooks.RunOptions{ResourceProvider: provider},
		3,
	)
	require.NoError(t, err)
	defer func() { require.NoError(t, cleanup()) }()
	assert.Equal(t, 3, gotCount)
	assert.Len(t, res, 3)
}

func TestProvisionResourcesCleanupRunsEveryResource(t *testing.T) {
	t.Parallel()
	var cleaned atomic.Int32
	provider := func(_ context.Context, count int) ([]hooks.Resource, error) {
		rs := make([]hooks.Resource, count)
		for i := range rs {
			rs[i] = hooks.Resource{Cleanup: func() error { cleaned.Add(1); return nil }}
		}
		return rs, nil
	}
	_, cleanup, err := provisionResources(
		context.Background(),
		hooks.RunOptions{ResourceProvider: provider},
		2,
	)
	require.NoError(t, err)
	require.NoError(t, cleanup())
	assert.Equal(t, int32(2), cleaned.Load())
}

func TestProvisionResourcesPropagatesProviderError(t *testing.T) {
	t.Parallel()
	want := errors.New("provision boom")
	provider := func(context.Context, int) ([]hooks.Resource, error) { return nil, want }
	_, cleanup, err := provisionResources(
		context.Background(),
		hooks.RunOptions{ResourceProvider: provider},
		1,
	)
	require.ErrorIs(t, err, want)
	if cleanup != nil {
		_ = cleanup()
	}
}

func TestProvisionResourcesCleanupReturnsJoinedErrors(t *testing.T) {
	t.Parallel()
	err1 := errors.New("err 1")
	err2 := errors.New("err 2")
	provider := func(_ context.Context, _ int) ([]hooks.Resource, error) {
		return []hooks.Resource{
			{Cleanup: func() error { return err1 }},
			{Cleanup: func() error { return nil }},
			{Cleanup: func() error { return err2 }},
		}, nil
	}

	_, cleanup, err := provisionResources(context.Background(), hooks.RunOptions{ResourceProvider: provider}, 3)
	require.NoError(t, err)
	err = cleanup()
	require.ErrorIs(t, err, err1)
	require.ErrorIs(t, err, err2)
}

func TestProvisionResourcesRejectsWrongCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		returned  int
		requested int
	}{
		{name: "too few", returned: 1, requested: 3},
		{name: "too many", returned: 4, requested: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var cleaned atomic.Int32
			provider := func(context.Context, int) ([]hooks.Resource, error) {
				rs := make([]hooks.Resource, tt.returned)
				for i := range rs {
					rs[i] = hooks.Resource{Cleanup: func() error {
						cleaned.Add(1)
						return nil
					}}
				}
				return rs, nil
			}
			_, cleanup, err := provisionResources(
				context.Background(),
				hooks.RunOptions{ResourceProvider: provider},
				tt.requested,
			)
			require.Error(t, err)
			assert.Contains(t, err.Error(), fmt.Sprintf("returned %d resources, want %d", tt.returned, tt.requested))
			assert.EqualValues(t, tt.returned, cleaned.Load(), "partial resources must be cleaned up")
			require.NotNil(t, cleanup)
			require.NoError(t, cleanup())
		})
	}
}

func TestProvisionResourcesCleanupRunsConcurrently(t *testing.T) {
	t.Parallel()
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	var wg sync.WaitGroup
	wg.Add(3)

	provider := func(_ context.Context, count int) ([]hooks.Resource, error) {
		rs := make([]hooks.Resource, count)
		for i := range rs {
			rs[i] = hooks.Resource{Cleanup: func() error {
				current := inFlight.Add(1)
				for {
					currMax := maxInFlight.Load()
					if current > currMax {
						if maxInFlight.CompareAndSwap(currMax, current) {
							break
						}
					} else {
						break
					}
				}
				wg.Done()
				wg.Wait() // all 3 must arrive to proceed
				inFlight.Add(-1)
				return nil
			}}
		}
		return rs, nil
	}

	_, cleanup, err := provisionResources(context.Background(), hooks.RunOptions{ResourceProvider: provider}, 3)
	require.NoError(t, err)
	err = cleanup()
	require.NoError(t, err)
	assert.Equal(t, int32(3), maxInFlight.Load())
}
