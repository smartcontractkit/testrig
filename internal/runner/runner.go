// Package runner implements go test wrappers, iteration orchestration, and result analysis.
package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/hooks"
	"github.com/smartcontractkit/testrig/internal/output"
	"github.com/smartcontractkit/testrig/internal/termstyle"
	"github.com/smartcontractkit/testrig/modresolve"
)

// failFastReasonBuildFailure is used when go test reports a compile/build failure
// (FailedBuild or package-level fail with no named tests run); diagnose stops immediately.
const failFastReasonBuildFailure = "build-failure"

// diagnoseIterationParams is per-iteration state for diagnose (see diagnoseIterationRunner;
// context is passed separately to avoid storing context in a struct).
type diagnoseIterationParams struct {
	Conf             *config.App
	Out              *output.Printer
	ResultsDir       string
	GoTestArgs       []string
	ModuleDir        string
	Iteration        int
	ShuffleSeed      int64
	Env              []string
	LiveProgress     bool
	ParallelProgress *parallelDiagnoseProgress
	DiagnoseRunStart time.Time
	SerialProgressMu *sync.Mutex
}

type diagnoseIterationRunner func(ctx context.Context, p diagnoseIterationParams) error

type diagnoseRunHooks struct {
	runIteration      diagnoseIterationRunner
	seed              func() int64
	iterationSetup    func(context.Context) error
	iterationTeardown func(context.Context) error
}

// DiagnoseRunState captures the runtime progress and limits of a diagnose session.
type DiagnoseRunState struct {
	completed           int
	failedFast          bool
	failedFastReason    string
	failedFastIteration int // 0-based diagnose iteration index; -1 if unset
	GracefulStop        bool
	iterDurations       []time.Duration
	shuffleSeeds        map[int]int64
	liveProgress        bool
}

func runCommand(ctx context.Context, conf *config.App, binary string, args []string, env []string) error {
	dir, args, err := modresolve.ResolveArgs(conf.RepoRoot, args)
	if err != nil {
		return err
	}
	//nolint:gosec // G204: user-controlled args by design
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = append(os.Environ(), env...)
	return cmd.Run()
}

// GoTest runs `go test` with the given args (repo root as working directory).
// env is appended to the child process environment (e.g. resource Env).
func GoTest(ctx context.Context, conf *config.App, args []string, env []string) error {
	return runCommand(ctx, conf, "go", append([]string{"test"}, args...), env)
}

// Gotestsum runs `gotestsum` with the given args (repo root as working directory).
// env is appended to the child process environment (e.g. resource Env).
func Gotestsum(ctx context.Context, conf *config.App, args []string, env []string) error {
	if _, err := exec.LookPath("gotestsum"); err != nil {
		return fmt.Errorf("gotestsum not on PATH: install with go install gotest.tools/gotestsum@latest: %w", err)
	}
	return runCommand(ctx, conf, "gotestsum", args, env)
}

// RunIterations runs go test -json once per iteration, writing each stream to
// iteration-<n>.log.jsonl under a new results directory and run-state.json on return.
// Test iteration failures do not stop later runs (unless --fail-fast); compile/build
// failures stop immediately. Returns a non-nil error for setup failures (e.g. mkdir,
// database reset) or ctx errors from dependencies — not for failing tests alone.
// iterSetup and iterTeardown run before/after each iteration. Either may be nil.
// Teardown runs even when the iteration's go test invocation fails; its error is
// reported only when the iteration itself succeeded.
// Call FinishDiagnoseAnalysis separately to analyze results and write report.json.
func RunIterations(
	ctx context.Context,
	conf *config.App,
	out *output.Printer,
	goTestArgs []string,
	resources []hooks.Resource,
	iterSetup, iterTeardown func(context.Context) error,
) (*DiagnoseRunState, time.Time, string, error) {
	if out == nil {
		out = output.NewFromApp(conf)
	}
	start := time.Now()

	resultsDir, err := makeDiagnoseResultsDir(conf, goTestArgs, start)
	if err != nil {
		return nil, start, "", err
	}
	printDiagnoseResultsDirHeader(out, resultsDir)
	if err := printDiagnoseRunTimeEstimate(out, conf, goTestArgs, 0); err != nil {
		return nil, start, resultsDir, err
	}

	iterHooks := diagnoseRunHooks{
		iterationSetup:    iterSetup,
		iterationTeardown: iterTeardown,
	}
	state, runErr := runDiagnoseIterations(
		ctx,
		conf,
		out,
		resultsDir,
		goTestArgs,
		toIterationResources(resources),
		iterHooks,
	)
	if runErr != nil {
		if ctx.Err() == nil {
			return nil, start, resultsDir, runErr
		}
	}

	if state.failedFast && state.failedFastReason == failFastReasonBuildFailure && state.failedFastIteration >= 0 {
		if !out.AIOutput() {
			printBuildError(out, resultsDir, state.failedFastIteration)
		} else {
			out.Stderrf("bf_stop iter=%d pkgs=\n", state.failedFastIteration+1)
		}
		// When build fails fast, we stop completely and don't analyze
		_ = writeRunState(resultsDir, conf, goTestArgs, state, start)
		return state, start, resultsDir, nil
	}

	_ = writeRunState(resultsDir, conf, goTestArgs, state, start)
	return state, start, resultsDir, runErr
}

// RunStateSnapshot is saved to disk so the diagnose analysis phase can be run
// separately from the execution phase.
type RunStateSnapshot struct {
	Conf       *config.App       `json:"conf"`
	GoTestArgs []string          `json:"go_test_args"`
	State      *DiagnoseRunState `json:"state"`
	Start      time.Time         `json:"start"`
}

