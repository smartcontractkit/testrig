package runner

import (
	"fmt"
	"sync"
	"time"

	"github.com/smartcontractkit/testrig/internal/output"
	"github.com/smartcontractkit/testrig/internal/termstyle"
)

const buildProgressTick = 250 * time.Millisecond

// buildStepOutcome tells a build step's finish function how the step ended.
// Failed steps leave a ❌ line; stopped steps (Ctrl+C) leave a "stopped" line.
type buildStepOutcome uint8

const (
	buildStepOK buildStepOutcome = iota
	buildStepFailed
	buildStepStopped
)

// buildProgress writes the compile and link steps with the same look as
// iteration progress: one live redraw line on a TTY, or a muted "started" line
// without one. Callers print the step's final line after finish returns.
type buildProgress struct {
	out *output.Printer
	// now and tick are seams for deterministic golden tests.
	now  func() time.Time
	tick time.Duration
}

func newBuildProgress(out *output.Printer) *buildProgress {
	return &buildProgress{out: out, now: time.Now, tick: buildProgressTick}
}

// begin emits the build_start token once the run is eligible for a build.
func (p *buildProgress) begin() {
	if p == nil || p.out == nil || !p.out.AIOutput() {
		return
	}
	p.out.Stderrf("build_start\n")
}

// start begins one build step. verb is the live label (for example
// "compiling"), startLine the non-TTY notice. count, when non-nil, returns the
// written and total binary count for a "[n/total]" bracket. The returned
// function stops the live redraw. A successful step clears the line so the
// caller can print the finished line; a failed or stopped step leaves a final
// line with a ❌ or "stopped" marker.
func (p *buildProgress) start(verb, startLine string, count func() (int, int)) func(buildStepOutcome) {
	if p == nil || p.out == nil || p.out.AIOutput() {
		return func(buildStepOutcome) {}
	}

	start := p.now()
	elapsed := func() time.Duration { return max(p.now().Sub(start), 0) }

	if !p.out.LiveInlineProgress() {
		p.out.HumanStderr(termstyle.Muted.Render(startLine))
		return func(outcome buildStepOutcome) {
			if outcome == buildStepOK {
				return
			}
			p.out.HumanStderr(buildStepOutcomeLine(verb, elapsed(), count, outcome))
		}
	}

	draw := func() {
		p.out.RedrawInline(buildStepLiveLine(verb, elapsed(), count))
	}
	draw()

	done := make(chan struct{})
	var once sync.Once
	var wg sync.WaitGroup
	if p.tick > 0 {
		wg.Go(func() {
			ticker := time.NewTicker(p.tick)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
					draw()
				}
			}
		})
	}
	return func(outcome buildStepOutcome) {
		once.Do(func() {
			close(done)
			wg.Wait()
			p.out.ClearInline()
			if outcome != buildStepOK {
				p.out.HumanStderr(buildStepOutcomeLine(verb, elapsed(), count, outcome))
			}
		})
	}
}

// buildStepLiveLine renders one live step: a Label verb, an optional Label
// binary count, and the Muted elapsed time.
func buildStepLiveLine(verb string, elapsed time.Duration, count func() (int, int)) string {
	line := termstyle.Label.Render(verb)
	if count != nil {
		if written, total := count(); total > 0 {
			line += " " + progressBracket(termstyle.Label.Render(fmt.Sprintf("%d/%d", written, total)))
		}
	}
	return line + " " + progressBracket(termstyle.Muted.Render(elapsed.Round(time.Second).String()))
}

// buildStepOutcomeLine is the final line for a failed or stopped step, such as
// "linking [12/175] [9s] ❌" or "linking [12/175] [9s] stopped".
func buildStepOutcomeLine(
	verb string,
	elapsed time.Duration,
	count func() (int, int),
	outcome buildStepOutcome,
) string {
	line := buildStepLiveLine(verb, elapsed, count)
	switch outcome {
	case buildStepFailed:
		return line + " " + termstyle.Bad.Render("❌")
	case buildStepStopped:
		return line + " " + termstyle.Accent.Render("stopped")
	default:
		return line
	}
}
