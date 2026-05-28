package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"text/tabwriter"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"

	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/output"
)

const (
	benchDummyTarget   = "./internal/runner/testdata/dummy/..."
	benchDogfoodTarget = "./..."
)

// baselineWorkload runs the raw `go test -json` floor for one diagnose-equivalent
// workload: `iterations` invocations against target, at most `parallel` running
// concurrently (mirroring how Diagnose schedules iterations across workers).
func baselineWorkload(ctx context.Context, repoRoot, target string, iterations, parallel int) error {
	if parallel < 1 {
		parallel = 1
	}
	sem := semaphore.NewWeighted(int64(parallel))
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	for range iterations {
		if err := sem.Acquire(ctx, 1); err != nil {
			wg.Wait()
			return err
		}
		wg.Go(func() {
			defer sem.Release(1)
			//nolint:gosec // G204: target is fixed per test (dummy or ./...)
			cmd := exec.CommandContext(ctx, "go", "test", "-json", "-count=1", target)
			cmd.Dir = repoRoot
			cmd.Env = envWithoutKey(os.Environ(), overheadMatrixEnv)
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			if err := cmd.Run(); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return firstErr
}

// diagnoseWorkload runs one Diagnose call against target with the given iteration
// count and parallelism. Output is discarded.
func diagnoseWorkload(
	ctx context.Context,
	out *output.Printer,
	repoRoot, target string,
	iterations, parallel int,
) error {
	conf := &config.App{
		RepoRoot:           repoRoot,
		Iterations:         iterations,
		ParallelIterations: parallel,
		SlowThreshold:      time.Second,
	}
	return Diagnose(ctx, conf, out, []string{target}, nil, nil)
}

func envWithoutKey(env []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env))
	for _, e := range env {
		if e == key || strings.HasPrefix(e, prefix) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// existingDiagnoseDirs lists the diagnose-* result dirs currently in repoRoot.
func existingDiagnoseDirs(repoRoot string) []string {
	matches, _ := filepath.Glob(filepath.Join(repoRoot, "diagnose-*"))
	return matches
}

// cleanupNewDiagnoseDirs removes any diagnose-* result dirs created during the
// benchmark, so repeated runs don't accumulate output dirs in the repo root.
func cleanupNewDiagnoseDirs(tb testing.TB, repoRoot string) {
	tb.Helper()
	before := make(map[string]struct{})
	for _, d := range existingDiagnoseDirs(repoRoot) {
		before[d] = struct{}{}
	}
	tb.Cleanup(func() {
		for _, d := range existingDiagnoseDirs(repoRoot) {
			if _, ok := before[d]; !ok {
				_ = os.RemoveAll(d)
			}
		}
	})
}

// BenchmarkBaselineGoTest is the floor: raw `go test -json` against the same
// target Diagnose runs. Subtract its ns/op, B/op, allocs/op from
// BenchmarkDiagnose to read the overhead Diagnose adds.
func BenchmarkBaselineGoTest(b *testing.B) {
	repoRoot, err := filepath.Abs("../..")
	require.NoError(b, err)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		require.NoError(b, baselineWorkload(ctx, repoRoot, benchDummyTarget, 1, 1))
	}
}

// BenchmarkDiagnose runs one Diagnose iteration against the same target as
// BenchmarkBaselineGoTest. ns/op minus baseline is the overhead Diagnose adds.
func BenchmarkDiagnose(b *testing.B) {
	repoRoot, err := filepath.Abs("../..")
	require.NoError(b, err)
	cleanupNewDiagnoseDirs(b, repoRoot)

	out := output.NewForTest(true, io.Discard, io.Discard, false)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		require.NoError(b, diagnoseWorkload(ctx, out, repoRoot, benchDummyTarget, 1, 1))
	}
}

// overheadConfig is one (iterations, parallel) point in the overhead matrix.
type overheadConfig struct {
	iterations int
	parallel   int
}

// overheadRow pairs a config with its measured baseline and diagnose results.
type overheadRow struct {
	cfg      overheadConfig
	baseline testing.BenchmarkResult
	diagnose testing.BenchmarkResult
}

// overheadMatrix is the set of (iterations, parallel) points measured by
// BenchmarkDiagnoseOverhead: single run, sequential iterations, then parallel.
var overheadMatrix = []overheadConfig{
	{iterations: 1, parallel: 1},
	{iterations: 4, parallel: 1},
	{iterations: 4, parallel: 4},
	{iterations: 8, parallel: 1},
	{iterations: 8, parallel: 8},
}

// overheadMatrixEnv gates TestDiagnoseOverhead_*; it spawns many `go test`
// subprocesses and is too slow for the normal test run.
const overheadMatrixEnv = "TESTRIG_BENCH_OVERHEAD"

func skipUnlessDiagnoseOverheadMatrix(t *testing.T) {
	t.Helper()
	if os.Getenv(overheadMatrixEnv) == "" {
		t.Skipf("set %s=1 to run the diagnose overhead matrix", overheadMatrixEnv)
	}
	if testing.Short() {
		t.Skip("skipping diagnose overhead matrix in short mode")
	}
}

// runDiagnoseOverheadMatrix measures Diagnose overhead vs the raw `go test` floor
// across overheadMatrix for target and logs a diff table. It is a test helper, not
// a Benchmark, because it drives testing.Benchmark internally (which deadlocks if
// called from a benchmark).
//
//nolint:paralleltest // serial by design: spawns many go test subprocesses and measures wall time.
func runDiagnoseOverheadMatrix(t *testing.T, label, target string) {
	t.Helper()
	skipUnlessDiagnoseOverheadMatrix(t)

	repoRoot, err := filepath.Abs("../..")
	require.NoError(t, err)
	cleanupNewDiagnoseDirs(t, repoRoot)

	// Child `go test` processes must not see TESTRIG_BENCH_OVERHEAD (dogfood runs ./...).
	t.Setenv(overheadMatrixEnv, "")

	out := output.NewForTest(true, io.Discard, io.Discard, false)
	ctx := context.Background()

	rows := make([]overheadRow, 0, len(overheadMatrix))
	for _, cfg := range overheadMatrix {
		base := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				require.NoError(b, baselineWorkload(ctx, repoRoot, target, cfg.iterations, cfg.parallel))
			}
		})
		require.NotZero(t, base.N, "baseline workload failed for %+v", cfg)

		diag := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				require.NoError(b, diagnoseWorkload(ctx, out, repoRoot, target, cfg.iterations, cfg.parallel))
			}
		})
		require.NotZero(t, diag.N, "diagnose workload failed for %+v", cfg)

		rows = append(rows, overheadRow{cfg: cfg, baseline: base, diagnose: diag})
	}
	printDiagnoseOverhead(t, label, target, rows)
}