func writeRunState(
	resultsDir string,
	conf *config.App,
	goTestArgs []string,
	state *DiagnoseRunState,
	start time.Time,
) error {
	snap := RunStateSnapshot{
		Conf:       conf,
		GoTestArgs: goTestArgs,
		State:      state,
		Start:      start,
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(resultsDir, "run-state.json"), b, 0600)
}

// ReadRunState loads the snapshot from a diagnose results directory.
func ReadRunState(resultsDir string) (*RunStateSnapshot, error) {
	b, err := os.ReadFile(filepath.Join(resultsDir, "run-state.json")) //nolint:gosec // G304: path from filepath.Join
	if err != nil {
		return nil, err
	}
	var snap RunStateSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// FinishDiagnoseAnalysis completes the analysis phase of a diagnose run.
// It is intended to be called after RunIterations and after resources have been cleaned up.
func FinishDiagnoseAnalysis(
	ctx context.Context,
	conf *config.App,
	out *output.Printer,
	goTestArgs []string,
	state *DiagnoseRunState,
	start time.Time,
	resultsDir string,
) error {
	if state.failedFast && state.failedFastReason == failFastReasonBuildFailure && state.failedFastIteration >= 0 {
		return nil
	}

	printDiagnoseInterruptions(ctx, conf, out, state)

	stopAnalyzing := startDiagnoseAnalyzingProgress(out, state.liveProgress)
	var report *Report
	var logs LogMap
	var analyzeErr error
	report, logs, analyzeErr = AnalyzeResults(resultsDir, conf.SlowThreshold)
	stopAnalyzing(analyzeErr)
	if analyzeErr != nil {
		out.Stderrf("analyze results: %v\n", analyzeErr)
		return analyzeErr
	}

	if report != nil {
		for i, d := range state.iterDurations {
			if i >= len(report.IterationSummaries) {
				break
			}
			report.IterationSummaries[i].Duration = d
			if state.shuffleSeeds != nil {
				report.IterationSummaries[i].ShuffleSeed = state.shuffleSeeds[i]
			}
		}
		finished := time.Now()
		report.Run = newRunMeta(conf, goTestArgs, resultsDir, start, &finished)
		fillIterationRuntimeSummary(report)
	}

	if err := writeDiagnoseArtifacts(out, resultsDir, report, logs); err != nil {
		return err
	}

	return finalizeDiagnoseOutput(ctx, conf, out, report, resultsDir, start)
}

func printDiagnoseInterruptions(ctx context.Context, conf *config.App, out *output.Printer, state *DiagnoseRunState) {
	if state.GracefulStop {
		if !out.AIOutput() {
			out.HumanStderr(
				termstyle.Accent.Render(
					fmt.Sprintf("stopped early after %d/%d iterations", state.completed, conf.Iterations),
				) +
					termstyle.Muted.Render(
						" — analyzing partial results…",
					),
			)
		}
	} else if interrupted := ctx.Err() != nil; interrupted {
		if out.AIOutput() {
			out.Stderrf("interrupted completed=%d total=%d\n", state.completed, conf.Iterations)
		} else {
			out.HumanStderr(
				termstyle.Accent.Render(
					fmt.Sprintf("interrupted after %d/%d iterations", state.completed, conf.Iterations),
				) +
					termstyle.Muted.Render(
						" — analyzing partial results…",
					),
			)
		}
	}

	if state.failedFast && !out.AIOutput() {
		if state.failedFastReason == failFastReasonBuildFailure && state.failedFastIteration >= 0 {
			iter := state.failedFastIteration + 1
			out.HumanStderr(termstyle.Bad.Render(
				fmt.Sprintf("Build failed — stopping diagnose run (iteration %d/%d).", iter, conf.Iterations)))
		} else {
			msg := "--fail-fast set, stopping early"
			if state.failedFastReason != "" {
				msg = fmt.Sprintf("fail-fast matched %s, stopping early", state.failedFastReason)
			}
			out.HumanStderr(termstyle.Accent.Render(msg))
		}
	}
}

func writeDiagnoseArtifacts(out *output.Printer, resultsDir string, report *Report, logs LogMap) error {
	if err := WriteLogFiles(resultsDir, report, logs); err != nil {
		out.Stderrf("write log files: %v\n", err)
		return err
	}
	if err := WriteReport(resultsDir, report); err != nil {
		out.Stderrf("write report: %v\n", err)
		return err
	}
	if err := WriteCSV(resultsDir, report); err != nil {
		out.Stderrf("write csv: %v\n", err)
		return err
	}
	return nil
}

func finalizeDiagnoseOutput(
	ctx context.Context,
	conf *config.App,
	out *output.Printer,
	report *Report,
	resultsDir string,
	start time.Time,
) error {
	reportPath := filepath.Join(resultsDir, "report.json")
	csvPath := diagnoseCSVPath(resultsDir, report)
	traceJSONPath := ""
	var traceFiles []string
	if conf.Trace {
		var err error
		traceFiles, err = WriteTrace(out, resultsDir)
		if err != nil {
			out.Stderrf("write trace: %v\n", err)
			return err
		}
		traceJSONPath = filepath.Join(resultsDir, "trace.json")
	}
	if out.AIOutput() {
		completeJSON, err := marshalAIDiagnoseComplete(resultsDir, reportPath, traceJSONPath, report)
		if err != nil {
			out.Stderrf("marshal ai complete: %v\n", err)
			return err
		}
		out.SparseStdoutln(string(completeJSON))
		return nil
	}

	out.HumanStderr(
		termstyle.Label.Render("Diagnosis Complete!") + " " +
			termstyle.Muted.Render("["+formatDiagnoseWallClock(time.Since(start))+"]"))
	if report != nil {
		PrintSummary(out.HumanStderrWriter(), report)
	}
	printDiagnoseArtifactsFooter(out, resultsDir, reportPath, csvPath, traceJSONPath)
	if conf.Trace && traceViewerEnabled() {
		if err := ServeTrace(ctx, resultsDir, traceFiles, out, TraceServeOptions{}); err != nil {
			out.Stderrf("serve trace: %v\n", err)
			return err
		}
	}
	return nil
}

// fillIterationRuntimeSummary sets summary iteration_duration_* from each
// iteration's wall-clock Duration (min / max / p50 across the run).
func fillIterationRuntimeSummary(rep *Report) {
	if rep == nil || rep.Summary == nil {
		return
	}
	var samples []time.Duration
	for _, s := range rep.IterationSummaries {
		if s.Duration > 0 {
			samples = append(samples, s.Duration)
		}
	}
	if len(samples) == 0 {
		return
	}
	minD, maxD, p50 := sortedDurationStats(samples)
	rep.Summary.IterationDurationMin = minD
	rep.Summary.IterationDurationMax = maxD
	rep.Summary.IterationDurationP50 = p50
}

// EffectiveParallelIterations returns the bounded diagnose worker count.
func EffectiveParallelIterations(conf *config.App) int {
	if conf == nil {
		return 1
	}
	parallel := max(conf.ParallelIterations, 1)
	if conf.Iterations > 0 && parallel > conf.Iterations {
		parallel = conf.Iterations
	}
	return parallel
}

func makeDiagnoseResultsDir(conf *config.App, goTestArgs []string, now time.Time) (string, error) {
	base := filepath.Join(conf.RepoRoot, diagnoseResultsDirName(goTestArgs, now))
	for i := 0; ; i++ {
		dir := base
		if i > 0 {
			dir = fmt.Sprintf("%s-%d", base, i)
		}
		err := os.Mkdir(dir, 0700)
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
}

type diagnoseIterationResult struct {
	iteration  int
	duration   time.Duration
	shuffle    int64
	iterErr    error
	fatalErr   error
	dumpErr    error
	failedFast bool
	failReason string
	digest     IterationDigest
	digestErr  error
}

func setupDiagnoseLiveProgress(
	conf *config.App,
	out *output.Printer,
	parallel int,
	state *DiagnoseRunState,
) (parallelProgress *parallelDiagnoseProgress, diagnoseRunStart time.Time, progressTickDone chan struct{}, progressTickWG *sync.WaitGroup) {
	progressTickDone = make(chan struct{})
	progressTickWG = &sync.WaitGroup{}
	if out.AIOutput() || !out.LiveInlineProgress() {
		return nil, time.Time{}, progressTickDone, progressTickWG
	}
	state.liveProgress = true
	if parallel <= 1 {
		return nil, time.Now(), progressTickDone, progressTickWG
	}
	parallelProgress = newParallelDiagnoseProgress(conf.Iterations)
	progressTickWG.Go(func() {
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-progressTickDone:
				return
			case <-tick.C:
				parallelProgress.withRenderLock(func() {
					renderParallelDiagnoseProgressLine(out, parallelProgress, time.Now())
				})
			}
		}
	})
	return parallelProgress, time.Time{}, progressTickDone, progressTickWG
}

