package runner

import (
	"bufio"
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/buger/jsonparser"

	"github.com/smartcontractkit/testrig/internal/termstyle"
)

const maxDiagnoseLogFilenameBytes = 240

// timeoutPanic appears in go test -json output when the test binary's
// -timeout fires. It may be attached to a running test or to the package.
const timeoutPanic = "panic: test timed out"

// TestEvent represents a parsed test event from the JSONL log stream.
type TestEvent struct {
	Time        time.Time
	Action      string
	Package     string
	Test        string
	Elapsed     float64
	Output      string `json:"Output"`
	OutputBytes []byte `json:"-"`
	FailedBuild string
}

// stringInterner deduplicates identical strings during JSON log parsing.
// Test logs often contain millions of events with highly repetitive fields
// like Package and Test names. By interning these byte slices into shared string
// references, we significantly reduce memory allocations and prevent OOM errors
// when analyzing very large test suites.
type stringInterner struct {
	m map[string]string
}

func newStringInterner() *stringInterner {
	return &stringInterner{m: make(map[string]string)}
}

func (i *stringInterner) intern(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	s, ok := i.m[string(b)]
	if ok {
		return s
	}
	s = string(b)
	i.m[s] = s
	return s
}

// iterationScanMeta collects signals during scan that are not represented in aggregates only.
type iterationScanMeta struct {
	sawFailedBuild bool
}

type testKey struct {
	Package string
	Test    string
}

type aggregate struct {
	passes       int
	fails        int
	skips        int
	maxElapsed   time.Duration
	timedOut     bool
	lastRunIter  int
	runs         int
	failedIters  []int
	timeoutIters []int
	skipIters    []int
	slowIters    []int
	outputs      map[int]*bytes.Buffer
	logPaths     map[int]string
	elapseds     []time.Duration
}

// ProblemLog points to log files for iterations where this entry actually had
// the reported problem. Path uses "{iter}" as the iteration placeholder.
type ProblemLog struct {
	Type  string `json:"type"`
	Iters string `json:"iters"`
	Path  string `json:"path"`
}

// TestEntry is a single row in the analysis report.
type TestEntry struct {
	Package       string        `json:"package"`
	Test          string        `json:"test,omitempty"`
	Runs          int           `json:"runs"`
	Successes     int           `json:"successes"`
	Fails         int           `json:"fails"`
	Skips         int           `json:"skips"`
	Timeouts      int           `json:"timeouts"`
	FailRateLower *float64      `json:"fail_rate_lower,omitempty"`
	FailRateUpper *float64      `json:"fail_rate_upper,omitempty"`
	MinElapsed    time.Duration `json:"min_elapsed"`
	MaxElapsed    time.Duration `json:"max_elapsed"`
	P50Elapsed    time.Duration `json:"p50_elapsed"`
	Logs          []ProblemLog  `json:"logs,omitempty"`
	FailIters     []int         `json:"-"`
	TimeoutIters  []int         `json:"-"`
	SlowIters     []int         `json:"-"`
}

// IterationSummary captures high-level stats for a single diagnose iteration.
// Duration and ShuffleSeed are populated by the runner after analysis.
type IterationSummary struct {
	Index        int           `json:"index"`
	Duration     time.Duration `json:"duration,omitempty"`
	Result       string        `json:"result"` // "pass", "fail", "timeout"
	FailingTests []string      `json:"failing_tests,omitempty"`
	ShuffleSeed  int64         `json:"shuffle_seed,omitempty"`
}

// RunMeta records how the diagnose harness was invoked and where output lives.
// Use this for full argv and flags; the directory name only carries a short target slug and timestamp.
type RunMeta struct {
	ResultsDirBasename string        `json:"results_dir_basename"`
	StartedAt          time.Time     `json:"started_at"`
	FinishedAt         *time.Time    `json:"finished_at,omitempty"`
	GoTestArgs         []string      `json:"go_test_args"`
	TargetSlug         string        `json:"target_slug"`
	DiagnoseIterations int           `json:"diagnose_iterations"`
	ParallelIterations int           `json:"parallel_iterations,omitempty"`
	SlowThreshold      time.Duration `json:"slow_threshold"`
	FailFast           bool          `json:"fail_fast,omitempty"`
	FailFastOn         []string      `json:"fail_fast_on,omitempty"`
	Shuffle            bool          `json:"shuffle,omitempty"`
}

// ReportSummary holds aggregate flake and slow rates for the full diagnose run.
// FlakePrevalence uses distinct named tests (package.test keys). Per-execution
// flake_fail_runs / flake_total_runs sum across flaky entries; flake_failing_iterations
// / flake_iteration_total count diagnose iterations (union of flake failures vs rep.Iterations).
type ReportSummary struct {
	DistinctNamedTests          int      `json:"distinct_named_tests"`
	FlakeNamedCount             int      `json:"flake_named_count"`
	FlakePrevalence             *float64 `json:"flake_prevalence,omitempty"`
	FlakeFailRuns               int      `json:"flake_fail_runs,omitempty"`
	FlakeTotalRuns              int      `json:"flake_total_runs,omitempty"`
	FlakeExecutionFailRate      *float64 `json:"flake_execution_fail_rate,omitempty"`
	FlakeExecutionFailRateLower *float64 `json:"flake_execution_fail_rate_lower,omitempty"`
	FlakeExecutionFailRateUpper *float64 `json:"flake_execution_fail_rate_upper,omitempty"`
	// FlakeFailingIterations is how many diagnose iterations had at least one
	// flake failure; FlakeIterationTotal is rep.Iterations (not summed per-test runs).
	FlakeFailingIterations      int      `json:"flake_failing_iterations,omitempty"`
	FlakeIterationTotal         int      `json:"flake_iteration_total,omitempty"`
	FlakeIterationFailRate      *float64 `json:"flake_iteration_fail_rate,omitempty"`
	FlakeIterationFailRateLower *float64 `json:"flake_iteration_fail_rate_lower,omitempty"`
	FlakeIterationFailRateUpper *float64 `json:"flake_iteration_fail_rate_upper,omitempty"`
	SlowCount                   int      `json:"slow_count,omitempty"`
	SlowPrevalence              *float64 `json:"slow_prevalence,omitempty"`
	// IterationDurationMin/Max/P50 summarize wall-clock runtimes (IterationSummary.Duration) across all completed iterations.
	IterationDurationMin time.Duration `json:"iteration_duration_min,omitempty"`
	IterationDurationMax time.Duration `json:"iteration_duration_max,omitempty"`
	IterationDurationP50 time.Duration `json:"iteration_duration_p50,omitempty"`
}

