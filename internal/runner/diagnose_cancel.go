package runner

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
)

type diagnoseCancelKey struct{}

type diagnoseCancelState struct {
	softStop   atomic.Bool
	hardCancel context.CancelFunc
	softStopCh chan struct{}
	softOnce   sync.Once
}

// NewDiagnoseRunContext returns a context for diagnose iteration runs with
// two-stage cancellation. The first SIGINT/SIGTERM requests a graceful stop
// (finish in-flight iterations, do not enqueue new ones). The second signal
// hard-cancels the context. Pass context.WithoutCancel(cmd.Context()) as parent
// so the root CLI signal handler does not cancel iteration execution on the
// first press.
func NewDiagnoseRunContext(parent context.Context) (context.Context, func()) {
	return newDiagnoseRunContext(parent, true)
}

// NewDiagnoseRunContextForTest is like NewDiagnoseRunContext but without OS
// signal handling. Use in unit and synctest tests with RequestDiagnoseGracefulStop
// and RequestDiagnoseHardCancel.
func NewDiagnoseRunContextForTest(parent context.Context) (context.Context, func()) {
	return newDiagnoseRunContext(parent, false)
}

func newDiagnoseRunContext(parent context.Context, listenSignals bool) (context.Context, func()) {
	ctx, hardCancel := context.WithCancel(parent)
	state := &diagnoseCancelState{
		hardCancel: hardCancel,
		softStopCh: make(chan struct{}),
	}
	ctx = context.WithValue(ctx, diagnoseCancelKey{}, state)

	stop := func() {
		hardCancel()
	}

	if !listenSignals {
		return ctx, stop
	}

	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	stop = func() {
		signal.Stop(sigCh)
		hardCancel()
	}

	go func() {
		defer signal.Stop(sigCh)
		for {
			select {
			case <-ctx.Done():
				return
			case <-sigCh:
				state.requestGracefulStop()
				select {
				case <-sigCh:
					hardCancel()
					return
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ctx, stop
}

func (s *diagnoseCancelState) requestGracefulStop() {
	s.softOnce.Do(func() {
		s.softStop.Store(true)
		close(s.softStopCh)
	})
}

func diagnoseCancelFromContext(ctx context.Context) *diagnoseCancelState {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(diagnoseCancelKey{}).(*diagnoseCancelState)
	return state
}

// DiagnoseGracefulStopChan returns a channel closed on the first graceful stop
// request, or nil when ctx is not from NewDiagnoseRunContext.
func DiagnoseGracefulStopChan(ctx context.Context) <-chan struct{} {
	state := diagnoseCancelFromContext(ctx)
	if state == nil {
		return nil
	}
	return state.softStopCh
}

// DiagnoseGracefulStopRequested reports whether the user requested a graceful
// stop (first Ctrl+C) on a context from NewDiagnoseRunContext.
func DiagnoseGracefulStopRequested(ctx context.Context) bool {
	state := diagnoseCancelFromContext(ctx)
	return state != nil && state.softStop.Load()
}

// RequestDiagnoseGracefulStop simulates the first interrupt for tests.
func RequestDiagnoseGracefulStop(ctx context.Context) {
	state := diagnoseCancelFromContext(ctx)
	if state != nil {
		state.requestGracefulStop()
	}
}

// RequestDiagnoseHardCancel simulates the second interrupt for tests.
func RequestDiagnoseHardCancel(ctx context.Context) {
	state := diagnoseCancelFromContext(ctx)
	if state != nil {
		state.hardCancel()
	}
}
