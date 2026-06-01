package hooks

import "context"

// Resource is one prepared, isolated piece of infrastructure for a test run
// (most commonly a database). It is the extension point consumers use to plug
// in setups that need per-worker isolation, reset between diagnose iterations,
// and post-iteration diagnostics — richer than the shell-style lifecycle hooks.
//
// All fields are optional:
//   - Env is appended to the child go test environment (e.g. CL_DATABASE_URL=...).
//   - Reset restores clean state before a diagnose worker is reused.
//   - DumpDiagnostics writes post-iteration state into the results dir.
//   - Cleanup tears the resource down once, after the command finishes.
type Resource struct {
	Env             []string
	Reset           func(context.Context) error
	DumpDiagnostics func(ctx context.Context, dir string, iteration int) error
	Cleanup         func() error
}

// ResourceProvider supplies count isolated resources for a run. It is called once,
// before any tests start, with a context that is canceled on SIGINT/SIGTERM.
//
// count is the effective parallel-iteration count for diagnose (one resource per
// worker, capped by --iterations). For the default root go test
// invocation and gotestsum, count is always 1 — even when go test uses -p>1,
// only a single Env slice is applied to the child process.
//
// The provider must return exactly count resources or an error. On error,
// testrig does not call Cleanup on any returned resources; roll back partial
// provisioning inside the provider before returning.
type ResourceProvider func(ctx context.Context, count int) ([]Resource, error)
