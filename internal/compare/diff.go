package compare

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/smartcontractkit/testrig/internal/runner"
)

type row struct {
	label  string
	before string
	after  string
	change string
}

type testKey struct {
	pkg  string
	test string
}

type testCategory struct {
	label string
	entry runner.TestEntry
}

type verdict int

const (
	verdictNew verdict = iota
	verdictWorse
	verdictNoChange
	verdictBetter
	verdictFixed
)

var verdictOrder = []verdict{verdictNew, verdictWorse, verdictNoChange, verdictBetter, verdictFixed}

func (v verdict) label() string {
	switch v {
	case verdictNew:
		return "New"
	case verdictWorse:
		return "Worse"
	case verdictNoChange:
		return "No clear change"
	case verdictBetter:
		return "Better"
	case verdictFixed:
		return "Fixed"
	default:
		return ""
	}
}

type testRow struct {
	test    string
	pkg     string
	before  string
	after   string
	rate    string
	verdict verdict
}

type testSection struct {
	title string
	rows  []testRow
}

type timingRow struct {
	name      string
	pkg       string
	beforeP50 string
	afterP50  string
	p50Change string
	beforeMax string
	afterMax  string
	maxChange string
}

type comparison struct {
	beforeLabel  string
	afterLabel   string
	beforePath   string
	afterPath    string
	warnings     []string
	runs         []row
	summary      []row
	testSections []testSection
	slowRows     []timingRow
	packageRows  []timingRow
}

func diff(before, after Run) comparison {
	doc := comparison{
		beforeLabel: runLabel(before, "before"),
		afterLabel:  runLabel(after, "after"),
		beforePath:  runResultsPath(before),
		afterPath:   runResultsPath(after),
	}
	doc.warnings = diffWarnings(before.Report, after.Report)
	doc.runs = diffRunRows(before.Report, after.Report)
	doc.summary = diffSummaryRows(before.Report, after.Report)
	doc.testSections = diffProblemTests(before.Report, after.Report)
	doc.slowRows = diffTimings(before.Report, after.Report, func(rep *runner.Report) []runner.TestEntry {
		if rep == nil {
			return nil
		}
		return rep.Slow
	})
	doc.packageRows = diffTimings(before.Report, after.Report, func(rep *runner.Report) []runner.TestEntry {
		if rep == nil {
			return nil
		}
		return rep.SlowestPackages
	})
	return doc
}

func runLabel(r Run, fallback string) string {
	if r.Label != "" {
		return r.Label
	}
	if meta := runMetaOf(r.Report); meta.Name != "" {
		return meta.Name
	}
	if meta := runMetaOf(r.Report); meta.ResultsDirBasename != "" {
		return meta.ResultsDirBasename
	}
	return fallback
}

func runResultsPath(r Run) string {
	return runMetaOf(r.Report).ResultsDirBasename
}

func runMetaOf(rep *runner.Report) *runner.RunMeta {
	if rep == nil || rep.Run == nil {
		return &runner.RunMeta{}
	}
	return rep.Run
}

func diffWarnings(before, after *runner.Report) []string {
	b, a := runMetaOf(before), runMetaOf(after)
	var reasons []string
	if !slices.Equal(b.GoTestArgs, a.GoTestArgs) {
		reasons = append(reasons, "go test arguments differ")
	}
	if b.SlowThreshold != a.SlowThreshold {
		reasons = append(reasons, "slow thresholds differ")
	}
	if len(reasons) == 0 {
		return nil
	}
	return []string{fmt.Sprintf(
		"%s. Results may not be comparable: report.json lists only flagged tests, so a test missing "+
			"from one run may have been fixed or may not have run at all.",
		strings.Join(reasons, " and "),
	)}
}