func recordDiagnoseIterationResult(
	state *DiagnoseRunState,
	result diagnoseIterationResult,
	conf *config.App,
	out *output.Printer,
	parallelProgress *parallelDiagnoseProgress,
	serialProgressMu *sync.Mutex,
) {
	state.completed++
	state.iterDurations[result.iteration] = result.duration
	if state.shuffleSeeds != nil {
		state.shuffleSeeds[result.iteration] = result.shuffle
	}
	diagnoseWithRenderLock(parallelProgress, serialProgressMu, func() {
		if parallelProgress != nil || serialProgressMu != nil {
			out.ClearInline()
		}
		printDiagnoseIterationDigest(
			out,
			result.iteration,
			conf.Iterations,
			result.digest,
			result.digestErr,
			result.duration,
		)
	})
	if result.dumpErr != nil && !out.AIOutput() {
		out.Stderrf("diagnostics dump iteration %d: %v\n", result.iteration, result.dumpErr)
	}
	if !result.failedFast {
		return
	}
	state.failedFast = true
	if state.failedFastReason == "" {
		state.failedFastReason = result.failReason
	}
	if state.failedFastIteration < 0 {
		state.failedFastIteration = result.iteration
	}
}

func runDiagnoseIterations(
	ctx context.Context,
	conf *config.App,
	out *output.Printer,
	resultsDir string,
	goTestArgs []string,
	resources []diagnoseIterationResource,
	hooks diagnoseRunHooks,
) (*DiagnoseRunState, error) {
	if hooks.runIteration == nil {
		hooks.runIteration = diagnoseIteration
	}

	moduleDir, adjustedArgs, err := modresolve.ResolveArgs(conf.RepoRoot, goTestArgs)
	if err != nil {
		return nil, err
	}

	if hooks.seed == nil {
		hooks.seed = func() int64 { return rand.Int64N(1<<62) + 1 } //nolint:gosec // G404: non-crypto seed for test shuffle
	}
	parallel := EffectiveParallelIterations(conf)
	if len(resources) == 0 {
		resources = make([]diagnoseIterationResource, parallel)
	}
	if len(resources) < parallel {
		parallel = len(resources)
	}
	resources = resources[:parallel]
	state := &DiagnoseRunState{
		iterDurations:       make([]time.Duration, conf.Iterations),
		failedFastIteration: -1,
	}
	if conf.Shuffle {
		state.shuffleSeeds = make(map[int]int64)
	}

	if !out.AIOutput() {
		printDiagnoseIterationTableHeader(out)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan int)
	results := make(chan diagnoseIterationResult)
	parallelProgress, diagnoseRunStart, progressTickDone, progressTickWG := setupDiagnoseLiveProgress(
		conf,
		out,
		parallel,
		state,
	)
	defer func() {
		close(progressTickDone)
		progressTickWG.Wait()
	}()

	var serialProgressMu *sync.Mutex
	if parallel == 1 && state.liveProgress {
		serialProgressMu = new(sync.Mutex)
	}

	var inFlight atomic.Int32
	worker := diagnoseWorker{
		conf:             conf,
		out:              out,
		resultsDir:       resultsDir,
		goTestArgs:       adjustedArgs,
		moduleDir:        moduleDir,
		hooks:            hooks,
		parallel:         parallel,
		parallelProgress: parallelProgress,
		diagnoseRunStart: diagnoseRunStart,
		serialProgressMu: serialProgressMu,
		jobs:             jobs,
		results:          results,
		cancel:           cancel,
		inFlight:         &inFlight,
	}
	var wg sync.WaitGroup
	for _, resource := range resources {
		wg.Go(func() {
			worker.run(runCtx, resource)
		})
	}

	wg.Go(func() {
		defer close(jobs)
		gracefulStopCh := DiagnoseGracefulStopChan(ctx)
		for i := range conf.Iterations {
			if DiagnoseGracefulStopRequested(ctx) {
				return
			}
			if gracefulStopCh == nil {
				select {
				case <-runCtx.Done():
					return
				case jobs <- i:
				}
				continue
			}
			select {
			case <-runCtx.Done():
				return
			case <-gracefulStopCh:
				return
			case jobs <- i:
			}
		}
	})

	go func() {
		wg.Wait()
		close(results)
	}()

	var firstErr error
	var gracefulStopNotice sync.Once
	maybePrintGracefulStopNotice := func() {
		if !DiagnoseGracefulStopRequested(ctx) || inFlight.Load() > 0 {
			return
		}
		gracefulStopNotice.Do(func() {
			state.GracefulStop = true
			printDiagnoseGracefulStopNotice(out, state.completed, conf.Iterations)
		})
	}

	for result := range results {
		if result.fatalErr != nil {
			if firstErr == nil {
				firstErr = result.fatalErr
				cancel()
			}
			continue
		}
		recordDiagnoseIterationResult(state, result, conf, out, parallelProgress, serialProgressMu)
		maybePrintGracefulStopNotice()
	}
	maybePrintGracefulStopNotice()
	return state, firstErr
}

