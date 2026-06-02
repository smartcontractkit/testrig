package runner

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/output"
)

func TestEllipsizeRight(t *testing.T) {
	t.Parallel()
	require.Equal(t, "short", ellipsizeRight("short", 10))
	require.Equal(t, "abcdefghij", ellipsizeRight("abcdefghij", 10))
	require.Equal(t, "…hij", ellipsizeRight("abcdefghij", 6))
}

func TestRenderDiagnoseProgressLine_smoke(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	t0 := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	runStart := t0.Add(-time.Hour)
	renderDiagnoseProgressLine(&b, 1, 3, 2*time.Second, runStart, t0, true)
	got := b.String()
	require.Contains(t, got, "iter 1/3 (2s)")
	require.Contains(t, got, "1h0m0s")
	require.NotContains(t, got, "·")
	require.NotContains(t, got, "%")
	require.NotContains(t, got, "✅")
	require.NotContains(t, got, "⌛")
	require.NotContains(t, got, "█")
}

func TestRenderDiagnoseProgressLine_noRunWallWhenRunStartZero(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	t0 := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	renderDiagnoseProgressLine(&b, 1, 3, 2*time.Second, time.Time{}, t0, true)
	got := b.String()
	require.Contains(t, got, "iter 1/3 (2s)")
	require.NotContains(t, got, "1h0m0s")
}

func TestRenderDiagnoseProgressLine_notLiveInline(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	t0 := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	renderDiagnoseProgressLine(&b, 1, 3, 2*time.Second, t0, t0, false)
	require.Empty(t, b.String())
}

func TestRenderDiagnoseProgressLine_etaShownWhenCompletionsExist(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	t0 := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	runStart := t0.Add(-60 * time.Second) // 60s elapsed, 2 iterations done
	// iteration=3 (1-based), iterations=5, iterElapsed=10s (shown in bracket only; ETA ignores it)
	// completedCount=2, completedWall=50s, avgPerIter=25s, remainingIters=3, gross=75s, minus 10s in-flight → 65s
	renderDiagnoseProgressLine(&b, 3, 5, 10*time.Second, runStart, t0, true)
	got := b.String()
	require.Contains(t, got, "iter 3/5")
	require.Contains(t, got, "left")
	require.Equal(t, "1m5s", extractDiagnoseETADuration(lineWithoutANSI(got)))
}

func TestDiagnoseRemainingETA_inFlightExceedsAvgDoesNotOverSubtract(t *testing.T) {
	t.Parallel()
	// 3 remaining slots (1 in flight + 2 not started), avg 60s, in-flight already 120s.
	got := diagnoseRemainingETA(3, 60*time.Second, 120*time.Second)
	require.Equal(t, 2*time.Minute, got)
}

func TestRenderDiagnoseProgressLine_etaCountsDownWhileIterationAdvances(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	runStart := t0.Add(-6 * time.Minute)

	var b1, b2 strings.Builder
	renderDiagnoseProgressLine(&b1, 2, 3, 25*time.Second, runStart, t0, true)
	renderDiagnoseProgressLine(&b2, 2, 3, 26*time.Second, runStart, t0.Add(time.Second), true)

	eta1 := extractDiagnoseETADuration(lineWithoutANSI(b1.String()))
	eta2 := extractDiagnoseETADuration(lineWithoutANSI(b2.String()))
	require.NotEmpty(t, eta1)
	require.NotEmpty(t, eta2)
	require.Equal(t, "10m45s", eta1)
	require.Equal(t, "10m44s", eta2, "ETA should tick down as the iteration runs")
}