// TestDiagnoseOverhead_Dummy runs the overhead matrix against the tiny dummy package.
// Run via `just bench_overhead_matrix_dummy`.
//
//nolint:paralleltest // serial by design: spawns many go test subprocesses and measures wall time.
func TestDiagnoseOverhead_Dummy(t *testing.T) {
	runDiagnoseOverheadMatrix(t, "dummy", benchDummyTarget)
}

// TestDiagnoseOverhead_Dogfood runs the overhead matrix against the full testrig module (./...).
// Run via `just bench_overhead_matrix_dogfood`; expect much longer wall time than dummy.
//
//nolint:paralleltest // serial by design: spawns many go test subprocesses and measures wall time.
func TestDiagnoseOverhead_Dogfood(t *testing.T) {
	runDiagnoseOverheadMatrix(t, "dogfood", benchDogfoodTarget)
}

// roundedDur renders ns as a duration rounded to microseconds for the table.
func roundedDur(ns int64) string {
	return time.Duration(ns).Round(time.Microsecond).String()
}

// overheadDur renders overhead; negative deltas (noise) show as 0.
func overheadDur(ns int64) string {
	if ns < 0 {
		ns = 0
	}
	return roundedDur(ns)
}

// printDiagnoseOverhead logs a table of baseline vs diagnose wall time per config.
// overhead = diagnose ns/op - baseline ns/op; overhead/iter divides by iterations.
func printDiagnoseOverhead(t *testing.T, label, target string, rows []overheadRow) {
	t.Helper()
	var sb strings.Builder
	tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "iters\tparallel\tbaseline\tdiagnose\toverhead\toverhead/iter")
	for _, r := range rows {
		overheadNs := r.diagnose.NsPerOp() - r.baseline.NsPerOp()
		perIterNs := overheadNs / int64(max(r.cfg.iterations, 1))
		_, _ = fmt.Fprintf(tw, "%d\t%d\t%s\t%s\t%s\t%s\n",
			r.cfg.iterations,
			r.cfg.parallel,
			roundedDur(r.baseline.NsPerOp()),
			roundedDur(r.diagnose.NsPerOp()),
			overheadDur(overheadNs),
			overheadDur(perIterNs),
		)
	}
	_ = tw.Flush()
	t.Logf("\nDiagnose overhead vs raw go test (%s, target=%s):\n%s", label, target, sb.String())
}

func BenchmarkResolveModuleDir(b *testing.B) {
	repoRoot, err := filepath.Abs("../..")
	require.NoError(b, err)
	args := []string{"./internal/runner/..."}

	for b.Loop() {
		_, _, err := resolveModuleDir(repoRoot, args)
		require.NoError(b, err)
	}
}