func diffRunRows(before, after *runner.Report) []row {
	b, a := runMetaOf(before), runMetaOf(after)
	return []row{
		{label: "Started at", before: formatTime(b.StartedAt), after: formatTime(a.StartedAt)},
		{label: "Iterations", before: intOrDash(b.DiagnoseIterations), after: intOrDash(a.DiagnoseIterations)},
		{
			label:  "go test args",
			before: mdCode(strings.Join(b.GoTestArgs, " ")),
			after:  mdCode(strings.Join(a.GoTestArgs, " ")),
		},
		{label: "Race", before: yesNo(b.Race), after: yesNo(a.Race)},
		{label: "Shuffle", before: yesNo(b.Shuffle), after: yesNo(a.Shuffle)},
		{
			label:  "Slow threshold",
			before: durationOrDash(b.SlowThreshold),
			after:  durationOrDash(a.SlowThreshold),
		},
	}
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Format("2006-01-02 15:04:05 MST")
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func intOrDash(n int) string {
	if n <= 0 {
		return "—"
	}
	return strconv.Itoa(n)
}

func durationOrDash(d time.Duration) string {
	if d <= 0 {
		return "—"
	}
	return formatDuration(d)
}

type repStats struct {
	iterations     int
	testsRun       int
	broken         int
	flaky          int
	races          int
	timeouts       int
	slow           int
	flakeFailIters int
	flakeIters     int
	passIters      int
	completed      int
	p50            time.Duration
	max            time.Duration
	race           bool
}

func statsOf(rep *runner.Report) repStats {
	if rep == nil {
		return repStats{}
	}
	stats := repStats{
		iterations: rep.Iterations,
		broken:     rep.BrokenNamedCount(),
		timeouts:   len(rep.Timeouts),
	}
	if rep.Summary != nil {
		stats.testsRun = rep.Summary.DistinctNamedTests
		stats.flaky = rep.Summary.FlakeNamedCount
		stats.races = rep.Summary.RaceNamedCount
		stats.slow = rep.Summary.SlowCount
		stats.flakeFailIters = rep.Summary.FlakeFailingIterations
		stats.flakeIters = rep.Summary.FlakeIterationTotal
		stats.p50 = rep.Summary.IterationDurationP50
		stats.max = rep.Summary.IterationDurationMax
	}
	for _, it := range rep.IterationSummaries {
		if it.Result == "pass" {
			stats.passIters++
		}
	}
	stats.completed = len(rep.IterationSummaries)
	if stats.completed == 0 {
		stats.completed = rep.Iterations
	}
	if stats.flakeIters == 0 {
		stats.flakeIters = rep.Iterations
	}
	if rep.Run != nil {
		stats.race = rep.Run.Race
	}
	return stats
}

func diffSummaryRows(before, after *runner.Report) []row {
	b, a := statsOf(before), statsOf(after)
	rows := []row{
		compareCount("Tests run", b.testsRun, a.testsRun, b.iterations, a.iterations, true),
		compareCount("Broken tests", b.broken, a.broken, b.iterations, a.iterations, false),
		compareCount("Flaky tests", b.flaky, a.flaky, b.iterations, a.iterations, false),
	}
	if b.race || a.race {
		rows = append(rows, compareCount("Races", b.races, a.races, b.iterations, a.iterations, false))
	}
	rows = append(rows,
		compareCount("Timeouts", b.timeouts, a.timeouts, b.iterations, a.iterations, false),
		compareCount("Slow tests", b.slow, a.slow, b.iterations, a.iterations, false),
		compareFraction("Flaky iterations", b.flakeFailIters, b.flakeIters, a.flakeFailIters, a.flakeIters, false),
		compareFraction("Passing iterations", b.passIters, b.completed, a.passIters, a.completed, true),
		compareDuration("Iteration wall time p50", b.p50, a.p50),
		compareDuration("Iteration wall time max", b.max, a.max),
	)
	return rows
}

// compareCount renders a count row. When iteration counts differ, counts are
// shown with a per-iteration rate and compared as rates.
func compareCount(label string, before, after, beforeIters, afterIters int, neutral bool) row {
	if beforeIters > 0 && afterIters > 0 && beforeIters != afterIters {
		beforeRate := percent(before, beforeIters)
		afterRate := percent(after, afterIters)
		return row{
			label:  label,
			before: fmt.Sprintf("%d (%.1f%%)", before, beforeRate),
			after:  fmt.Sprintf("%d (%.1f%%)", after, afterRate),
			change: ppChange(beforeRate, afterRate, neutral, false),
		}
	}
	delta := after - before
	return row{
		label:  label,
		before: strconv.Itoa(before),
		after:  strconv.Itoa(after),
		change: countChange(delta, neutral, false),
	}
}

func compareFraction(label string, beforeN, beforeD, afterN, afterD int, lowerBetter bool) row {
	r := row{label: label, before: fraction(beforeN, beforeD), after: fraction(afterN, afterD)}
	if beforeD <= 0 || afterD <= 0 {
		return r
	}
	r.change = ppChange(percent(beforeN, beforeD), percent(afterN, afterD), false, lowerBetter)
	return r
}

func compareDuration(label string, before, after time.Duration) row {
	r := row{label: label, before: durationOrDash(before), after: durationOrDash(after)}
	if before > 0 && after > 0 {
		r.change = durationChange(before, after)
	}
	return r
}

func percent(n, d int) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) / float64(d) * 100
}