// Report classifies tests across iterations of a diagnose run.
type Report struct {
	Run                *RunMeta           `json:"run,omitempty"`
	Iterations         int                `json:"iterations"`
	SlowThreshold      time.Duration      `json:"slow_threshold"`
	Summary            *ReportSummary     `json:"summary,omitempty"`
	IterationSummaries []IterationSummary `json:"iteration_summaries,omitempty"`
	Flakes             []TestEntry        `json:"flakes,omitempty"`
	Failures           []TestEntry        `json:"failures,omitempty"`
	Timeouts           []TestEntry        `json:"timeouts,omitempty"`
	Slow               []TestEntry        `json:"slow,omitempty"`
	SlowestPackages    []TestEntry        `json:"slowest_packages,omitempty"`
}

// TestGroup defines a category of flagged tests within a Report,
// exposing references for mutation and metadata for presentation.
type TestGroup struct {
	Entries     *[]TestEntry
	LogKind     string
	CSVCategory string
	Iters       func(TestEntry) []int
}

// TestGroups returns all flagged test categories in precedence order
// (Timeout > Failure > Flake > Slow) for deduplication and output generation.
func (rep *Report) TestGroups() []TestGroup {
	if rep == nil {
		return nil
	}
	return []TestGroup{
		{
			Entries:     &rep.Timeouts,
			LogKind:     "timeout",
			CSVCategory: "timeout",
			Iters:       func(e TestEntry) []int { return e.TimeoutIters },
		},
		{
			Entries:     &rep.Failures,
			LogKind:     "fail",
			CSVCategory: "failure",
			Iters:       func(e TestEntry) []int { return e.FailIters },
		},
		{
			Entries:     &rep.Flakes,
			LogKind:     "fail",
			CSVCategory: "flake",
			Iters:       func(e TestEntry) []int { return e.FailIters },
		},
		{
			Entries:     &rep.Slow,
			LogKind:     "slow",
			CSVCategory: "slow",
			Iters:       func(e TestEntry) []int { return e.SlowIters },
		},
	}
}

// LogMap maps (package,test) → iteration → raw interleaved output.
// Returned alongside Report so callers can write per-test log files without
// coupling the parser to the filesystem.
type LogMap map[testKey]map[int]string

var readerPool = sync.Pool{
	New: func() any {
		return bufio.NewReaderSize(nil, 1024*1024)
	},
}

// Analyze reads per-iteration test2json streams and classifies tests.
// Malformed lines are silently skipped (go test can interleave non-JSON).
func Analyze(iterations []io.Reader, slowThreshold time.Duration) (*Report, LogMap, func(), error) {
	tmpDir, err := os.MkdirTemp("", "testrig-analyze-*")
	if err != nil {
		return nil, nil, nil, err
	}
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	aggs := make(map[testKey]*aggregate)
	interner := newStringInterner()
	for i, r := range iterations {
		if err := scanIterationJSONL(r, i, aggs, nil, slowThreshold, interner, tmpDir); err != nil {
			return nil, nil, cleanup, err
		}
		if err := reattributeTimeoutsIter(aggs, i, tmpDir); err != nil {
			return nil, nil, cleanup, err
		}
		if err := flushOutputsToDisk(i, aggs, tmpDir); err != nil {
			return nil, nil, cleanup, err
		}
	}
	rep, logs := buildReportFromAggs(aggs, len(iterations), slowThreshold)
	return rep, logs, cleanup, nil
}

func newAggregate() *aggregate {
	return &aggregate{
		lastRunIter: -1,
	}
}

func (a *aggregate) recordElapsed(d time.Duration) {
	a.elapseds = append(a.elapseds, d)
	if d > a.maxElapsed {
		a.maxElapsed = d
	}
}

// parseTestEvent parses a single JSONL test event into ev. If interner is not nil,
// it deduplicates Action, Package, and Test strings.
func parseTestEvent(line []byte, ev *TestEvent, interner *stringInterner, parseTime bool, customBuf []byte) error {
	return jsonparser.ObjectEach(
		line,
		func(key []byte, value []byte, dataType jsonparser.ValueType, _ int) error {
			switch string(key) {
			case "Time":
				if parseTime && dataType == jsonparser.String {
					s, _ := jsonparser.ParseString(value)
					ev.Time, _ = time.Parse(time.RFC3339Nano, s)
				}
			case "Action":
				if interner != nil {
					ev.Action = interner.intern(value)
				} else if dataType == jsonparser.String {
					ev.Action, _ = jsonparser.ParseString(value)
				}
			case "Package":
				if interner != nil {
					ev.Package = interner.intern(value)
				} else if dataType == jsonparser.String {
					ev.Package, _ = jsonparser.ParseString(value)
				}
			case "Test":
				if interner != nil {
					ev.Test = interner.intern(value)
				} else if dataType == jsonparser.String {
					ev.Test, _ = jsonparser.ParseString(value)
				}
			case "Elapsed":
				if dataType == jsonparser.Number {
					ev.Elapsed, _ = jsonparser.ParseFloat(value)
				}
			case "Output":
				if dataType == jsonparser.String {
					// We only unescape here to get a flat byte slice to write.
					// Often this does not allocate if there are no escapes.
					b, _ := jsonparser.Unescape(value, customBuf)
					ev.OutputBytes = b
				}
			case "FailedBuild":
				if dataType == jsonparser.String {
					s, _ := jsonparser.ParseString(value)
					ev.FailedBuild = s
				}
			}
			return nil
		},
	)
}

// scanIterationJSONL merges one iteration's JSONL stream into aggs at iterIdx.
// meta may be nil; when set, records e.g. compile/build failure from FailedBuild on fail events.
func scanIterationJSONL(
	r io.Reader,
	iterIdx int,
	aggs map[testKey]*aggregate,
	meta *iterationScanMeta,
	slowThreshold time.Duration,
	interner *stringInterner,
	tmpDir string,
) error {
	reader := readerPool.Get().(*bufio.Reader)
	reader.Reset(r)
	defer func() {
		reader.Reset(nil)
		readerPool.Put(reader)
	}()

	customBuf := make([]byte, 0, 8192)
	var totalBuffered int

	for {
		line, err := reader.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			rest, err2 := reader.ReadBytes('\n')
			line = append(append([]byte(nil), line...), rest...)
			err = err2
		}

		if len(line) > 0 && line[0] == '{' {
			var ev TestEvent
			customBuf = customBuf[:0]
			err := parseTestEvent(line, &ev, interner, false, customBuf)
			if err == nil {
				applyTestEvent(aggs, iterIdx, &ev, meta, slowThreshold, &totalBuffered)
				if tmpDir != "" && totalBuffered > 32*1024*1024 {
					if err := flushOutputsToDisk(iterIdx, aggs, tmpDir); err != nil {
						return fmt.Errorf("iteration %d: flush output: %w", iterIdx, err)
					}
					totalBuffered = 0
				}
			}
		}

		if err != nil {
			if err != io.EOF {
				return fmt.Errorf("reading iteration %d: %w", iterIdx, err)
			}
			return nil
		}
	}
}

