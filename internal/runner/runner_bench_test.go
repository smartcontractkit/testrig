package runner

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"text/tabwriter"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/sync/semaphore"

	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/output"
	"github.com/smartcontractkit/testrig/modresolve"
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
	return Diagnose(ctx, conf, out, []string{target}, nil, nil, nil)
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

// overheadMatrixRunsEnv sets how many times each matrix cell is benchmarked
// before averaging (default 5). Use 3–5 for stabler numbers; 1 for a quick smoke.
const overheadMatrixRunsEnv = "TESTRIG_BENCH_OVERHEAD_RUNS"

const (
	overheadMatrixRunsDefault = 5
	overheadMatrixRunsMax     = 10
)

func skipUnlessDiagnoseOverheadMatrix(t *testing.T) {
	t.Helper()
	if os.Getenv(overheadMatrixEnv) == "" {
		t.Skipf("set %s=1 to run the diagnose overhead matrix", overheadMatrixEnv)
	}
	if testing.Short() {
		t.Skip("skipping diagnose overhead matrix in short mode")
	}
}

func overheadMatrixRuns() int {
	s := strings.TrimSpace(os.Getenv(overheadMatrixRunsEnv))
	if s == "" {
		return overheadMatrixRunsDefault
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return overheadMatrixRunsDefault
	}
	return min(n, overheadMatrixRunsMax)
}

// averageBenchmarkResults averages per-op metrics across repeated benchmark runs.
func averageBenchmarkResults(results []testing.BenchmarkResult) testing.BenchmarkResult {
	if len(results) == 0 {
		return testing.BenchmarkResult{}
	}
	var ns, bytes, allocs int64
	for _, r := range results {
		ns += r.NsPerOp()
		bytes += r.AllocedBytesPerOp()
		allocs += r.AllocsPerOp()
	}
	n := int64(len(results))
	avgBytes := bytes / n
	avgAllocs := allocs / n
	if avgBytes < 0 {
		avgBytes = 0
	}
	if avgAllocs < 0 {
		avgAllocs = 0
	}
	return testing.BenchmarkResult{
		N:         1,
		T:         time.Duration(ns / n),
		MemBytes:  uint64(avgBytes),
		MemAllocs: uint64(avgAllocs),
	}
}

// repeatBenchmark runs fn runs times and returns the averaged result.
// phase labels log lines; pass "" to omit per-run logging.
func repeatBenchmark(t *testing.T, runs int, phase string, fn func() testing.BenchmarkResult) testing.BenchmarkResult {
	t.Helper()
	results := make([]testing.BenchmarkResult, runs)
	for i := range runs {
		start := time.Now()
		results[i] = fn()
		if phase != "" {
			t.Logf("%s: run %d/%d done (wall %s, %s/op)",
				phase, i+1, runs, time.Since(start).Round(time.Second), roundedDur(results[i].NsPerOp()))
		}
	}
	avg := averageBenchmarkResults(results)
	if phase != "" {
		t.Logf("%s: mean %s/op over %d runs", phase, roundedDur(avg.NsPerOp()), runs)
	}
	return avg
}

// runDiagnoseOverheadMatrix measures Diagnose overhead vs the raw `go test` floor
// across overheadMatrix for target and logs a diff table. It is a test helper, not
// a Benchmark, because it drives testing.Benchmark internally (which deadlocks if
// called from a benchmark).
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
	runs := overheadMatrixRuns()
	total := len(overheadMatrix)
	t.Logf("[%s] overhead matrix: target=%s, %d cells, %d runs/cell (%s overrides)",
		label, target, total, runs, overheadMatrixRunsEnv)

	rows := make([]overheadRow, 0, total)
	for cell, cfg := range overheadMatrix {
		cellLabel := fmt.Sprintf("[%s] cell %d/%d iters=%d parallel=%d",
			label, cell+1, total, cfg.iterations, cfg.parallel)

		base := repeatBenchmark(t, runs, cellLabel+" baseline", func() testing.BenchmarkResult {
			r := testing.Benchmark(func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					require.NoError(b, baselineWorkload(ctx, repoRoot, target, cfg.iterations, cfg.parallel))
				}
			})
			require.NotZero(t, r.N, "baseline workload failed for %+v", cfg)
			return r
		})

		diag := repeatBenchmark(t, runs, cellLabel+" diagnose", func() testing.BenchmarkResult {
			r := testing.Benchmark(func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					require.NoError(b, diagnoseWorkload(ctx, out, repoRoot, target, cfg.iterations, cfg.parallel))
				}
			})
			require.NotZero(t, r.N, "diagnose workload failed for %+v", cfg)
			return r
		})

		overheadNs := diag.NsPerOp() - base.NsPerOp()
		t.Logf("%s: done — overhead %s (%s of diagnose; baseline %s, diagnose %s)",
			cellLabel, overheadDur(overheadNs), overheadPercent(overheadNs, diag.NsPerOp()),
			roundedDur(base.NsPerOp()), roundedDur(diag.NsPerOp()))

		rows = append(rows, overheadRow{cfg: cfg, baseline: base, diagnose: diag})
	}
	printDiagnoseOverhead(t, label, target, runs, rows)
}

