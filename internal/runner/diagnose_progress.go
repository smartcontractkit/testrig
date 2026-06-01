package runner

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/smartcontractkit/testrig/internal/termstyle"
)

// testBinaryTwoArgSuffixFlags are test-binary flags that consume the following argv token.
// When scanning backwards from the end, a token immediately after one of these is skipped
// so package patterns can appear before `-run TestName` (valid `go test` ordering).
var testBinaryTwoArgSuffixFlags = map[string]bool{
	"-run":   true,
	"-bench": true,
	"-skip":  true,
	"-fuzz":  true,
}

func singleArgTestBinaryFlagPrefix(arg string) (prefix string, ok bool) {
	for _, p := range []string{"-run=", "-bench=", "-skip=", "-fuzz="} {
		if strings.HasPrefix(arg, p) {
			return p, true
		}
	}
	return "", false
}

func looksLikeGoPackagePattern(arg string) bool {
	return strings.Contains(arg, ".") ||
		strings.Contains(arg, "/") ||
		strings.Contains(arg, "...")
}

// packagePatternsFromEnd returns trailing arguments that look like package patterns.
// It scans backward from the end of goTestFlagsBeforeArgs(args), skipping `-run`,
// `-bench`, `-skip`, and `-fuzz` and their values so `./pkg -run TestName` still
// yields `./pkg`. This matches the usual `go test [flags] [packages]` layout and
// also package-first ordering with test flags after packages.
func packagePatternsFromEnd(args []string) []string {
	args = goTestFlagsBeforeArgs(args)
	var pkgs []string
	for i := len(args) - 1; i >= 0; i-- {
		arg := args[i]
		if _, ok := singleArgTestBinaryFlagPrefix(arg); ok {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			break
		}
		if i >= 1 && testBinaryTwoArgSuffixFlags[args[i-1]] {
			i--
			continue
		}
		if !looksLikeGoPackagePattern(arg) {
			break
		}
		pkgs = append(pkgs, arg)
	}
	slices.Reverse(pkgs)
	return pkgs
}

type parallelDiagnoseProgress struct {
	mu                          sync.Mutex
	renderMu                    sync.Mutex
	totalIterations             int
	completed                   int
	active                      map[int]parallelIterationProgress
	poolStartedAt               time.Time
	poolElapsedAtLastCompletion time.Duration // frozen wall at last finish; ETA only
}

type parallelIterationProgress struct {
	startedAt time.Time
}

type activeIterElapsed struct {
	iteration int           // 0-based diagnose iteration index
	elapsed   time.Duration // wall since this iteration's go test started
}

func newParallelDiagnoseProgress(totalIterations int) *parallelDiagnoseProgress {
	return newParallelDiagnoseProgressAt(totalIterations, time.Now())
}

func newParallelDiagnoseProgressAt(totalIterations int, poolStartedAt time.Time) *parallelDiagnoseProgress {
	return &parallelDiagnoseProgress{
		totalIterations: totalIterations,
		active:          make(map[int]parallelIterationProgress),
		poolStartedAt:   poolStartedAt,
	}
}

func (p *parallelDiagnoseProgress) start(iteration int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active[iteration] = parallelIterationProgress{startedAt: time.Now()}
}

func (p *parallelDiagnoseProgress) finish(iteration int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.active, iteration)
	p.completed++
	p.poolElapsedAtLastCompletion = max(time.Since(p.poolStartedAt), 0).Round(time.Second)
}

func (p *parallelDiagnoseProgress) withRenderLock(fn func()) {
	if p == nil {
		fn()
		return
	}
	p.renderMu.Lock()
	defer p.renderMu.Unlock()
	fn()
}

// diagnoseWithRenderLock serializes inline progress/digest output for parallel or serial diagnose runs.
func diagnoseWithRenderLock(parallelProgress *parallelDiagnoseProgress, serialProgressMu *sync.Mutex, fn func()) {
	switch {
	case parallelProgress != nil:
		parallelProgress.withRenderLock(fn)
	case serialProgressMu != nil:
		serialProgressMu.Lock()
		fn()
		serialProgressMu.Unlock()
	default:
		fn()
	}
}

