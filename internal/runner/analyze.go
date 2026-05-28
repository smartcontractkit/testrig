package runner

import (
	"bufio"
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
	"time"

	"charm.land/lipgloss/v2"

	"github.com/smartcontractkit/testrig/internal/termstyle"
)

const maxDiagnoseLogFilenameBytes = 240

// timeoutPanic appears in go test -json output when the test binary's
// -timeout fires. It may be attached to a running test or to the package.
const timeoutPanic = "panic: test timed out"

// TestEvent mirrors cmd/internal/test2json's TestEvent; only fields we need.
type TestEvent struct {
	Action      string  `json:"Action"`
	Package     string  `json:"Package"`
	Test        string  `json:"Test"`
	Elapsed     float64 `json:"Elapsed"`
	Output      string  `json:"Output"`
	FailedBuild string  `json:"FailedBuild,omitempty"`
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
	passes        int
	fails         int
	skips         int
	maxElapsed    time.Duration
	timedOut      bool
	iterations    map[int]struct{}
	failedIters   map[int]bool
	timeoutIters  map[int]bool
	skipIters     map[int]bool
	outputs       map[int]*strings.Builder
	elapseds      []time.Duration
	elapsedByIter map[int]time.Duration
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

// Analyze reads per-iteration test2json streams and classifies tests.
// Malformed lines are silently skipped (go test can interleave non-JSON).
func Analyze(iterations []io.Reader, slowThreshold time.Duration) (*Report, LogMap, error) {
	aggs := make(map[testKey]*aggregate)
	for i, r := range iterations {
		if err := scanIterationJSONL(r, i, aggs, nil); err != nil {
			return nil, nil, err
		}
	}
	reattributeTimeouts(aggs, newAggregate)
	rep, logs := buildReportFromAggs(aggs, len(iterations), slowThreshold)
	return rep, logs, nil
}

func newAggregate() *aggregate {
	return &aggregate{
		iterations:    map[int]struct{}{},
		failedIters:   map[int]bool{},
		timeoutIters:  map[int]bool{},
		skipIters:     map[int]bool{},
		outputs:       map[int]*strings.Builder{},
		elapsedByIter: map[int]time.Duration{},
	}
}

func (a *aggregate) recordElapsed(iterIdx int, d time.Duration) {
	a.elapseds = append(a.elapseds, d)
	a.elapsedByIter[iterIdx] = d
	if d > a.maxElapsed {
		a.maxElapsed = d
	}
}

// scanIterationJSONL merges one iteration's JSONL stream into aggs at iterIdx.
// meta may be nil; when set, records e.g. compile/build failure from FailedBuild on fail events.
func scanIterationJSONL(r io.Reader, iterIdx int, aggs map[testKey]*aggregate, meta *iterationScanMeta) error {
	reader := bufio.NewReaderSize(r, 1024*1024)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 && line[0] == '{' {
			var ev TestEvent
			if json.Unmarshal(line, &ev) == nil {
				applyTestEvent(aggs, iterIdx, &ev, meta)
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

func applyTestEvent(aggs map[testKey]*aggregate, iterIdx int, ev *TestEvent, meta *iterationScanMeta) {
	key := testKey{Package: ev.Package, Test: ev.Test}
	a := aggs[key]
	if a == nil {
		a = newAggregate()
		aggs[key] = a
	}
	switch ev.Action {
	case "pass":
		a.passes++
		a.iterations[iterIdx] = struct{}{}
		a.recordElapsed(iterIdx, seconds(ev.Elapsed))
	case "fail":
		if meta != nil && ev.FailedBuild != "" {
			meta.sawFailedBuild = true
		}
		a.fails++
		a.iterations[iterIdx] = struct{}{}
		a.failedIters[iterIdx] = true
		a.recordElapsed(iterIdx, seconds(ev.Elapsed))
	case "skip":
		a.skips++
		a.iterations[iterIdx] = struct{}{}
		a.skipIters[iterIdx] = true
		a.recordElapsed(iterIdx, seconds(ev.Elapsed))
	case "output":
		if strings.Contains(ev.Output, timeoutPanic) {
			a.timedOut = true
			a.iterations[iterIdx] = struct{}{}
			a.timeoutIters[iterIdx] = true
		}
		buf := a.outputs[iterIdx]
		if buf == nil {
			buf = &strings.Builder{}
			a.outputs[iterIdx] = buf
		}
		buf.WriteString(ev.Output)
	}
}

// buildReportFromAggs produces Report and LogMap from merged aggregates (after reattributeTimeouts).
//
//nolint:gocyclo
func buildReportFromAggs(
	aggs map[testKey]*aggregate,
	numIterations int,
	slowThreshold time.Duration,
) (*Report, LogMap) {
	rep := &Report{
		Iterations:    numIterations,
		SlowThreshold: slowThreshold,
	}

	var pkgEntries []TestEntry
	var testsByPkg = make(map[string][]TestEntry)

	for key, a := range aggs {
		minE, p50 := stats(a.elapseds)
		runs := len(a.iterations)
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
			FailIters:     sortedBoolMapKeys(a.failedIters),
			TimeoutIters:  sortedBoolMapKeys(a.timeoutIters),
			SlowIters:     slowIterations(a.elapsedByIter, slowThreshold),
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

	pkgNames := make([]string, 0, len(testsByPkg))
	for pkgName := range testsByPkg {
		pkgNames = append(pkgNames, pkgName)
	}
	sort.Strings(pkgNames)
	for _, pkgName := range pkgNames {
		rep.Slow = append(rep.Slow, testsByPkg[pkgName]...)
	}

	if slowThreshold > 0 {
		slices.SortFunc(pkgEntries, func(a, b TestEntry) int {
			return cmp.Or(
				cmp.Compare(b.MaxElapsed, a.MaxElapsed),
				strings.Compare(a.Package, b.Package),
			)
		})
		for _, pkg := range pkgEntries {
			if pkg.MaxElapsed >= slowThreshold && !pkgAggregateExcludedFromSlowReports(pkg) {
				rep.SlowestPackages = append(rep.SlowestPackages, pkg)
				if len(rep.SlowestPackages) >= 10 {
					break
				}
			}
		}
	}

	sortEntries(rep.Flakes)
	sortEntries(rep.Failures)
	sortEntries(rep.Timeouts)
	sortEntries(rep.Slow)

	iterFails := make(map[int][]string, numIterations)
	iterTimedOut := make(map[int]bool, numIterations)
	iterPkgHasTestFail := make(map[int]map[string]bool, numIterations)
	for key, a := range aggs {
		if key.Test == "" {
			continue
		}
		for i := range a.failedIters {
			if iterPkgHasTestFail[i] == nil {
				iterPkgHasTestFail[i] = make(map[string]bool)
			}
			iterPkgHasTestFail[i][key.Package] = true
		}
	}
	for key, a := range aggs {
		for i := range a.timeoutIters {
			iterTimedOut[i] = true
		}
		failName := key.Test
		if failName == "" {
			failName = key.Package
		}
		for i := range a.failedIters {
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
	rep.IterationSummaries = summaries

	rep.Summary = buildReportSummary(rep, aggs, slowThreshold)

	logs := buildLogMap(aggs)
	return rep, logs
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
	Result       string // pass, fail, timeout
	RanTests     int    // distinct named tests (package.test) that executed (pass/fail/timeout), excluding skip-only
	FailTests    int    // len(IterationSummaries[0].FailingTests)
	TimeoutTests int    // len(Timeouts) for this iteration
	SkipTests    int    // distinct named tests skipped in this iteration
	SlowTests    int    // tests over slow threshold
	BuildFailure bool   // compile/build failed or heuristic package-level fail with no named tests run
}

// countNamedTestsRanInAggs counts distinct non-empty test keys that recorded
// pass, fail, or timeout in this iteration (skip-only tests are excluded).
func countNamedTestsRanInAggs(aggs map[testKey]*aggregate) int {
	n := 0
	for k, a := range aggs {
		if k.Test == "" {
			continue
		}
		if len(a.iterations) == 0 || aggregateSkipOnly(a) {
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
	if err := scanIterationJSONL(r, 0, aggs, &meta); err != nil {
		return IterationDigest{}, err
	}
	reattributeTimeouts(aggs, newAggregate)
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
		Result:       s.Result,
		FailTests:    len(s.FailingTests),
		SlowTests:    slowTests,
		TimeoutTests: len(rep.Timeouts),
	}
}

// AnalyzeResults opens every `iteration-*.log.jsonl` file in resultsDir, in
// numeric-iteration order, and delegates to Analyze.
func AnalyzeResults(resultsDir string, slowThreshold time.Duration) (*Report, LogMap, error) {
	matches, err := filepath.Glob(filepath.Join(resultsDir, "iteration-*.log.jsonl"))
	if err != nil {
		return nil, nil, err
	}
	sort.Slice(matches, func(i, j int) bool {
		return iterNumber(matches[i]) < iterNumber(matches[j])
	})
	readers := make([]io.Reader, 0, len(matches))
	files := make([]*os.File, 0, len(matches))
	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
	}()
	for _, p := range matches {
		f, err := os.Open(p) //nolint:gosec // G304: path from filepath.Glob
		if err != nil {
			return nil, nil, err
		}
		files = append(files, f)
		readers = append(readers, f)
	}
	return Analyze(readers, slowThreshold)
}

// WriteReport writes the report as pretty JSON to <resultsDir>/report.json.
func WriteReport(resultsDir string, rep *Report) error {
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(resultsDir, "report.json"), b, 0600)
}

// WriteLogFiles writes per-test per-iteration log files under <resultsDir>/logs/
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
				out := m[it]
				if out == "" {
					continue
				}
				name := diagnoseLogFilenameForIterWithBudget(
					entry.Package,
					entry.Test,
					strconv.Itoa(it),
					budgetIteration,
				)
				abs := filepath.Join(logsDir, name)
				if err := os.WriteFile(abs, []byte(out), 0600); err != nil {
					return err
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

// reattributeTimeouts fixes the go-test-json quirk where a `panic: test timed out`
// is attached to whichever test most recently emitted events rather than the
// actually-stuck one. The real culprits are listed in the panic's
// "running tests:" block — move the timeout mark (and the captured stack
// trace) onto those tests.
func reattributeTimeouts(aggs map[testKey]*aggregate, newAgg func() *aggregate) {
	keys := make([]testKey, 0, len(aggs))
	for k := range aggs {
		keys = append(keys, k)
	}
	for _, key := range keys {
		a := aggs[key]
		if !a.timedOut {
			continue
		}
		for i := range a.timeoutIters {
			buf := a.outputs[i]
			if buf == nil {
				continue
			}
			output := buf.String()
			names := parseRunningTests(output)
			if len(names) == 0 {
				continue
			}
			if slices.Contains(names, key.Test) {
				continue
			}
			delete(a.timeoutIters, i)
			if len(a.timeoutIters) == 0 {
				a.timedOut = false
			}
			for _, name := range names {
				nk := testKey{Package: key.Package, Test: name}
				na := aggs[nk]
				if na == nil {
					na = newAgg()
					aggs[nk] = na
				}
				na.timedOut = true
				na.timeoutIters[i] = true
				na.iterations[i] = struct{}{}
				if na.outputs[i] == nil {
					na.outputs[i] = &strings.Builder{}
				}
				_, _ = na.outputs[i].WriteString(output)
			}
		}
	}
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
func parseRunningTests(output string) []string {
	const marker = "running tests:"
	_, tail, found := strings.Cut(output, marker)
	if !found {
		return nil
	}
	var names []string
	for line := range strings.SplitSeq(tail, "\n") {
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
	return names
}

// buildLogMap returns the raw per-iteration output for every (pkg, test) that
// has any output recorded. Callers use this to write per-test log files.
func buildLogMap(aggs map[testKey]*aggregate) LogMap {
	out := LogMap{}
	for k, a := range aggs {
		if len(a.outputs) == 0 {
			continue
		}
		m := map[int]string{}
		for i, buf := range a.outputs {
			if buf != nil && buf.Len() > 0 {
				m[i] = buf.String()
			}
		}
		if len(m) > 0 {
			out[k] = m
		}
	}
	return out
}

func sortedBoolMapKeys(m map[int]bool) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
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

func slowIterations(elapsedByIter map[int]time.Duration, threshold time.Duration) []int {
	if threshold <= 0 {
		return nil
	}
	var iters []int
	for iter, elapsed := range elapsedByIter {
		if elapsed > threshold {
			iters = append(iters, iter)
		}
	}
	sort.Ints(iters)
	return iters
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