// diagnoseWorker is shared, immutable state across diagnose iteration workers.
// One run() goroutine is launched per resource; the worker pulls iterations
// from jobs until the channel closes or runCtx is cancelled.
type diagnoseWorker struct {
	conf             *config.App
	out              *output.Printer
	resultsDir       string
	goTestArgs       []string
	moduleDir        string
	hooks            diagnoseRunHooks
	parallel         int
	parallelProgress *parallelDiagnoseProgress
	diagnoseRunStart time.Time
	serialProgressMu *sync.Mutex
	jobs             <-chan int
	results          chan<- diagnoseIterationResult
	cancel           context.CancelFunc
	inFlight         *atomic.Int32
}

func (w *diagnoseWorker) run(runCtx context.Context, resource diagnoseIterationResource) {
	used := false
	for iteration := range w.jobs {
		done := w.runIterationJob(runCtx, resource, iteration, &used)
		if done {
			return
		}
	}
}

func (w *diagnoseWorker) runIterationJob(
	runCtx context.Context,
	resource diagnoseIterationResource,
	iteration int,
	used *bool,
) bool {
	defer w.trackInFlight()()
	if runCtx.Err() != nil {
		return true
	}
	if *used && resource.Reset != nil {
		if err := resource.Reset(runCtx); err != nil {
			if runCtx.Err() != nil {
				return true
			}
			w.sendFatal(runCtx, iteration, fmt.Errorf("reset database before iteration %d: %w", iteration, err))
			return true
		}
	}
	*used = true
	if w.hooks.iterationSetup != nil {
		if err := w.hooks.iterationSetup(runCtx); err != nil {
			w.sendFatal(runCtx, iteration, fmt.Errorf("iteration setup %d: %w", iteration, err))
			return true
		}
	}
	var seed int64
	if w.conf.Shuffle {
		seed = w.hooks.seed()
	}
	iterStart := time.Now()
	iterErr := w.hooks.runIteration(runCtx, diagnoseIterationParams{
		Conf:             w.conf,
		Out:              w.out,
		ResultsDir:       w.resultsDir,
		GoTestArgs:       w.goTestArgs,
		ModuleDir:        w.moduleDir,
		Iteration:        iteration,
		ShuffleSeed:      seed,
		Env:              resource.Env,
		LiveProgress:     w.parallel == 1,
		ParallelProgress: w.parallelProgress,
		DiagnoseRunStart: w.diagnoseRunStart,
		SerialProgressMu: w.serialProgressMu,
	})
	iterDur := time.Since(iterStart)
	if w.hooks.iterationTeardown != nil {
		if tdErr := w.hooks.iterationTeardown(runCtx); tdErr != nil && iterErr == nil {
			iterErr = fmt.Errorf("iteration teardown %d: %w", iteration, tdErr)
		}
	}
	var dumpErr error
	if resource.DumpDiagnostics != nil {
		dumpErr = resource.DumpDiagnostics(runCtx, w.resultsDir, iteration)
	}
	digest, digestErr := loadIterationDigest(w.resultsDir, iteration, w.conf.SlowThreshold)
	failedFast, failReason := shouldFailFastIteration(w.conf, iterErr, digest, digestErr)
	failedFast = failedFast && runCtx.Err() == nil
	if failedFast {
		w.cancel()
	}
	result := diagnoseIterationResult{
		iteration:  iteration,
		duration:   iterDur,
		shuffle:    seed,
		iterErr:    iterErr,
		dumpErr:    dumpErr,
		failedFast: failedFast,
		failReason: failReason,
		digest:     digest,
		digestErr:  digestErr,
	}
	select {
	case w.results <- result:
	case <-runCtx.Done():
		if failedFast {
			w.results <- result
		}
		return true
	}
	return false
}

