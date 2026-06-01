package runner

import (
	"context"

	"github.com/smartcontractkit/testrig/internal/hooks"
)

// diagnoseIterationResource holds per-worker state for parallel diagnose runs.
// Env is appended to os.Environ for each iteration. Reset is called before a
// worker is reused. DumpDiagnostics is called after each iteration.
type diagnoseIterationResource struct {
	Env             []string
	Reset           func(context.Context) error
	DumpDiagnostics func(ctx context.Context, dir string, iteration int) error
}

// toIterationResources maps consumer-supplied hooks.Resource values onto the
// per-worker diagnose state. Cleanup is intentionally dropped — the command
// layer owns resource teardown after the run completes.
func toIterationResources(resources []hooks.Resource) []diagnoseIterationResource {
	if len(resources) == 0 {
		return nil
	}
	out := make([]diagnoseIterationResource, len(resources))
	for i, r := range resources {
		out[i] = diagnoseIterationResource{
			Env:             r.Env,
			Reset:           r.Reset,
			DumpDiagnostics: r.DumpDiagnostics,
		}
	}
	return out
}