// renderSnapshot returns completed iteration count, total planned iterations,
// per-active-iteration elapsed (sorted by iteration index), wall time since the
// parallel pool began, and wall time recorded at the last completion (for ETA).
func (p *parallelDiagnoseProgress) renderSnapshot(
	now time.Time,
) (completed, total int, actives []activeIterElapsed, poolElapsed, completedWall time.Duration) {
	if p == nil {
		return 0, 0, nil, 0, 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	completed = p.completed
	total = p.totalIterations
	poolElapsed = max(now.Sub(p.poolStartedAt), 0)
	poolElapsed = poolElapsed.Round(time.Second)
	completedWall = p.poolElapsedAtLastCompletion
	for iter, pr := range p.active {
		elapsed := max(now.Sub(pr.startedAt), 0)
		actives = append(actives, activeIterElapsed{iteration: iter, elapsed: elapsed.Round(time.Second)})
	}
	slices.SortFunc(actives, func(a, b activeIterElapsed) int {
		return a.iteration - b.iteration
	})
	return completed, total, actives, poolElapsed, completedWall
}

// progressBracket wraps inner (already styled) in muted square brackets.
func progressBracket(inner string) string {
	return termstyle.Muted.Render("[") + inner + termstyle.Muted.Render("]")
}

// renderDiagnoseProgressLine writes one status line to w when liveInline is true
// (TTY stderr in human mode). Otherwise it is a no-op so logs are not spammed.
// diagnoseRunStart is when the overall diagnose run began; if zero, only the
// per-iteration bracket is shown.
func renderDiagnoseProgressLine(
	w io.Writer,
	iteration, iterations int,
	iterElapsed time.Duration,
	diagnoseRunStart time.Time,
	now time.Time,
	liveInline bool,
) {
	if !liveInline {
		return
	}
	iterBracket := fmt.Sprintf("iter %d/%d (%s)", iteration, iterations, iterElapsed.Round(time.Second).String())
	line := progressBracket(termstyle.Label.Render(iterBracket))
	if !diagnoseRunStart.IsZero() {
		runEl := max(now.Sub(diagnoseRunStart), 0)
		line += "  " + progressBracket(termstyle.Muted.Render(runEl.Round(time.Second).String()))
		completedCount := iteration - 1
		if completedCount > 0 {
			// runEl includes the current iteration; counting it in the average makes
			// avgPerIter grow every tick during an in-flight iteration (~ETA counts up).
			completedWall := max(runEl-iterElapsed, 0)
			avgPerIter := completedWall / time.Duration(completedCount)
			remainingIters := iterations - completedCount
			estimated := time.Duration(remainingIters) * avgPerIter
			if estimated > 0 {
				line += formatETA(estimated)
			}
		}
	}
	_, _ = fmt.Fprint(w, "\r\033[K")
	_, _ = fmt.Fprint(w, line)
}

func renderParallelDiagnoseProgressLine(w io.Writer, prog *parallelDiagnoseProgress, now time.Time, liveInline bool) {
	if !liveInline || prog == nil {
		return
	}
	completed, totalIters, actives, poolElapsed, completedWall := prog.renderSnapshot(now)
	line := progressBracket(termstyle.Label.Render(fmt.Sprintf("%d/%d", completed, totalIters)))
	if len(actives) > 0 {
		var sb strings.Builder
		for i, a := range actives {
			if i > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(termstyle.Label.Render(fmt.Sprintf("%d(%s)", a.iteration+1, a.elapsed.String())))
		}
		line += "  " + progressBracket(sb.String())
	}
	line += "  " + progressBracket(termstyle.Muted.Render(poolElapsed.String()))
	if completed > 0 && totalIters > completed {
		// completedWall is frozen at the last finish so in-flight pool growth
		// does not inflate avgPerIter each tick (~ETA counts up), mirroring serial.
		wallForETA := completedWall
		if wallForETA <= 0 {
			wallForETA = poolElapsed
		}
		remaining := totalIters - completed
		estimated := time.Duration(remaining) * wallForETA / time.Duration(completed)
		if estimated > 0 {
			line += formatETA(estimated)
		}
	}
	_, _ = fmt.Fprint(w, "\r\033[K")
	_, _ = fmt.Fprint(w, line)
}

func formatETA(estimated time.Duration) string {
	return "  " + progressBracket(termstyle.Muted.Render("~"+estimated.Round(time.Second).String()+" left"))
}

func ellipsizeRight(s string, maxLen int) string {
	if maxLen <= 3 || len(s) <= maxLen {
		return s
	}
	return "…" + s[len(s)-(maxLen-3):]
}