func fraction(n, d int) string {
	if d <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d/%d (%.1f%%)", n, d, percent(n, d))
}

func countChange(delta int, neutral, lowerBetter bool) string {
	if delta == 0 {
		return "0"
	}
	return fmt.Sprintf("%+d", delta) + changeIcon(float64(delta), neutral, lowerBetter)
}

func ppChange(before, after float64, neutral, lowerBetter bool) string {
	delta := after - before
	if delta == 0 {
		return ""
	}
	return fmt.Sprintf("%+.1f pp", delta) + changeIcon(delta, neutral, lowerBetter)
}

func changeIcon(delta float64, neutral, lowerBetter bool) string {
	if neutral {
		return ""
	}
	improved := delta < 0
	if !lowerBetter {
		improved = delta > 0
	}
	if improved {
		return " ✅"
	}
	return " ❌"
}

func durationChange(before, after time.Duration) string {
	delta := after - before
	if delta == 0 {
		return "0"
	}
	change := signedDuration(delta)
	if before > 0 {
		change += fmt.Sprintf(" (%+.0f%%)", float64(delta)/float64(before)*100)
	}
	return change + changeIcon(float64(delta), false, true)
}

func formatDuration(d time.Duration) string {
	return d.Round(time.Millisecond).String()
}

func signedDuration(d time.Duration) string {
	if d < 0 {
		return "-" + (-d).Round(time.Millisecond).String()
	}
	return "+" + d.Round(time.Millisecond).String()
}

func diffProblemTests(before, after *runner.Report) []testSection {
	beforeCats := categories(before)
	afterCats := categories(after)
	keys := make([]testKey, 0, len(beforeCats)+len(afterCats))
	seen := make(map[testKey]struct{}, len(beforeCats)+len(afterCats))
	for _, cats := range []map[testKey]testCategory{beforeCats, afterCats} {
		for k := range cats {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].pkg != keys[j].pkg {
			return keys[i].pkg < keys[j].pkg
		}
		return keys[i].test < keys[j].test
	})

	byVerdict := make(map[verdict][]testRow)
	for _, k := range keys {
		b, bok := beforeCats[k]
		a, aok := afterCats[k]
		v := verdictFor(b, bok, a, aok)
		byVerdict[v] = append(byVerdict[v], testRow{
			test:    displayTest(k),
			pkg:     k.pkg,
			before:  categoryLabel(b, bok),
			after:   categoryLabel(a, aok),
			rate:    rateArrow(b, bok, a, aok),
			verdict: v,
		})
	}
	var sections []testSection
	for _, v := range verdictOrder {
		if rows := byVerdict[v]; len(rows) > 0 {
			sections = append(sections, testSection{title: v.label(), rows: rows})
		}
	}
	return sections
}

func categories(rep *runner.Report) map[testKey]testCategory {
	out := make(map[testKey]testCategory)
	if rep == nil {
		return out
	}
	for _, group := range rep.TestGroups() {
		for _, entry := range *group.Entries {
			k := testKey{pkg: entry.Package, test: entry.Test}
			if _, ok := out[k]; ok {
				continue
			}
			out[k] = testCategory{label: group.CSVCategory, entry: entry}
		}
	}
	return out
}