// TestDiagnoseOverhead_Dummy runs the overhead matrix against the tiny dummy package.
// Run via `just bench_overhead_matrix_dummy`. Each cell is benchmarked 5 times by default
// and averaged; set TESTRIG_BENCH_OVERHEAD_RUNS (e.g. 3) to tune accuracy vs wall time.
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

// overheadPercent is overhead as a share of diagnose runtime (overhead / diagnose).
func overheadPercent(overheadNs, diagnoseNs int64) string {
	if diagnoseNs <= 0 {
		return "n/a"
	}
	if overheadNs < 0 {
		overheadNs = 0
	}
	return fmt.Sprintf("%.1f%%", float64(overheadNs)*100/float64(diagnoseNs))
}

// printDiagnoseOverhead logs a table of baseline vs diagnose wall time per config.
// overhead = diagnose ns/op - baseline ns/op; overhead/iter divides by iterations.
// Each cell is the mean of runs repeated benchmark invocations.
func printDiagnoseOverhead(t *testing.T, label, target string, runs int, rows []overheadRow) {
	t.Helper()
	var sb strings.Builder
	tw := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "iters\tparallel\tbaseline\tdiagnose\toverhead\toverhead%\toverhead/iter")
	for _, r := range rows {
		overheadNs := r.diagnose.NsPerOp() - r.baseline.NsPerOp()
		perIterNs := overheadNs / int64(max(r.cfg.iterations, 1))
		diagNs := r.diagnose.NsPerOp()
		_, _ = fmt.Fprintf(tw, "%d\t%d\t%s\t%s\t%s\t%s\t%s\n",
			r.cfg.iterations,
			r.cfg.parallel,
			roundedDur(r.baseline.NsPerOp()),
			roundedDur(diagNs),
			overheadDur(overheadNs),
			overheadPercent(overheadNs, diagNs),
			overheadDur(perIterNs),
		)
	}
	_ = tw.Flush()
	t.Logf(`
--------------------------------------------------------------------------------
Diagnose overhead vs raw go test (%s, target=%s, %d-run average per cell)
--------------------------------------------------------------------------------
%s`,
		label, target, runs, sb.String())
}

func TestAverageBenchmarkResults(t *testing.T) {
	t.Parallel()
	avg := averageBenchmarkResults([]testing.BenchmarkResult{
		{N: 10, T: 1_000, MemBytes: 1_000, MemAllocs: 100},
		{N: 10, T: 3_000, MemBytes: 3_000, MemAllocs: 300},
	})
	require.Equal(t, int64(200), avg.NsPerOp())
	require.Equal(t, int64(200), avg.AllocedBytesPerOp())
	require.Equal(t, int64(20), avg.AllocsPerOp())
}

func TestOverheadPercent(t *testing.T) {
	t.Parallel()
	require.Equal(t, "20.0%", overheadPercent(20, 100))
	require.Equal(t, "0.0%", overheadPercent(-5, 100))
	require.Equal(t, "n/a", overheadPercent(10, 0))
}

func TestOverheadMatrixRuns(t *testing.T) {
	require.Equal(t, 5, overheadMatrixRuns())
	t.Setenv(overheadMatrixRunsEnv, "3")
	require.Equal(t, 3, overheadMatrixRuns())
	t.Setenv(overheadMatrixRunsEnv, "99")
	require.Equal(t, overheadMatrixRunsMax, overheadMatrixRuns())
	t.Setenv(overheadMatrixRunsEnv, "nope")
	require.Equal(t, overheadMatrixRunsDefault, overheadMatrixRuns())
}

func BenchmarkResolveArgs(b *testing.B) {
	repoRoot, err := filepath.Abs("../..")
	require.NoError(b, err)
	args := []string{"./internal/runner/..."}

	for b.Loop() {
		_, _, err := modresolve.ResolveArgs(repoRoot, args)
		require.NoError(b, err)
	}
}
