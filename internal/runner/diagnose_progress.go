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
	completedDurationSum        time.Duration // sum of finished iteration wall times; ETA avg
	active                      map[int]parallelIterationProgress
	poolStartedAt               time.Time
	poolElapsedAtLastCompletion time.Duration // frozen pool wall at last finish (legacy / tests)
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
	if pr, ok := p.active[iteration]; ok {
		p.completedDurationSum += max(time.Since(pr.startedAt), 0).Round(time.Second)
	}
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
) (completed, total int, actives []activeIterElapsed, poolElapsed, completedWall, completedDurationSum time.Duration) {
	if p == nil {
		return 0, 0, nil, 0, 0, 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	completed = p.completed
	total = p.totalIterations
	poolElapsed = max(now.Sub(p.poolStartedAt), 0)
	poolElapsed = poolElapsed.Round(time.Second)
	completedWall = p.poolElapsedAtLastCompletion
	completedDurationSum = p.completedDurationSum
	for iter, pr := range p.active {
		elapsed := max(now.Sub(pr.startedAt), 0)
		actives = append(actives, activeIterElapsed{iteration: iter, elapsed: elapsed.Round(time.Second)})
	}
	slices.SortFunc(actives, func(a, b activeIterElapsed) int {
		return a.iteration - b.iteration
	})
	return completed, total, actives, poolElapsed, completedWall, completedDurationSum
}

// progressBracket wraps inner (already styled) in muted square brackets.
func progressBracket(inner string) string {
	return termstyle.Muted.Render("[") + inner + termstyle.Muted.Render("]")
}

// diagnoseRemainingETA returns gross remaining time at historical average per
// iteration, minus time already spent on in-flight work, floored at zero.
// Used for serial diagnose where exactly one iteration is in flight.
func diagnoseRemainingETA(remaining int, avgPerIter, inFlightElapsed time.Duration) time.Duration {
	if remaining <= 0 || avgPerIter <= 0 {
		return 0
	}
	gross := time.Duration(remaining) * avgPerIter
	return max(gross-inFlightElapsed, 0)
}

// diagnoseParallelRemainingETA estimates wall time left using mean completed
// iteration duration. Not-started slots get a full avg; each in-flight slot
// contributes max(0, avg-elapsed) so long-running workers are not over-subtracted.
func diagnoseParallelRemainingETA(
	total, completed int,
	actives []activeIterElapsed,
	avgPerIter time.Duration,
) time.Duration {
	if avgPerIter <= 0 || total <= completed {
		return 0
	}
	notStarted := max(total-completed-len(actives), 0)
	estimated := time.Duration(notStarted) * avgPerIter
	for _, a := range actives {
		estimated += max(avgPerIter-a.elapsed, 0)
	}
	return estimated
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
			// Freeze completed wall so avgPerIter does not grow each tick (~ETA counts up).
			completedWall := max(runEl-iterElapsed, 0)
			avgPerIter := completedWall / time.Duration(completedCount)
			remainingIters := iterations - completedCount
			estimated := diagnoseRemainingETA(remainingIters, avgPerIter, iterElapsed)
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
	completed, totalIters, actives, poolElapsed, completedWall, completedDurationSum := prog.renderSnapshot(now)
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
		avgPerIter := completedWall / time.Duration(completed) // fallback when no durations recorded yet
		if completedDurationSum > 0 {
			avgPerIter = completedDurationSum / time.Duration(completed)
		} else if completedWall <= 0 {
			avgPerIter = poolElapsed / time.Duration(completed)
		}
		estimated := diagnoseParallelRemainingETA(totalIters, completed, actives, avgPerIter)
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
