package runner

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/smartcontractkit/testrig/internal/output"
	"github.com/smartcontractkit/testrig/internal/termstyle"
)

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
		p.completedDurationSum += max(time.Since(pr.startedAt), 0)
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

// diagnoseRemainingETA estimates wall time left for serial diagnose where
// exactly one iteration is in flight. Remaining slots include that iteration;
// not-started slots get a full avg and the in-flight slot contributes
// max(0, avg-inFlightElapsed).
func diagnoseRemainingETA(remaining int, avgPerIter, inFlightElapsed time.Duration) time.Duration {
	if remaining <= 0 || avgPerIter <= 0 {
		return 0
	}
	notStarted := max(remaining-1, 0)
	return time.Duration(notStarted)*avgPerIter + max(avgPerIter-inFlightElapsed, 0)
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

func buildDiagnoseProgressLineParts(
	iteration, iterations int,
	iterElapsed time.Duration,
	diagnoseRunStart time.Time,
	now time.Time,
) (core string, optional []string) {
	iterBracket := fmt.Sprintf("iter %d/%d (%s)", iteration, iterations, iterElapsed.Round(time.Second).String())
	core = progressBracket(termstyle.Label.Render(iterBracket))
	if diagnoseRunStart.IsZero() {
		return core, nil
	}
	runEl := max(now.Sub(diagnoseRunStart), 0)
	optional = append(optional, "  "+progressBracket(termstyle.Muted.Render(runEl.Round(time.Second).String())))
	completedCount := iteration - 1
	if completedCount > 0 {
		// Freeze completed wall so avgPerIter does not grow each tick (~ETA counts up).
		completedWall := max(runEl-iterElapsed, 0)
		avgPerIter := completedWall / time.Duration(completedCount)
		remainingIters := iterations - completedCount
		estimated := diagnoseRemainingETA(remainingIters, avgPerIter, iterElapsed)
		if estimated > 0 {
			optional = append(optional, formatETA(estimated))
		}
	}
	return core, optional
}

// fitProgressLineParts joins core with optional segments that fit cols.
// When truncate is true, hard-truncates the result as a last resort.
func fitProgressLineParts(core string, optional []string, cols int, truncate bool) string {
	if cols < 1 {
		cols = 80
	}
	line := core
	for _, part := range optional {
		if part == "" {
			continue
		}
		next := line + part
		if ansi.StringWidth(next) <= cols {
			line = next
		}
	}
	if truncate && ansi.StringWidth(line) > cols {
		line = ansi.Truncate(line, cols, "…")
	}
	return line
}

func fitDiagnoseProgressLine(
	iteration, iterations int,
	iterElapsed time.Duration,
	diagnoseRunStart time.Time,
	now time.Time,
	cols int,
) string {
	core, optional := buildDiagnoseProgressLineParts(iteration, iterations, iterElapsed, diagnoseRunStart, now)
	return fitProgressLineParts(core, optional, cols, true)
}

func parallelActivesPart(actives []activeIterElapsed, maxShown int) string {
	if len(actives) == 0 {
		return ""
	}
	shown := actives
	extra := 0
	if maxShown >= 0 && len(actives) > maxShown {
		shown = actives[:maxShown]
		extra = len(actives) - maxShown
	}
	var sb strings.Builder
	for i, a := range shown {
		if i > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(termstyle.Label.Render(fmt.Sprintf("%d(%s)", a.iteration+1, a.elapsed.String())))
	}
	if extra > 0 {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(termstyle.Muted.Render("+" + strconv.Itoa(extra)))
	}
	return "  " + progressBracket(sb.String())
}

func fitParallelDiagnoseProgressLine(
	core string,
	actives []activeIterElapsed,
	poolPart, etaPart string,
	cols int,
) string {
	if cols < 1 {
		cols = 80
	}
	var best string
	bestWidth := -1
	for maxShown := len(actives); maxShown >= 0; maxShown-- {
		var optional []string
		if part := parallelActivesPart(actives, maxShown); part != "" {
			optional = append(optional, part)
		}
		if poolPart != "" {
			optional = append(optional, poolPart)
		}
		if etaPart != "" {
			optional = append(optional, etaPart)
		}
		line := fitProgressLineParts(core, optional, cols, false)
		w := ansi.StringWidth(line)
		if w <= cols && w > bestWidth {
			best = line
			bestWidth = w
		}
	}
	if best != "" {
		return ansi.Truncate(best, cols, "…")
	}
	return ansi.Truncate(core, cols, "…")
}

// renderDiagnoseProgressLine writes one status line when live inline progress is active.
// diagnoseRunStart is when the overall diagnose run began; if zero, only the
// per-iteration bracket is shown.
func renderDiagnoseProgressLine(
	out *output.Printer,
	iteration, iterations int,
	iterElapsed time.Duration,
	diagnoseRunStart time.Time,
	now time.Time,
) {
	if out == nil || !out.LiveInlineProgress() {
		return
	}
	line := fitDiagnoseProgressLine(iteration, iterations, iterElapsed, diagnoseRunStart, now, out.TermColumns())
	out.RedrawInline(line)
}

func renderParallelDiagnoseProgressLine(out *output.Printer, prog *parallelDiagnoseProgress, now time.Time) {
	if out == nil || !out.LiveInlineProgress() || prog == nil {
		return
	}
	completed, totalIters, actives, poolElapsed, completedWall, completedDurationSum := prog.renderSnapshot(now)
	core := progressBracket(termstyle.Label.Render(fmt.Sprintf("%d/%d", completed, totalIters)))
	poolPart := "  " + progressBracket(termstyle.Muted.Render(poolElapsed.String()))
	var etaPart string
	if completed > 0 && totalIters > completed {
		avgPerIter := completedWall / time.Duration(completed) // fallback when no durations recorded yet
		if completedDurationSum > 0 {
			avgPerIter = completedDurationSum / time.Duration(completed)
		} else if completedWall <= 0 {
			avgPerIter = poolElapsed / time.Duration(completed)
		}
		estimated := diagnoseParallelRemainingETA(totalIters, completed, actives, avgPerIter)
		if estimated > 0 {
			etaPart = formatETA(estimated)
		}
	}
	line := fitParallelDiagnoseProgressLine(core, actives, poolPart, etaPart, out.TermColumns())
	out.RedrawInline(line)
}

func formatETA(estimated time.Duration) string {
	return "  " + progressBracket(termstyle.Muted.Render("~"+estimated.Round(time.Second).String()+" left"))
}