func applyTestEvent(
	aggs map[testKey]*aggregate,
	iterIdx int,
	ev *TestEvent,
	meta *iterationScanMeta,
	slowThreshold time.Duration,
	totalBuffered *int,
) {
	key := testKey{Package: ev.Package, Test: ev.Test}
	a := aggs[key]
	if a == nil {
		a = newAggregate()
		aggs[key] = a
	}

	if a.lastRunIter != iterIdx && ev.Action != "output" {
		a.runs++
		a.lastRunIter = iterIdx
	}

	switch ev.Action {
	case "pass":
		a.passes++
		el := seconds(ev.Elapsed)
		a.recordElapsed(el)
		if slowThreshold > 0 && el > slowThreshold {
			if len(a.slowIters) == 0 || a.slowIters[len(a.slowIters)-1] != iterIdx {
				a.slowIters = append(a.slowIters, iterIdx)
			}
		}
		if !a.timedOut && (slowThreshold == 0 || el <= slowThreshold) {
			delete(a.outputs, iterIdx)
		}
	case "fail":
		if meta != nil && ev.FailedBuild != "" {
			meta.sawFailedBuild = true
		}
		a.fails++
		if len(a.failedIters) == 0 || a.failedIters[len(a.failedIters)-1] != iterIdx {
			a.failedIters = append(a.failedIters, iterIdx)
		}
		a.recordElapsed(seconds(ev.Elapsed))
	case "skip":
		a.skips++
		if len(a.skipIters) == 0 || a.skipIters[len(a.skipIters)-1] != iterIdx {
			a.skipIters = append(a.skipIters, iterIdx)
		}
		el := seconds(ev.Elapsed)
		a.recordElapsed(el)
		if !a.timedOut {
			delete(a.outputs, iterIdx)
		}
	case "output":
		if bytes.Contains(ev.OutputBytes, []byte(timeoutPanic)) {
			a.timedOut = true
			if len(a.timeoutIters) == 0 || a.timeoutIters[len(a.timeoutIters)-1] != iterIdx {
				a.timeoutIters = append(a.timeoutIters, iterIdx)
			}
		}
		if a.outputs == nil {
			a.outputs = make(map[int]*bytes.Buffer)
		}
		buf := a.outputs[iterIdx]
		if buf == nil {
			buf = &bytes.Buffer{}
			a.outputs[iterIdx] = buf
		}
		buf.Write(ev.OutputBytes)
		if totalBuffered != nil {
			*totalBuffered += len(ev.OutputBytes)
		}
	}
}

// buildReportFromAggs produces Report and LogMap from merged aggregates (after reattributeTimeouts).
func buildReportFromAggs(
	aggs map[testKey]*aggregate,
	numIterations int,
	slowThreshold time.Duration,
) (*Report, LogMap) {
	rep := &Report{
		Iterations:    numIterations,
		SlowThreshold: slowThreshold,
	}

	pkgEntries, testsByPkg := categorizeAggregates(aggs, slowThreshold, rep)

	for _, entries := range testsByPkg {
		rep.Slow = append(rep.Slow, entries...)
	}

	rep.SlowestPackages = computeSlowestPackages(pkgEntries, slowThreshold)

	sortEntries(rep.Flakes)
	sortEntries(rep.Failures)
	sortEntries(rep.Timeouts)
	sortEntries(rep.Slow)

	rep.IterationSummaries = buildIterationSummaries(aggs, numIterations)
	rep.Summary = buildReportSummary(rep, aggs, slowThreshold)

	logs := buildLogMap(aggs)
	return rep, logs
}

func categorizeAggregates(
	aggs map[testKey]*aggregate,
	slowThreshold time.Duration,
	rep *Report,
) ([]TestEntry, map[string][]TestEntry) {
	var pkgEntries []TestEntry
	var testsByPkg = make(map[string][]TestEntry)

	for key, a := range aggs {
		minE, p50 := stats(a.elapseds)
		runs := a.runs
		ciLo, ciHi := WilsonScoreInterval(a.fails, runs, 0)
		base := TestEntry{
			Package:       key.Package,
			Test:          key.Test,
			Runs:          runs,
			Successes:     a.passes,
			Fails:         a.fails,
			Skips:         a.skips,
			Timeouts:      len(a.timeoutIters),
			FailRateLower: &ciLo,
			FailRateUpper: &ciHi,
			MinElapsed:    minE,
			MaxElapsed:    a.maxElapsed,
			P50Elapsed:    p50,
			FailIters:     a.failedIters,
			TimeoutIters:  a.timeoutIters,
			SlowIters:     a.slowIters,
		}
		if key.Test == "" {
			pkgEntries = append(pkgEntries, base)
		}

		switch {
		case a.timedOut:
			rep.Timeouts = append(rep.Timeouts, base)
		case key.Test == "" && a.fails == 0:
			// Package-level pass summary or benign events (no failing tests).
		case key.Test == "" && a.fails > 0:
			// Build failures, TestMain/init failures: Test is empty in go test -json.
			if a.passes > 0 {
				rep.Flakes = append(rep.Flakes, base)
			} else {
				rep.Failures = append(rep.Failures, base)
			}
		case key.Test != "" && a.passes > 0 && a.fails > 0:
			rep.Flakes = append(rep.Flakes, base)
		case key.Test != "" && a.fails > 0 && a.passes == 0:
			rep.Failures = append(rep.Failures, base)
		}

		if key.Test != "" && !a.timedOut && slowThreshold > 0 && a.maxElapsed > slowThreshold {
			testsByPkg[key.Package] = append(testsByPkg[key.Package], base)
		}
	}
	return pkgEntries, testsByPkg
}

func computeSlowestPackages(pkgEntries []TestEntry, slowThreshold time.Duration) []TestEntry {
	if slowThreshold <= 0 {
		return nil
	}
	slices.SortFunc(pkgEntries, func(a, b TestEntry) int {
		return cmp.Or(
			cmp.Compare(b.MaxElapsed, a.MaxElapsed),
			strings.Compare(a.Package, b.Package),
		)
	})
	var slowest []TestEntry
	for _, pkg := range pkgEntries {
		if pkg.MaxElapsed >= slowThreshold && !pkgAggregateExcludedFromSlowReports(pkg) {
			slowest = append(slowest, pkg)
			if len(slowest) >= 10 {
				break
			}
		}
	}
	return slowest
}