func (w *diagnoseWorker) trackInFlight() func() {
	if w.inFlight == nil {
		return func() {}
	}
	w.inFlight.Add(1)
	return func() { w.inFlight.Add(-1) }
}

// sendFatal posts a fatal iteration result and cancels the run. The select
// guards against ctx cancellation racing the send.
func (w *diagnoseWorker) sendFatal(runCtx context.Context, iteration int, err error) {
	select {
	case w.results <- diagnoseIterationResult{iteration: iteration, fatalErr: err}:
	case <-runCtx.Done():
	}
	w.cancel()
}

// loadIterationDigest opens iteration-<n>.log.jsonl and computes the digest.
// Returns an empty digest and the error when the file can't be opened or parsed.
func loadIterationDigest(resultsDir string, iteration int, slow time.Duration) (IterationDigest, error) {
	jsonPath := filepath.Join(resultsDir, fmt.Sprintf("iteration-%d.log.jsonl", iteration))
	f, err := os.Open(jsonPath) //nolint:gosec // G304: path from filepath.Join
	if err != nil {
		return IterationDigest{}, err
	}
	defer func() { _ = f.Close() }()
	return DigestIterationJSONL(f, slow)
}

// shouldFailFastIteration decides whether the diagnose run should stop after
// this iteration. The caller has already loaded the iteration's digest (or the
// error from loading it).
func shouldFailFastIteration(conf *config.App, iterErr error, d IterationDigest, digestErr error) (bool, string) {
	if conf == nil {
		return false, ""
	}
	if iterErr == nil && !conf.FailFast && len(conf.FailFastOn) == 0 {
		return false, ""
	}
	if digestErr != nil {
		if iterErr != nil && conf.FailFast {
			return true, "failure"
		}
		return false, ""
	}
	if d.BuildFailure {
		return true, failFastReasonBuildFailure
	}
	if iterErr != nil && conf.FailFast {
		return true, "failure"
	}
	if len(conf.FailFastOn) == 0 {
		return false, ""
	}
	return failFastDigestMatch(d, conf.FailFastOn)
}

func failFastDigestMatch(d IterationDigest, categories []string) (bool, string) {
	for _, category := range categories {
		switch strings.ToLower(strings.TrimSpace(category)) {
		case config.FailFastOnAny:
			if d.Result == "fail" || d.Result == "timeout" || d.SlowTests > 0 {
				return true, config.FailFastOnAny
			}
		case config.FailFastOnFailure:
			if d.Result == "fail" && d.FailTests > 0 {
				return true, config.FailFastOnFailure
			}
		case config.FailFastOnTimeout:
			if d.Result == "timeout" || d.TimeoutTests > 0 {
				return true, config.FailFastOnTimeout
			}
		case config.FailFastOnSlow:
			if d.SlowTests > 0 {
				return true, config.FailFastOnSlow
			}
		}
	}
	return false, ""
}

// findLastFlagValue returns the last value of -flag (forms "-flag=val" and
// "-flag val") within the go test flag section (before -args). set is false
// when the flag does not appear. An error is returned only for "-flag" with no
// following token.
func findLastFlagValue(goTestArgs []string, flag string) (raw string, set bool, err error) {
	args := modresolve.GoTestFlagsBeforeArgs(goTestArgs)
	prefix := flag + "="
	for i := 0; i < len(args); i++ {
		a := args[i]
		if after, ok := strings.CutPrefix(a, prefix); ok {
			raw = strings.TrimSpace(after)
			set = true
			continue
		}
		if a == flag {
			if i+1 >= len(args) {
				return "", false, fmt.Errorf("invalid go test arguments: %s must be followed by a value", flag)
			}
			i++
			raw = strings.TrimSpace(args[i])
			set = true
		}
	}
	return raw, set, nil
}

// parseDiagnoseGoTestCount returns the last -count in the portion of argv that
// belongs to `go test` itself (before -args). If no -count appears, set is false.
func parseDiagnoseGoTestCount(goTestArgs []string) (set bool, n int, err error) {
	raw, set, err := findLastFlagValue(goTestArgs, "-count")
	if err != nil || !set {
		return false, 0, err
	}
	num, e := strconv.Atoi(raw)
	if e != nil {
		return false, 0, fmt.Errorf("invalid -count value %q: %w", raw, e)
	}
	if num < 1 {
		return false, 0, fmt.Errorf("invalid go test arguments: -count must be a positive integer, got %d", num)
	}
	return true, num, nil
}

// defaultGoTestTimeout matches `go help testflag` when -timeout is omitted.
const defaultGoTestTimeout = 10 * time.Minute

