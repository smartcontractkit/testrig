package runner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiagnoseGracefulStopRequested(t *testing.T) {
	t.Parallel()

	t.Run("background", func(t *testing.T) {
		t.Parallel()
		assert.False(t, DiagnoseGracefulStopRequested(context.Background()))
	})

	t.Run("diagnose_context_before_signal", func(t *testing.T) {
		t.Parallel()
		ctx, stop := NewDiagnoseRunContext(context.Background())
		defer stop()
		assert.False(t, DiagnoseGracefulStopRequested(ctx))
	})

	t.Run("diagnose_context_after_graceful_request", func(t *testing.T) {
		t.Parallel()
		ctx, stop := NewDiagnoseRunContext(context.Background())
		defer stop()
		RequestDiagnoseGracefulStop(ctx)
		assert.True(t, DiagnoseGracefulStopRequested(ctx))
	})
}

func TestDiagnoseHardCancel(t *testing.T) {
	t.Parallel()
	ctx, stop := NewDiagnoseRunContext(context.Background())
	defer stop()

	RequestDiagnoseHardCancel(ctx)
	require.Error(t, ctx.Err())
	assert.False(t, DiagnoseGracefulStopRequested(ctx))
}

func TestDiagnoseGracefulThenHardCancel(t *testing.T) {
	t.Parallel()
	ctx, stop := NewDiagnoseRunContext(context.Background())
	defer stop()

	RequestDiagnoseGracefulStop(ctx)
	assert.True(t, DiagnoseGracefulStopRequested(ctx))
	require.NoError(t, ctx.Err())

	RequestDiagnoseHardCancel(ctx)
	require.Error(t, ctx.Err())
}

func TestDiagnoseRunContextStopTearsDown(t *testing.T) {
	t.Parallel()
	ctx, stop := NewDiagnoseRunContext(context.Background())
	stop()
	require.Error(t, ctx.Err())
}