func buildIterationSummaries(aggs map[testKey]*aggregate, numIterations int) []IterationSummary {
	iterFails := make(map[int][]string, numIterations)
	iterTimedOut := make(map[int]bool, numIterations)
	iterPkgHasTestFail := make(map[int]map[string]bool, numIterations)
	for key, a := range aggs {
		if key.Test == "" {
			continue
		}
		for _, i := range a.failedIters {
			if iterPkgHasTestFail[i] == nil {
				iterPkgHasTestFail[i] = make(map[string]bool)
			}
			iterPkgHasTestFail[i][key.Package] = true
		}
	}
	for key, a := range aggs {
		for _, i := range a.timeoutIters {
			iterTimedOut[i] = true
		}
		failName := key.Test
		if failName == "" {
			failName = key.Package
		}
		for _, i := range a.failedIters {
			if key.Test == "" && iterPkgHasTestFail[i][key.Package] {
				continue
			}
			iterFails[i] = append(iterFails[i], failName)
		}
	}
	summaries := make([]IterationSummary, numIterations)
	for i := range numIterations {
		s := IterationSummary{Index: i}
		switch {
		case iterTimedOut[i]:
			s.Result = "timeout"
		case len(iterFails[i]) > 0:
			s.Result = "fail"
			sort.Strings(iterFails[i])
			s.FailingTests = iterFails[i]
		default:
			s.Result = "pass"
		}
		summaries[i] = s
	}
	return summaries
}

func buildReportSummary(rep *Report, aggs map[testKey]*aggregate, slowThreshold time.Duration) *ReportSummary {
	if len(aggs) == 0 {
		return nil
	}
	distinct := 0
	for k := range aggs {
		if k.Test != "" {
			distinct++
		}
	}
	flakeNamed := 0
	for _, e := range rep.Flakes {
		if e.Test != "" {
			flakeNamed++
		}
	}
	var flakeFailRuns, flakeTotalRuns int
	iterWithFlakeFail := make(map[int]struct{})
	for _, e := range rep.Flakes {
		flakeFailRuns += e.Fails
		flakeTotalRuns += e.Runs
		for _, i := range e.FailIters {
			iterWithFlakeFail[i] = struct{}{}
		}
	}
	slowCount := len(rep.Slow)

	s := &ReportSummary{
		DistinctNamedTests: distinct,
		FlakeNamedCount:    flakeNamed,
		FlakeFailRuns:      flakeFailRuns,
		FlakeTotalRuns:     flakeTotalRuns,
		SlowCount:          slowCount,
	}
	if rep.Iterations > 0 {
		s.FlakeFailingIterations = len(iterWithFlakeFail)
		s.FlakeIterationTotal = rep.Iterations
		v := float64(s.FlakeFailingIterations) / float64(rep.Iterations)
		s.FlakeIterationFailRate = &v
		lo, hi := WilsonScoreInterval(s.FlakeFailingIterations, s.FlakeIterationTotal, 1.96)
		s.FlakeIterationFailRateLower = &lo
		s.FlakeIterationFailRateUpper = &hi
	}
	if distinct > 0 {
		v := float64(flakeNamed) / float64(distinct)
		s.FlakePrevalence = &v
	}
	if flakeTotalRuns > 0 {
		v := float64(flakeFailRuns) / float64(flakeTotalRuns)
		s.FlakeExecutionFailRate = &v
		lo, hi := WilsonScoreInterval(flakeFailRuns, flakeTotalRuns, 1.96)
		s.FlakeExecutionFailRateLower = &lo
		s.FlakeExecutionFailRateUpper = &hi
	}
	if slowThreshold > 0 && distinct > 0 {
		v := float64(slowCount) / float64(distinct)
		s.SlowPrevalence = &v
	}
	return s
}

// IterationDigest summarizes one iteration JSONL log for per-iteration CLI output.
// Counts match a single-iteration Analyze (same rules as the final report).
type IterationDigest struct {
	Result        string   // pass, fail, timeout
	RanTests      int      // distinct named tests (package.test) that executed (pass/fail/timeout), excluding skip-only
	FailTests     int      // len(IterationSummaries[0].FailingTests)
	TimeoutTests  int      // len(Timeouts) for this iteration
	SkipTests     int      // distinct named tests skipped in this iteration
	SlowTests     int      // tests over slow threshold
	BuildFailure  bool     // compile/build failed or heuristic package-level fail with no named tests run
	FailingTests  []string // named tests / packages that failed this iteration
	TimedOutTests []string // named tests that timed out this iteration
}

// countNamedTestsRanInAggs counts distinct non-empty test keys that recorded
// pass, fail, or timeout in this iteration (skip-only tests are excluded).
func countNamedTestsRanInAggs(aggs map[testKey]*aggregate) int {
	n := 0
	for k, a := range aggs {
		if k.Test == "" {
			continue
		}
		if a.runs == 0 || aggregateSkipOnly(a) {
			continue
		}
		n++
	}
	return n
}

// aggregateSkipOnly is true when the test was only skipped (no pass/fail/timeout).
func aggregateSkipOnly(a *aggregate) bool {
	return len(a.skipIters) > 0 && a.passes == 0 && len(a.failedIters) == 0 && !a.timedOut
}

func countNamedTestsSkippedInAggs(aggs map[testKey]*aggregate) int {
	n := 0
	for k, a := range aggs {
		if k.Test == "" {
			continue
		}
		if len(a.skipIters) > 0 {
			n++
		}
	}
	return n
}

// DigestIterationJSONL parses one `go test -json` stream and returns counts for progress UI.
// It uses the same scan + report pipeline as Analyze for one iteration (no redundant Analyze wrapper).
func DigestIterationJSONL(r io.Reader, slowThreshold time.Duration) (IterationDigest, error) {
	aggs := make(map[testKey]*aggregate)
	var meta iterationScanMeta
	interner := newStringInterner()
	if err := scanIterationJSONL(r, 0, aggs, &meta, slowThreshold, interner, ""); err != nil {
		return IterationDigest{}, err
	}
	if err := reattributeTimeoutsIter(aggs, 0, ""); err != nil {
		return IterationDigest{}, err
	}
	ran := countNamedTestsRanInAggs(aggs)
	rep, _ := buildReportFromAggs(aggs, 1, slowThreshold)
	d := iterationDigestFromReport(rep)
	d.RanTests = ran
	d.SkipTests = countNamedTestsSkippedInAggs(aggs)
	d.BuildFailure = meta.sawFailedBuild ||
		(d.Result == "fail" && d.RanTests == 0 && d.FailTests > 0)
	return d, nil
}