// parseGoTestTimeout returns the last -timeout in the go test flag section (before -args).
// If the value parses to 0, disabled is true (timeout disabled for the test binary).
// If no -timeout appears, set is false and callers should assume defaultGoTestTimeout for estimates.
func parseGoTestTimeout(goTestArgs []string) (set bool, d time.Duration, disabled bool, err error) {
	raw, set, err := findLastFlagValue(goTestArgs, "-timeout")
	if err != nil || !set {
		return false, 0, false, err
	}
	if raw == "" {
		return false, 0, false, errors.New("invalid go test arguments: -timeout= requires a duration")
	}
	dur, e := time.ParseDuration(raw)
	if e != nil {
		return false, 0, false, fmt.Errorf("invalid -timeout value %q: %w", raw, e)
	}
	if dur == 0 {
		return true, 0, true, nil
	}
	return true, dur, false, nil
}

// diagnoseIterationWaves returns ceil(iterations/workers) for scheduling diagnose iterations.
func diagnoseIterationWaves(iterations, workers int) int {
	w := max(workers, 1)
	if iterations < 1 {
		return 0
	}
	return (iterations + w - 1) / w
}

// diagnoseWallUpperBoundDetails holds the inputs used for the human-facing estimate line.
type diagnoseWallUpperBoundDetails struct {
	Bound       time.Duration
	Workers     int
	Waves       int
	PerInv      time.Duration
	UsedDefault bool // true when -timeout was omitted (10m assumed)
}

// diagnoseWallUpperBound returns worst-case wall clock if each go test invocation runs
// for the full per-invocation test timeout (Go default 10m when -timeout is unset).
// ok is false when -timeout=0 disables the test binary timeout (no finite bound).
func diagnoseWallUpperBound(
	conf *config.App,
	goTestArgs []string,
	resourceCount int,
) (diag diagnoseWallUpperBoundDetails, ok bool, err error) {
	if conf == nil {
		return diagnoseWallUpperBoundDetails{}, false, errors.New("config is nil")
	}
	set, d, disabled, err := parseGoTestTimeout(goTestArgs)
	if err != nil {
		return diagnoseWallUpperBoundDetails{}, false, err
	}
	if set && disabled {
		return diagnoseWallUpperBoundDetails{}, false, nil
	}
	perInv := defaultGoTestTimeout
	usedDefault := !set
	if set && !disabled {
		perInv = d
		usedDefault = false
	}
	parallel := EffectiveParallelIterations(conf)
	if resourceCount > 0 && resourceCount < parallel {
		parallel = resourceCount
	}
	if parallel < 1 {
		parallel = 1
	}
	waves := diagnoseIterationWaves(conf.Iterations, parallel)
	bound := time.Duration(waves) * perInv
	return diagnoseWallUpperBoundDetails{
		Bound:       bound,
		Workers:     parallel,
		Waves:       waves,
		PerInv:      perInv,
		UsedDefault: usedDefault,
	}, true, nil
}

func printDiagnoseRunTimeEstimate(out *output.Printer, conf *config.App, goTestArgs []string, resourceCount int) error {
	if out == nil || conf == nil {
		return nil
	}
	diag, ok, err := diagnoseWallUpperBound(conf, goTestArgs, resourceCount)
	if err != nil {
		return err
	}
	if out.AIOutput() {
		if !ok {
			out.Stderrf("lpr_s:inf\n")
			return nil
		}
		sec := max(diag.Bound.Round(time.Second)/time.Second, 0)
		out.Stderrf("lpr_s:%d\n", sec)
		return nil
	}
	return nil
}

// WarnDiagnoseGoTestCount returns an error if the user sets -count on go test.
func WarnDiagnoseGoTestCount(goTestArgs []string) error {
	set, _, err := parseDiagnoseGoTestCount(goTestArgs)
	if err != nil {
		return err
	}
	if set {
		return fmt.Errorf(
			"manual -count flag detected; prefer diagnose --iterations for repetition. -count is not allowed in go test flags",
		)
	}
	return nil
}

// WarnDiagnoseGoTestTrace returns an error if the user sets -trace on go test.
func WarnDiagnoseGoTestTrace(goTestArgs []string) error {
	_, set, err := findLastFlagValue(goTestArgs, "-trace")
	if err != nil {
		return err
	}
	if set {
		return fmt.Errorf(
			"manual -trace flag detected; use diagnose --trace instead. -trace is not allowed in go test flags",
		)
	}
	return nil
}

// filterDiagnoseUserGoTestArgs removes -json/--json from the go test flag
// section so the harness can inject -json; arguments after -args are unchanged.
func filterDiagnoseUserGoTestArgs(args []string) []string {
	split := len(args)
	for i, a := range args {
		if a == "-args" {
			split = i
			break
		}
	}
	prefix := args[:split]
	suffix := args[split:]
	var out []string
	for i := range prefix {
		a := prefix[i]
		if a == "-json" || a == "--json" {
			continue
		}
		out = append(out, a)
	}
	return append(out, suffix...)
}

// buildDiagnoseArgs constructs the `go test` argv for a single diagnose iteration.
func buildDiagnoseArgs(goTestArgs []string, shuffleSeed int64) ([]string, error) {
	filtered := filterDiagnoseUserGoTestArgs(goTestArgs)
	set, n, err := parseDiagnoseGoTestCount(goTestArgs)
	if err != nil {
		return nil, err
	}
	args := []string{"test", "-json"}
	args = append(args, filtered...)
	if shuffleSeed != 0 {
		args = append(args, fmt.Sprintf("-shuffle=%d", shuffleSeed))
	}
	if !set || n <= 1 {
		args = append(args, "-count=1")
	}
	return args, nil
}

