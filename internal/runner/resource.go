package runner

import "context"

// diagnoseIterationResource holds per-worker state for parallel diagnose runs.
// Env is appended to os.Environ for each iteration. Reset is called before a
// worker is reused. DumpDiagnostics is called after each iteration.
type diagnoseIterationResource struct {
	Env             []string
	Reset           func(context.Context) error
	DumpDiagnostics func(ctx context.Context, dir string, iteration int) error
}
