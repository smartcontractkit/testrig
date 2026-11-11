package runner

import (
	"context"
	"io"
)

// iterationExecutor writes one iteration's `go test -json` stream to w.
type iterationExecutor interface {
	Run(ctx context.Context, it iterationRun, w io.Writer) error
}

// iterationRun is the per-iteration input an executor needs. Env is the
// resource environment, appended to os.Environ() for child processes.
type iterationRun struct {
	Iteration   int
	ShuffleSeed int64
	Env         []string
}