func printBuildError(out *output.Printer, resultsDir string, iteration int) {
	sep := termstyle.Bad.Render("════════════════════════════════════════")
	_, _ = fmt.Fprintln(out.HumanStderrWriter(), sep)
	_, _ = fmt.Fprintln(out.HumanStderrWriter(), termstyle.Bad.Render("BUILD ERROR"))
	_, _ = fmt.Fprintln(out.HumanStderrWriter(), sep)
	jsonPath := filepath.Join(resultsDir, fmt.Sprintf("iteration-%d.log.jsonl", iteration))
	buildOut, err := extractBuildOutput(jsonPath)
	if err == nil && buildOut != "" {
		_, _ = fmt.Fprint(out.HumanStderrWriter(), buildOut)
	}
}

// extractBuildOutput reads a go-test-json JSONL log and returns all compiler output.
// JSON lines: Output fields are concatenated. Non-JSON lines are included verbatim.
func extractBuildOutput(path string) (string, error) {
	f, err := os.Open(path) //nolint:gosec // G304: path from filepath.Join
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	var sb strings.Builder
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || line[0] != '{' {
			sb.Write(line)
			sb.WriteByte('\n')
			continue
		}
		var ev TestEvent
		if jsonErr := json.Unmarshal(line, &ev); jsonErr != nil {
			sb.Write(line)
			sb.WriteByte('\n')
			continue
		}
		if ev.Output != "" {
			sb.WriteString(ev.Output)
		}
	}
	return sb.String(), scanner.Err()
}

func printDiagnoseResultsDirHeader(out *output.Printer, resultsDir string) {
	if out.AIOutput() {
		out.Stdoutln(resultsDir)
	}
}

// formatDiagnoseWallClock formats total wall time for human diagnose footers (0.1s resolution, seconds show two decimals when fractional).
func formatDiagnoseWallClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(100 * time.Millisecond)
	if d == 0 {
		return "0s"
	}
	h := d / time.Hour
	d -= h * time.Hour
	mins := d / time.Minute
	d -= mins * time.Minute
	s := float64(d) / float64(time.Second)
	secStr := formatDiagnoseSecondsFragment(s)
	if h > 0 {
		return fmt.Sprintf("%dh%dm%s", h, mins, secStr)
	}
	if mins > 0 {
		return fmt.Sprintf("%dm%s", mins, secStr)
	}
	return secStr
}

func formatDiagnoseSecondsFragment(s float64) string {
	cs := max(int(math.Round(s*100)), 0)
	w := cs / 100
	f := cs % 100
	if f == 0 {
		return fmt.Sprintf("%ds", w)
	}
	return fmt.Sprintf("%d.%02ds", w, f)
}

// startDiagnoseAnalyzingProgress prints a live "analyzing [duration]" line on stderr and
// returns stop. Call stop when analysis finishes: on success it clears the progress line
// so the caller can print "diagnosis complete"; on failure it leaves an analyzing ❌ line.
func startDiagnoseAnalyzingProgress(out *output.Printer, afterLiveProgress bool) (stop func(error)) {
	if out.AIOutput() {
		return func(error) {}
	}
	if afterLiveProgress {
		if out.LiveInlineProgress() {
			out.ClearInline()
			_, _ = fmt.Fprint(out.HumanStderrWriter(), "\n")
		} else {
			_, _ = fmt.Fprint(out.HumanStderrWriter(), "\r\033[2K\n")
		}
	}

	analyzeStart := time.Now()
	var once sync.Once
	finalize := func(live bool, err error) {
		if err == nil {
			if live {
				out.ClearInline()
			}
			return
		}
		elapsed := max(time.Since(analyzeStart).Round(time.Second), 0)
		line := termstyle.Label.Render("analyzing") + " " +
			termstyle.Muted.Render("["+elapsed.String()+"]") + " " +
			termstyle.Bad.Render("❌")
		if live {
			out.ClearInline()
			out.HumanStderr(line)
			return
		}
		out.HumanStderr(line)
	}

	if !out.LiveInlineProgress() {
		return func(err error) {
			once.Do(func() { finalize(false, err) })
		}
	}

	renderProgress := func() {
		elapsed := max(time.Since(analyzeStart).Round(time.Second), 0)
		line := termstyle.Label.Render("analyzing") + " " + termstyle.Muted.Render("["+elapsed.String()+"]")
		out.RedrawInline(line)
	}
	renderProgress()

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Go(func() {
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				renderProgress()
			}
		}
	})

	return func(err error) {
		once.Do(func() {
			close(done)
			wg.Wait()
			finalize(true, err)
		})
	}
}

func printDiagnoseIterationDigest(
	out *output.Printer,
	iterationIdx0, totalIters int,
	d IterationDigest,
	digestErr error,
	iterDur time.Duration,
) {
	if digestErr != nil {
		out.Stderrf("diagnose iteration %d summary: %v\n", iterationIdx0+1, digestErr)
		return
	}
	iter := iterationIdx0 + 1
	if out.AIOutput() {
		out.Stdoutln(formatIterationDigestAI(iter, totalIters, d, iterDur))
		return
	}
	printIterationDigestHuman(out, iter, d, iterDur)
}