func lineWithoutANSI(s string) string {
	var b strings.Builder
	inCSI := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inCSI {
			if c >= 0x40 && c <= 0x7e {
				inCSI = false
			}
			continue
		}
		if c == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			inCSI = true
			i++
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func extractDiagnoseETADuration(line string) string {
	_, after, ok := strings.Cut(line, "~")
	if !ok {
		return ""
	}
	rest := after
	before0, _, ok0 := strings.Cut(rest, " left")
	if !ok0 {
		return ""
	}
	return before0
}

func TestRenderDiagnoseProgressLine_noEtaOnFirstIteration(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	t0 := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	runStart := t0.Add(-5 * time.Second)
	renderDiagnoseProgressLine(&b, 1, 5, 5*time.Second, runStart, t0, true)
	got := b.String()
	require.Contains(t, got, "iter 1/5")
	require.NotContains(t, got, "left")
}

func TestRenderParallelDiagnoseProgressLine(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	t0 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := t0.Add(100 * time.Second)
	p := newParallelDiagnoseProgressAt(10, t0)
	p.completed = 1
	p.active[1] = parallelIterationProgress{startedAt: t0.Add(-40 * time.Second)}
	p.active[3] = parallelIterationProgress{startedAt: t0.Add(-10 * time.Second)}

	renderParallelDiagnoseProgressLine(&b, p, now, true)

	got := b.String()
	require.Contains(t, got, "1/10")
	require.NotContains(t, got, "done 1/10")
	require.Contains(t, got, "2(2m20s)")
	require.Contains(t, got, "4(1m50s)")
	require.NotContains(t, got, "active")
	require.NotContains(t, got, "·")
	require.NotContains(t, got, "core/")
}

func TestRenderParallelDiagnoseProgressLine_etaShownWhenCompletionsExist(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	t0 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := t0.Add(60 * time.Second)
	p := newParallelDiagnoseProgressAt(10, t0)
	p.completed = 2
	p.completedDurationSum = 60 * time.Second // avg 30s per iteration, 8 not started → 4m0s
	p.poolElapsedAtLastCompletion = 60 * time.Second
	renderParallelDiagnoseProgressLine(&b, p, now, true)
	got := b.String()
	require.Contains(t, got, "2/10")
	require.Contains(t, got, "left")
	require.Equal(t, "4m0s", extractDiagnoseETADuration(lineWithoutANSI(got)))
}

func TestRenderParallelDiagnoseProgressLine_etaCountsDownWhileIterationAdvances(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	p := newParallelDiagnoseProgressAt(10, t0)
	p.completed = 1
	p.completedDurationSum = 30 * time.Second
	p.poolElapsedAtLastCompletion = 30 * time.Second
	p.active[1] = parallelIterationProgress{startedAt: t0.Add(5 * time.Second)}

	var b1, b2 strings.Builder
	renderParallelDiagnoseProgressLine(&b1, p, t0.Add(20*time.Second), true)
	renderParallelDiagnoseProgressLine(&b2, p, t0.Add(21*time.Second), true)

	eta1 := extractDiagnoseETADuration(lineWithoutANSI(b1.String()))
	eta2 := extractDiagnoseETADuration(lineWithoutANSI(b2.String()))
	require.NotEmpty(t, eta1)
	require.NotEmpty(t, eta2)
	require.Equal(t, "4m15s", eta1) // 8 not started * 30s + (30s - 15s in-flight)
	require.Equal(t, "4m14s", eta2, "ETA should tick down as in-flight iterations run")
}

func TestRenderParallelDiagnoseProgressLine_etaNotCollapsedWithMultipleActives(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	t0 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := t0.Add(60 * time.Second)
	p := newParallelDiagnoseProgressAt(20, t0)
	p.completed = 4
	p.completedDurationSum = 4 * 60 * time.Second // avg 60s per iteration
	p.poolElapsedAtLastCompletion = 60 * time.Second
	for i := range 4 {
		p.active[i] = parallelIterationProgress{startedAt: t0}
	}

	renderParallelDiagnoseProgressLine(&b, p, now, true)
	got := extractDiagnoseETADuration(lineWithoutANSI(b.String()))
	require.Equal(t, "12m0s", got) // 12 not started * 60s; in-flight at avg contribute 0
}

func TestParallelDiagnoseProgress_finishRecordsCompletedWall(t *testing.T) {
	t.Parallel()
	t0 := time.Now().Add(-90 * time.Second)
	p := newParallelDiagnoseProgressAt(3, t0)
	p.active[0] = parallelIterationProgress{startedAt: t0}
	p.finish(0)
	_, _, _, _, completedWall, completedDurationSum := p.renderSnapshot(time.Now())
	require.InDelta(t, 90.0, completedWall.Seconds(), 1.0)
	require.InDelta(t, 90.0, completedDurationSum.Seconds(), 1.0)
}

func TestParallelDiagnoseProgress_completedDurationSumPreservesSubSecond(t *testing.T) {
	t.Parallel()
	t0 := time.Now()
	p := newParallelDiagnoseProgressAt(5, t0)
	p.active[0] = parallelIterationProgress{startedAt: t0.Add(-400 * time.Millisecond)}
	p.finish(0)
	p.active[1] = parallelIterationProgress{startedAt: t0.Add(-400 * time.Millisecond)}
	p.finish(1)
	_, _, _, _, _, sum := p.renderSnapshot(time.Now())
	require.GreaterOrEqual(t, sum, 800*time.Millisecond)
}

func TestRenderParallelDiagnoseProgressLine_noEtaWhenNoneComplete(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	t0 := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := t0.Add(10 * time.Second)
	p := newParallelDiagnoseProgressAt(10, t0)
	renderParallelDiagnoseProgressLine(&b, p, now, true)
	got := b.String()
	require.Contains(t, got, "0/10")
	require.NotContains(t, got, "left")
}

// Simulates: worker goroutine redraws the next iteration's \r line before the
// receiver prints the previous iteration's table row (unbuffered channel
// completion order). Without ClearInline, Fprintln appends to the progress line.
func TestDiagnoseDigestAfterProgressNeedsClear_mergedWithoutClear(t *testing.T) {
	t.Parallel()
	var stderr strings.Builder
	out := output.NewForTest(false, io.Discard, &stderr, true)
	runStart := time.Date(2020, 1, 1, 11, 0, 0, 0, time.UTC)
	now := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	renderDiagnoseProgressLine(out.HumanStderrWriter(), 5, 20, 2*time.Second, runStart, now, true)
	out.HumanStderr("    4  fail")
	got := stderr.String()
	require.Contains(t, got, "iter 5/20")
	require.Contains(t, got, "    4  fail")
	idxRow := strings.Index(got, "    4")
	require.Positive(t, idxRow)
	require.NotContains(t, got[:idxRow], "\n", "digest must not start on a new line (glitch)")
}

func TestDiagnoseDigestAfterProgressNeedsClear_clearedBeforeHumanStderr(t *testing.T) {
	t.Parallel()
	var stderr strings.Builder
	out := output.NewForTest(false, io.Discard, &stderr, true)
	runStart := time.Date(2020, 1, 1, 11, 0, 0, 0, time.UTC)
	now := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	renderDiagnoseProgressLine(out.HumanStderrWriter(), 5, 20, 2*time.Second, runStart, now, true)
	out.ClearInline()
	out.HumanStderr("    4  fail")
	got := stderr.String()
	require.Contains(t, got, "\r\u001b[K    4  fail\n")
}