func iterationDigestFromReport(rep *Report) IterationDigest {
	if rep.Iterations == 0 {
		return IterationDigest{Result: "pass"}
	}
	s := rep.IterationSummaries[0]
	slowTests := 0
	if rep.Summary != nil {
		slowTests = rep.Summary.SlowCount
	}
	return IterationDigest{
		Result:        s.Result,
		FailTests:     len(s.FailingTests),
		SlowTests:     slowTests,
		TimeoutTests:  len(rep.Timeouts),
		FailingTests:  append([]string(nil), s.FailingTests...),
		TimedOutTests: timedOutTestNamesFromReport(rep),
	}
}

func timedOutTestNamesFromReport(rep *Report) []string {
	if len(rep.Timeouts) == 0 {
		return nil
	}
	names := make([]string, 0, len(rep.Timeouts))
	for _, e := range rep.Timeouts {
		name := e.Test
		if name == "" {
			name = e.Package
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// AnalyzeResults opens every `iteration-*.log.jsonl` file in resultsDir, in
// numeric-iteration order, and delegates to Analyze.
func AnalyzeResults(resultsDir string, slowThreshold time.Duration) (*Report, LogMap, func(), error) {
	tmpDir, err := os.MkdirTemp("", "testrig-analyze-*")
	if err != nil {
		return nil, nil, nil, err
	}
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	matches, err := filepath.Glob(filepath.Join(resultsDir, "iteration-*.log.jsonl"))
	if err != nil {
		return nil, nil, cleanup, err
	}
	sort.Slice(matches, func(i, j int) bool {
		return iterNumber(matches[i]) < iterNumber(matches[j])
	})
	aggs := make(map[testKey]*aggregate)
	interner := newStringInterner()
	for i, p := range matches {
		if err := func() error {
			f, err := os.Open(p) //nolint:gosec // G304: path from filepath.Glob
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			return scanIterationJSONL(f, i, aggs, nil, slowThreshold, interner, tmpDir)
		}(); err != nil {
			return nil, nil, cleanup, err
		}
		if err := reattributeTimeoutsIter(aggs, i, tmpDir); err != nil {
			return nil, nil, cleanup, err
		}
		if err := flushOutputsToDisk(i, aggs, tmpDir); err != nil {
			return nil, nil, cleanup, err
		}
	}
	rep, logs := buildReportFromAggs(aggs, len(matches), slowThreshold)
	return rep, logs, cleanup, nil
}

// WriteReport writes the report as pretty JSON to <resultsDir>/report.json.
func WriteReport(resultsDir string, rep *Report) error {
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(resultsDir, "report.json"), b, 0600)
}

// WriteLogFiles moves per-test per-iteration temporary log files into <resultsDir>/logs/
// for flagged tests and populates each flagged TestEntry's Logs slice with a
// compact problem-kind, iteration-range, and path pattern.
func WriteLogFiles(resultsDir string, rep *Report, logs LogMap) error {
	if rep == nil {
		return nil
	}
	logsDir := filepath.Join(resultsDir, "logs")
	if err := os.MkdirAll(logsDir, 0700); err != nil {
		return err
	}
	for _, group := range rep.TestGroups() {
		for ei, entry := range *group.Entries {
			key := testKey{Package: entry.Package, Test: entry.Test}
			m, ok := logs[key]
			if !ok || len(m) == 0 {
				continue
			}
			iterations := group.Iters(entry)
			budgetIteration := longestIterationString(iterations)
			written := make([]int, 0, len(iterations))
			for _, it := range iterations {
				tmpPath := m[it]
				if tmpPath == "" {
					continue
				}
				name := diagnoseLogFilenameForIterWithBudget(
					entry.Package,
					entry.Test,
					strconv.Itoa(it),
					budgetIteration,
				)
				abs := filepath.Join(logsDir, name)

				// Rename the temporary file to the final destination
				if err := os.Rename(tmpPath, abs); err != nil {
					if os.IsNotExist(err) {
						continue
					}
					// Fallback to copy if rename fails across filesystems
					src, readErr := os.Open(tmpPath) //nolint:gosec // G304: path is securely generated temp file
					if readErr != nil {
						return readErr
					}
					dst, createErr := os.Create(abs) //nolint:gosec // G304: path from filepath.Join
					if createErr != nil {
						_ = src.Close()
						return createErr
					}
					_, copyErr := io.Copy(dst, src)
					_ = src.Close()
					_ = dst.Close()
					if copyErr != nil {
						return copyErr
					}
					_ = os.Remove(tmpPath)
				}

				written = append(written, it)
			}
			if len(written) > 0 {
				(*group.Entries)[ei].Logs = append((*group.Entries)[ei].Logs, ProblemLog{
					Type:  group.LogKind,
					Iters: compactIterations(written),
					Path: filepath.Join(
						"logs",
						diagnoseLogFilenameForIterWithBudget(entry.Package, entry.Test, "{iter}", budgetIteration),
					),
				})
			}
		}
	}
	return nil
}

func longestIterationString(iterations []int) string {
	longest := "{iter}"
	for _, it := range iterations {
		s := strconv.Itoa(it)
		if len(s) > len(longest) {
			longest = s
		}
	}
	return longest
}

func diagnoseLogFilenameForIterWithBudget(pkg, test string, iteration string, budgetIteration string) string {
	var parts []string
	if p := sanitize(shortPackage(pkg)); p != "" {
		parts = append(parts, p)
	}
	if t := sanitize(test); t != "" {
		parts = append(parts, t)
	}
	base := strings.Join(parts, "_")
	if base == "" {
		base = "test"
	}
	suffix := fmt.Sprintf("_iter-%s.log", iteration)
	name := base + suffix
	if len(name) <= maxDiagnoseLogFilenameBytes {
		return name
	}
	sum := sha256.Sum256([]byte(base))
	hash := fmt.Sprintf("_%x", sum[:4])
	budgetSuffix := fmt.Sprintf("_iter-%s.log", budgetIteration)
	reservedSuffix := max(len(suffix), len(budgetSuffix))
	return truncateUTF8MaxBytes(base, maxDiagnoseLogFilenameBytes-len(hash)-reservedSuffix) + hash + suffix
}

func compactIterations(iters []int) string {
	if len(iters) == 0 {
		return ""
	}
	sorted := append([]int(nil), iters...)
	sort.Ints(sorted)
	var parts []string
	for i := 0; i < len(sorted); {
		start := sorted[i]
		end := start
		i++
		for i < len(sorted) && sorted[i] == end+1 {
			end = sorted[i]
			i++
		}
		if start == end {
			parts = append(parts, strconv.Itoa(start))
		} else {
			parts = append(parts, fmt.Sprintf("%d-%d", start, end))
		}
	}
	return strings.Join(parts, ",")
}

// WriteCSV writes a human-readable CSV of every flagged test
// (Flakes ∪ Failures ∪ Timeouts ∪ Slow) to <resultsDir>/report.csv.
// Rows sort worst-first: (timeouts+fails) desc, then package, then test.
func WriteCSV(resultsDir string, rep *Report) error {
	if rep == nil {
		return nil
	}
	rows := flaggedRows(rep)
	f, err := os.Create(filepath.Join(resultsDir, "report.csv")) //nolint:gosec // G304: path from filepath.Join
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	w := csv.NewWriter(f)
	if err := w.Write([]string{
		"package", "test", "category",
		"runs", "successes", "fails", "skips", "timeouts",
		"min", "max", "p50",
	}); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write(r.record()); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

type csvRow struct {
	Package   string
	Test      string
	Category  string
	Runs      int
	Successes int
	Fails     int
	Skips     int
	Timeouts  int
	Min       time.Duration
	Max       time.Duration
	P50       time.Duration
}

func (r csvRow) record() []string {
	return []string{
		r.Package, r.Test, r.Category,
		strconv.Itoa(r.Runs),
		strconv.Itoa(r.Successes),
		strconv.Itoa(r.Fails),
		strconv.Itoa(r.Skips),
		strconv.Itoa(r.Timeouts),
		r.Min.Round(time.Millisecond).String(),
		r.Max.Round(time.Millisecond).String(),
		r.P50.Round(time.Millisecond).String(),
	}
}

// flaggedRows builds the deduped CSV row set. A test in both Flakes and Slow
// is categorized as "flake" (primary signal wins over "slow").
// The implicit category precedence rule is: Timeout > Failure > Flake > Slow.
func flaggedRows(rep *Report) []csvRow {
	seen := map[testKey]struct{}{}
	var rows []csvRow

	for _, group := range rep.TestGroups() {
		for _, e := range *group.Entries {
			k := testKey{Package: e.Package, Test: e.Test}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			rows = append(rows, csvRow{
				Package:   e.Package,
				Test:      e.Test,
				Category:  group.CSVCategory,
				Runs:      e.Runs,
				Successes: e.Successes,
				Fails:     e.Fails,
				Skips:     e.Skips,
				Timeouts:  e.Timeouts,
				Min:       e.MinElapsed,
				Max:       e.MaxElapsed,
				P50:       e.P50Elapsed,
			})
		}
	}

	sort.SliceStable(rows, func(i, j int) bool {
		li := rows[i].Timeouts + rows[i].Fails
		lj := rows[j].Timeouts + rows[j].Fails
		if li != lj {
			return li > lj
		}
		if rows[i].Package != rows[j].Package {
			return rows[i].Package < rows[j].Package
		}
		return rows[i].Test < rows[j].Test
	})
	return rows
}

// PrintSummary writes a human-readable diagnose summary: scope line and rates
// table first, then detail sections (Broken, Flaky, Timeout, Slow) as flat lines.
// Broken and Timeout entries are sorted alphabetically by package then test.
// Flaky entries are sorted by fails/runs (desc), then fails (desc), then name.
// Slow entries are sorted by max runtime (desc), then name.
func PrintSummary(w io.Writer, rep *Report) {
	if rep == nil {
		return
	}

	printOverallStats(w, rep)

	if n := len(rep.Failures); n > 0 {
		fails := append([]TestEntry(nil), rep.Failures...)
		sort.Slice(fails, func(i, j int) bool {
			if fails[i].Package != fails[j].Package {
				return fails[i].Package < fails[j].Package
			}
			return fails[i].Test < fails[j].Test
		})
		printSummarySectionFlat(w, "Broken", n, fails, termstyle.Bad, termstyle.Bad, formatBrokenStats)
	}

	if n := len(rep.Flakes); n > 0 {
		flakes := append([]TestEntry(nil), rep.Flakes...)
		sort.Slice(flakes, func(i, j int) bool {
			ri := flakeFailRatio(flakes[i])
			rj := flakeFailRatio(flakes[j])
			if ri != rj {
				return ri > rj
			}
			if flakes[i].Fails != flakes[j].Fails {
				return flakes[i].Fails > flakes[j].Fails
			}
			return entryFQName(flakes[i]) < entryFQName(flakes[j])
		})
		printSummarySectionFlat(w, "Flaky", n, flakes, termstyle.Flaky, termstyle.Flaky, formatFlakyStats)
	}

	if n := len(rep.Timeouts); n > 0 {
		touts := append([]TestEntry(nil), rep.Timeouts...)
		sort.Slice(touts, func(i, j int) bool {
			if touts[i].Package != touts[j].Package {
				return touts[i].Package < touts[j].Package
			}
			return touts[i].Test < touts[j].Test
		})
		printSummarySectionFlat(w, "Timeout", n, touts, termstyle.Accent, termstyle.Accent, formatTimeoutStats)
	}

	if n := len(rep.Slow); n > 0 {
		slow := append([]TestEntry(nil), rep.Slow...)
		sort.Slice(slow, func(i, j int) bool {
			if slow[i].MaxElapsed != slow[j].MaxElapsed {
				return slow[i].MaxElapsed > slow[j].MaxElapsed
			}
			if slow[i].Package != slow[j].Package {
				return slow[i].Package < slow[j].Package
			}
			return slow[i].Test < slow[j].Test
		})
		printSummarySectionFlat(w, "Slow", n, slow, termstyle.Accent, termstyle.Accent, formatSlowStats)
	}

	if n := len(rep.SlowestPackages); n > 0 {
		pkgs := append([]TestEntry(nil), rep.SlowestPackages...)
		printSummarySectionFlat(w, "Slowest Packages", n, pkgs, termstyle.Muted, termstyle.Muted, formatSlowStats)
	}
}

func printOverallStats(w io.Writer, rep *Report) {
	if rep == nil {
		return
	}
	hasFindings := len(rep.Failures) > 0 || len(rep.Flakes) > 0 || len(rep.Timeouts) > 0 ||
		len(rep.Slow) > 0 || len(rep.SlowestPackages) > 0
	s := rep.Summary
	hasIterRuntime := s != nil &&
		(s.IterationDurationMin > 0 || s.IterationDurationP50 > 0 || s.IterationDurationMax > 0)
	hasSummaryStats := s != nil && (s.DistinctNamedTests > 0 || s.FlakeTotalRuns > 0 || hasIterRuntime)
	if !hasFindings && !hasSummaryStats {
		return
	}

	printOverallScope(w, rep)
	if s == nil {
		_, _ = fmt.Fprintln(w)
		return
	}
	printOverallRatesTable(w, rep)
	_, _ = fmt.Fprintln(w)
}

func overallFlakyIterationCI(s *ReportSummary) string {
	if s == nil || s.FlakeIterationFailRateLower == nil || s.FlakeIterationFailRateUpper == nil {
		return ""
	}
	gap := *s.FlakeIterationFailRateUpper - *s.FlakeIterationFailRateLower
	ciText := fmt.Sprintf(
		"CI %.1f%%–%.1f%%",
		*s.FlakeIterationFailRateLower*100,
		*s.FlakeIterationFailRateUpper*100,
	)
	return ciStyleForGap(gap).Render(ciText)
}

func slowPackageCount(rep *Report) int {
	if rep == nil {
		return 0
	}
	return len(rep.SlowestPackages)
}

func countBrokenNamedTests(rep *Report) int {
	if rep == nil {
		return 0
	}
	n := 0
	for _, f := range rep.Failures {
		if f.Test != "" {
			n++
		}
	}
	return n
}

func printSummarySectionFlat(
	w io.Writer,
	title string,
	n int,
	entries []TestEntry,
	headingStyle, lineStyle lipgloss.Style,
	statsFor func(TestEntry) string,
) {
	_, _ = fmt.Fprintln(w, headingStyle.Render(fmt.Sprintf("%s (%d)", title, n)))
	for _, e := range entries {
		_, _ = fmt.Fprintln(w, lineStyle.Render(formatSummaryFlatLine(e, statsFor)))
	}
	_, _ = fmt.Fprintln(w)
}

func formatSummaryFlatLine(e TestEntry, statsFor func(TestEntry) string) string {
	stats := statsFor(e)
	if e.Test == "" {
		if stats == "" {
			return e.Package
		}
		return e.Package + "  " + stats
	}
	if stats == "" {
		return e.Package + "  " + e.Test
	}
	return e.Package + "  " + e.Test + "  " + stats
}

func formatBrokenStats(TestEntry) string { return "" }

func formatTimeoutStats(TestEntry) string { return "" }

func formatFlakyStats(e TestEntry) string {
	runs := e.Runs
	if runs < 1 {
		runs = e.Successes + e.Fails
	}
	if runs < 1 {
		runs = 1
	}
	pct := flakeFailRatio(e) * 100
	lo, hi := WilsonScoreInterval(e.Fails, runs, 0)
	ciText := fmt.Sprintf(" [Confidence Interval: %.1f%%–%.1f%%]", lo*100, hi*100)
	ci := ciStyleForGap(hi - lo).Render(ciText)
	return fmt.Sprintf("(%d/%d) %.1f%%%s", e.Fails, runs, pct, ci)
}

func formatSlowStats(e TestEntry) string {
	return termstyle.Accent.Render(e.MaxElapsed.Round(time.Millisecond).String())
}

func entryFQName(e TestEntry) string {
	if e.Test == "" {
		return e.Package
	}
	return e.Package + "." + e.Test
}

func flakeFailRatio(e TestEntry) float64 {
	runs := e.Runs
	if runs < 1 {
		runs = e.Successes + e.Fails
	}
	if runs < 1 {
		return 0
	}
	return float64(e.Fails) / float64(runs)
}

func reattributeTimeoutsIter(aggs map[testKey]*aggregate, i int, tmpDir string) error {
	keys := make([]testKey, 0, len(aggs))
	for k := range aggs {
		keys = append(keys, k)
	}
	for _, key := range keys {
		a := aggs[key]
		if !a.timedOut || len(a.timeoutIters) == 0 || a.timeoutIters[len(a.timeoutIters)-1] != i {
			continue
		}

		names, r, f, err := getTimeoutOutputs(a, i, key, tmpDir)
		if err != nil {
			return err
		}
		if r == nil {
			continue
		}

		if len(names) == 0 || slices.Contains(names, key.Test) {
			if f != nil {
				_ = f.Close()
			}
			continue
		}

		// Remove the timeout from the parent
		a.timeoutIters = a.timeoutIters[:len(a.timeoutIters)-1]
		if len(a.timeoutIters) == 0 {
			a.timedOut = false
		}

		for _, name := range names {
			nk := testKey{Package: key.Package, Test: name}
			na := aggs[nk]
			if na == nil {
				na = newAggregate()
				aggs[nk] = na
			}
			na.timedOut = true
			if len(na.timeoutIters) == 0 || na.timeoutIters[len(na.timeoutIters)-1] != i {
				na.timeoutIters = append(na.timeoutIters, i)
			}
			if na.lastRunIter != i {
				na.runs++
				na.lastRunIter = i
			}

			if err := copyTimeoutToTarget(i, na, f, r, tmpDir); err != nil {
				if f != nil {
					_ = f.Close()
				}
				return err
			}
		}

		if f != nil {
			_ = f.Close()
		}
	}
	return nil
}

func getTimeoutOutputs(a *aggregate, i int, key testKey, tmpDir string) ([]string, io.Reader, *os.File, error) {
	var r io.Reader
	var f *os.File
	var err error

	if tmpDir != "" {
		if err := flushOutputForBuffer(i, a, tmpDir); err != nil {
			return nil, nil, nil, fmt.Errorf(
				"reattribute timeouts iter %d pkg %s test %s: flush: %w",
				i,
				key.Package,
				key.Test,
				err,
			)
		}
		if a.logPaths != nil && a.logPaths[i] != "" {
			f, err = os.Open(a.logPaths[i]) //nolint:gosec
			if err != nil {
				return nil, nil, nil, fmt.Errorf(
					"reattribute timeouts iter %d pkg %s test %s: open: %w",
					i,
					key.Package,
					key.Test,
					err,
				)
			}
			r = f
		}
	} else {
		if buf := a.outputs[i]; buf != nil && buf.Len() > 0 {
			r = bytes.NewReader(buf.Bytes())
		}
	}

	if r == nil {
		return nil, nil, nil, nil
	}

	names, err := parseRunningTests(r)
	if err != nil {
		if f != nil {
			_ = f.Close()
		}
		return nil, nil, nil, fmt.Errorf(
			"reattribute timeouts iter %d pkg %s test %s: parse: %w",
			i,
			key.Package,
			key.Test,
			err,
		)
	}
	return names, r, f, nil
}

func copyTimeoutToTarget(i int, na *aggregate, f *os.File, r io.Reader, tmpDir string) error {
	if tmpDir != "" {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return fmt.Errorf("seek: %w", err)
		}
		if err := flushOutputForBuffer(i, na, tmpDir); err != nil {
			return fmt.Errorf("flush target: %w", err)
		}
		var dst *os.File
		var err error
		if na.logPaths != nil && na.logPaths[i] != "" {
			dst, err = os.OpenFile(na.logPaths[i], os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		} else {
			dst, err = os.CreateTemp(tmpDir, "log-*")
			if err == nil {
				if na.logPaths == nil {
					na.logPaths = make(map[int]string)
				}
				na.logPaths[i] = dst.Name()
			}
		}
		if err != nil {
			return fmt.Errorf("open target log: %w", err)
		}
		_, copyErr := io.Copy(dst, f)
		_ = dst.Close()
		if copyErr != nil {
			return fmt.Errorf("copy log: %w", copyErr)
		}
	} else {
		if na.outputs == nil {
			na.outputs = make(map[int]*bytes.Buffer)
		}
		if na.outputs[i] == nil {
			na.outputs[i] = &bytes.Buffer{}
		}
		if br, ok := r.(*bytes.Reader); ok {
			_, _ = br.Seek(0, io.SeekStart)
			_, _ = io.Copy(na.outputs[i], br)
		}
	}
	return nil
}

func flushOutputForBuffer(iterIdx int, a *aggregate, tmpDir string) error {
	buf := a.outputs[iterIdx]
	if buf == nil || buf.Len() == 0 {
		return nil
	}

	var f *os.File
	var err error
	if a.logPaths != nil && a.logPaths[iterIdx] != "" {
		f, err = os.OpenFile(a.logPaths[iterIdx], os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	} else {
		f, err = os.CreateTemp(tmpDir, "log-*")
		if err == nil {
			if a.logPaths == nil {
				a.logPaths = make(map[int]string)
			}
			a.logPaths[iterIdx] = f.Name()
		}
	}
	if err != nil {
		return err
	}
	_, err = buf.WriteTo(f)
	if err != nil {
		_ = f.Close()
		return err
	}
	_ = f.Close()

	delete(a.outputs, iterIdx)
	return nil
}

func flushOutputsToDisk(iterIdx int, aggs map[testKey]*aggregate, tmpDir string) error {
	for _, a := range aggs {
		if err := flushOutputForBuffer(iterIdx, a, tmpDir); err != nil {
			return err
		}
	}
	return nil
}

// parseRunningTests extracts test names from a `panic: test timed out` block:
//
//	running tests:
//	        TestName (5s)
//	        TestOther/sub (4s)
//	goroutine 17 [running]:
//
// The list ends at the first `goroutine ` line (stack trace boundary). Lines
// that still contain embedded whitespace after stripping the " (Xs)" suffix
// aren't test names: before any name has been seen they are tolerated as
// preamble (e.g. extra header text); after a name has been seen they mark the
// end of the names section (e.g. trailing prose before the stack trace).
func parseRunningTests(r io.Reader) ([]string, error) {
	reader := bufio.NewReader(r)
	var names []string
	found := false
	for {
		lineBytes, isPrefix, err := reader.ReadLine()
		if err != nil {
			if err == io.EOF {
				break
			}
			return names, err
		}

		line := string(lineBytes)

		// Consume the rest of the line if it was too long for ReadLine buffer
		for isPrefix {
			_, isPrefix, err = reader.ReadLine()
			if err != nil {
				break
			}
		}

		if !found {
			if strings.Contains(line, "running tests:") {
				found = true
			}
			continue
		}
		if strings.HasPrefix(line, "goroutine ") {
			break
		}
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if open := strings.LastIndex(trim, " ("); open > 0 {
			trim = strings.TrimSpace(trim[:open])
		}
		if strings.Contains(trim, " ") {
			if len(names) > 0 {
				break
			}
			continue
		}
		names = append(names, trim)
	}
	return names, nil
}

// buildLogMap returns the raw per-iteration output for every (pkg, test) that
// has any output recorded. Callers use this to write per-test log files.
func buildLogMap(aggs map[testKey]*aggregate) LogMap {
	out := LogMap{}
	for k, a := range aggs {
		if len(a.logPaths) == 0 {
			continue
		}
		m := map[int]string{}
		for i, p := range a.logPaths {
			if p != "" {
				m[i] = p
			}
		}
		if len(m) > 0 {
			out[k] = m
		}
	}
	return out
}

// pkgAggregateExcludedFromSlowReports is true for package-level aggregates that
// belong only in Failures or Timeouts, not in rep.SlowestPackages ranking.
func pkgAggregateExcludedFromSlowReports(e TestEntry) bool {
	if e.Test != "" {
		return false
	}
	if e.Timeouts > 0 {
		return true
	}
	if e.Fails > 0 && e.Successes == 0 {
		return true
	}
	return false
}

// sortedDurationStats returns min, max, and median (p50) from wall-clock or elapsed samples.
// Returns (0, 0, 0) for an empty sample.
func sortedDurationStats(samples []time.Duration) (minDur, maxDur, p50 time.Duration) {
	if len(samples) == 0 {
		return 0, 0, 0
	}
	sorted := append([]time.Duration(nil), samples...)
	slices.Sort(sorted)
	minDur = sorted[0]
	maxDur = sorted[len(sorted)-1]
	n := len(sorted)
	if n%2 == 1 {
		p50 = sorted[n/2]
	} else {
		p50 = (sorted[n/2-1] + sorted[n/2]) / 2
	}
	return minDur, maxDur, p50
}

// stats computes min and p50 from a sample of durations.
// Returns (0, 0) for an empty sample.
func stats(samples []time.Duration) (minDur, p50 time.Duration) {
	minDur, _, p50 = sortedDurationStats(samples)
	return minDur, p50
}

// shortPackage keeps the last two path segments of a Go import path so log
// filenames stay under the OS NAME_MAX (255 on most filesystems). Deeply
// nested packages like github.com/.../core/services/ocr2/plugins/llo collapse
// to plugins/llo.
func shortPackage(pkg string) string {
	if pkg == "" {
		return ""
	}
	parts := strings.Split(pkg, "/")
	if len(parts) <= 2 {
		return pkg
	}
	return strings.Join(parts[len(parts)-2:], "/")
}

// sanitize turns a package path or test name into a filename-safe token.
// Replaces path separators and other hostile characters with '_'.
func sanitize(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func seconds(f float64) time.Duration {
	return time.Duration(f * float64(time.Second))
}

func sortEntries(entries []TestEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Package != entries[j].Package {
			return entries[i].Package < entries[j].Package
		}
		return entries[i].Test < entries[j].Test
	})
}

func iterNumber(path string) int {
	base := filepath.Base(path)
	base = strings.TrimPrefix(base, "iteration-")
	base = strings.TrimSuffix(base, ".log.jsonl")
	n := 0
	for _, c := range base {
		if c < '0' || c > '9' {
			return -1
		}
		n = n*10 + int(c-'0')
	}
	return n
}