func displayTest(k testKey) string {
	if k.test == "" {
		return "(package)"
	}
	return k.test
}

func categoryLabel(c testCategory, ok bool) string {
	if !ok || c.label == "" {
		return "ok"
	}
	return c.label
}

func rateArrow(before testCategory, bok bool, after testCategory, aok bool) string {
	return rateText(before, bok) + " → " + rateText(after, aok)
}

func rateText(c testCategory, ok bool) string {
	if !ok {
		return "—"
	}
	rate, lo, hi, ok := failRate(c.entry)
	if !ok {
		return "—"
	}
	return fmt.Sprintf("%.1f%% [%.1f–%.1f%%]", rate*100, lo*100, hi*100)
}

// failRate returns the fail rate and its 95% CI, preferring bounds stored on the
// entry and falling back to WilsonScoreInterval.
func failRate(e runner.TestEntry) (rate, lower, upper float64, ok bool) {
	runs := e.Runs
	if runs < 1 {
		runs = e.Successes + e.Fails
	}
	if runs < 1 {
		return 0, 0, 0, false
	}
	lower, upper = runner.WilsonScoreInterval(e.Fails, runs, 0)
	if e.FailRateLower != nil && e.FailRateUpper != nil {
		lower, upper = *e.FailRateLower, *e.FailRateUpper
	}
	return float64(e.Fails) / float64(runs), lower, upper, true
}

func verdictFor(before testCategory, beforeOK bool, after testCategory, afterOK bool) verdict {
	if !beforeOK {
		return verdictNew
	}
	if !afterOK {
		return verdictFixed
	}
	_, beforeLower, beforeUpper, beforeRateOK := failRate(before.entry)
	_, afterLower, afterUpper, afterRateOK := failRate(after.entry)
	if !beforeRateOK || !afterRateOK {
		return verdictNoChange
	}
	switch {
	case afterLower > beforeUpper:
		return verdictWorse
	case afterUpper < beforeLower:
		return verdictBetter
	default:
		return verdictNoChange
	}
}

func diffTimings(
	before, after *runner.Report,
	entries func(*runner.Report) []runner.TestEntry,
) []timingRow {
	beforeMap := entryMap(entries(before))
	afterMap := entryMap(entries(after))
	keys := make([]testKey, 0, len(beforeMap)+len(afterMap))
	seen := make(map[testKey]struct{}, len(beforeMap)+len(afterMap))
	for _, m := range []map[testKey]runner.TestEntry{beforeMap, afterMap} {
		for k := range m {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].pkg != keys[j].pkg {
			return keys[i].pkg < keys[j].pkg
		}
		return keys[i].test < keys[j].test
	})

	rows := make([]timingRow, 0, len(keys))
	for _, k := range keys {
		b, bok := beforeMap[k]
		a, aok := afterMap[k]
		r := timingRow{name: displayTest(k), pkg: k.pkg}
		r.beforeP50, r.afterP50, r.p50Change = durationCells(b, bok, a, aok, func(e runner.TestEntry) time.Duration {
			return e.P50Elapsed
		})
		r.beforeMax, r.afterMax, r.maxChange = durationCells(b, bok, a, aok, func(e runner.TestEntry) time.Duration {
			return e.MaxElapsed
		})
		rows = append(rows, r)
	}
	return rows
}

func entryMap(entries []runner.TestEntry) map[testKey]runner.TestEntry {
	out := make(map[testKey]runner.TestEntry, len(entries))
	for _, e := range entries {
		out[testKey{pkg: e.Package, test: e.Test}] = e
	}
	return out
}

func durationCells(
	before runner.TestEntry,
	beforeOK bool,
	after runner.TestEntry,
	afterOK bool,
	get func(runner.TestEntry) time.Duration,
) (string, string, string) {
	beforeText, afterText, change := "—", "—", "—"
	if beforeOK {
		beforeText = formatDuration(get(before))
	}
	if afterOK {
		afterText = formatDuration(get(after))
	}
	if beforeOK && afterOK {
		change = durationChange(get(before), get(after))
	}
	return beforeText, afterText, change
}