// formatIterationDigestAI prints one line for --ai-output diagnose progress.
// Tokens: d iter/total; p|f|t result; wall seconds; r named tests executed (excludes skip-only);
// k skipped; f failing-test entries; t timeouts; s slow tests.
func formatIterationDigestAI(iter, total int, d IterationDigest, dur time.Duration) string {
	rs := "?"
	switch d.Result {
	case "pass":
		rs = "p"
	case "fail":
		rs = "f"
	case "timeout":
		rs = "t"
	}
	sec := max(int(dur.Round(time.Second)/time.Second), 0)
	return fmt.Sprintf(
		"d %d/%d %s %ds r%d k%d f%d t%d s%d",
		iter,
		total,
		rs,
		sec,
		d.RanTests,
		d.SkipTests,
		d.FailTests,
		d.TimeoutTests,
		d.SlowTests,
	)
}

func printIterationDigestHuman(out *output.Printer, iter int, d IterationDigest, dur time.Duration) {
	row := formatDiagnoseIterationTableRow(iter, d, dur)
	switch d.Result {
	case "fail":
		row += formatDiagnoseProblemTestsSuffix(d.FailingTests, out.TermColumns(), row, termstyle.Bad.Render)
	case "timeout":
		row += formatDiagnoseProblemTestsSuffix(d.TimedOutTests, out.TermColumns(), row, termstyle.Accent.Render)
	}
	out.HumanStderr(row)
}

func renderIterationResultHuman(r string) string {
	switch r {
	case "pass":
		return termstyle.OK.Render("pass")
	case "fail":
		return termstyle.Bad.Render("fail")
	case "timeout":
		return termstyle.Accent.Render("timeout")
	default:
		return termstyle.Muted.Render(r)
	}
}

// syncedWriter serializes writes to w so stdout and stderr from `go test` can
// share one JSONL file without interleaved corrupt lines.
type syncedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (sw *syncedWriter) Write(p []byte) (int, error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	return sw.w.Write(p)
}

func diagnoseIteration(ctx context.Context, p diagnoseIterationParams) error {
	conf, out := p.Conf, p.Out
	resultsDir := p.ResultsDir
	iteration, shuffleSeed := p.Iteration, p.ShuffleSeed
	env := p.Env
	liveProgress, parallelProgress := p.LiveProgress, p.ParallelProgress
	diagnoseRunStart, serialProgressMu := p.DiagnoseRunStart, p.SerialProgressMu
	moduleDir, goTestArgs := p.ModuleDir, p.GoTestArgs

	start := time.Now()
	jsonPath := filepath.Join(resultsDir, fmt.Sprintf("iteration-%d.log.jsonl", iteration))
	resultsFile, err := os.Create(jsonPath) //nolint:gosec // G304: path from filepath.Join
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(resultsFile, 128*1024)
	var retErr error
	defer func() {
		if err := bw.Flush(); err != nil && retErr == nil {
			retErr = err
		}
		if err := resultsFile.Close(); err != nil && retErr == nil {
			retErr = err
		}
	}()

	args, err := buildDiagnoseArgs(goTestArgs, shuffleSeed)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "go", args...) //nolint:gosec // G204: user-controlled go test args by design
	cmd.Dir = moduleDir
	cmd.Stdin = os.Stdin
	cmd.Env = append(os.Environ(), env...)
	// Soft-cancel on ctx cancellation so `go test -json` gets a chance to flush
	// its final events before we escalate to SIGKILL after WaitDelay.
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 5 * time.Second

	sw := &syncedWriter{w: bw}
	cmd.Stdout = sw
	cmd.Stderr = sw

	if out.AIOutput() {
		retErr = cmd.Run()
		return retErr
	}

	if parallelProgress != nil {
		parallelProgress.start(iteration)
		defer parallelProgress.finish(iteration)
	}

	live := liveProgress && out.LiveInlineProgress()
	iter, iters := iteration+1, conf.Iterations
	if liveProgress && !live {
		out.HumanStderr(termstyle.Muted.Render(fmt.Sprintf("iteration %d/%d started", iter, iters)))
	}

	redraw := func() {
		if serialProgressMu != nil {
			serialProgressMu.Lock()
			defer serialProgressMu.Unlock()
		}
		renderDiagnoseProgressLine(
			out,
			iter,
			iters,
			time.Since(start),
			diagnoseRunStart,
			time.Now(),
		)
	}

	tickDone := make(chan struct{})
	var tickWG sync.WaitGroup
	if live {
		tickWG.Go(func() {
			tick := time.NewTicker(250 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-tickDone:
					return
				case <-tick.C:
					redraw()
				}
			}
		})
		redraw()
	}

	retErr = cmd.Run()
	close(tickDone)
	tickWG.Wait()

	if live {
		out.ClearInline()
	}
	return retErr
}

func newRunMeta(
	conf *config.App,
	goTestArgs []string,
	resultsDir string,
	started time.Time,
	finished *time.Time,
) *RunMeta {
	if conf == nil {
		return nil
	}
	target := guessPackagePatternForSlug(goTestArgs)
	slug := diagnoseTargetSlug(target)
	args := append([]string(nil), goTestArgs...)
	slow := conf.SlowThreshold
	if slow == 0 {
		slow = 30 * time.Second
	}
	var ffo []string
	if n, err := config.NormalizeFailFastOn(conf.FailFastOn); err == nil && len(n) > 0 {
		ffo = n
	}
	par := max(conf.ParallelIterations, 1)
	var fin *time.Time
	if finished != nil {
		t := finished.UTC()
		fin = &t
	}
	return &RunMeta{
		ResultsDirBasename: filepath.Base(resultsDir),
		StartedAt:          started.UTC(),
		FinishedAt:         fin,
		GoTestArgs:         args,
		TargetSlug:         slug,
		DiagnoseIterations: conf.Iterations,
		ParallelIterations: par,
		SlowThreshold:      slow,
		FailFast:           conf.FailFast,
		FailFastOn:         ffo,
		Shuffle:            conf.Shuffle,
	}
}
