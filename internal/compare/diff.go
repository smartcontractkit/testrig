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
	label    string
	before   string
	after    string
	change   string
	evidence string
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
	verdictUnflagged
)

var verdictOrder = []verdict{verdictNew, verdictWorse, verdictNoChange, verdictBetter, verdictUnflagged}

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
	case verdictUnflagged:
		return "No longer flagged"
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
	sortDelta time.Duration // p50 after − before; used to order package speed rows
}

type packageProblemRow struct {
	pkg         string
	beforeTests int
	afterTests  int
	beforeFails int
	afterFails  int
	change      string
}

type comparison struct {
	beforeLabel          string
	afterLabel           string
	beforePath           string
	afterPath            string
	warnings             []string
	runs                 []row
	summary              []row
	testSections         []testSection
	racesCompared        bool
	raceRows             []packageProblemRow
	raceUnchangedRows    []packageProblemRow
	flakeRows            []packageProblemRow
	flakeUnchangedRows   []packageProblemRow
	packageSpeedRows     []timingRow
	packageSpeedSameRows []timingRow
	slowRows             []timingRow
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
	// Race counts only make sense when both runs enabled -race. Comparing a race
	// run to a non-race run would always show races "fixed".
	doc.racesCompared = runMetaOf(before.Report).Race && runMetaOf(after.Report).Race
	doc.testSections = diffProblemTests(before.Report, after.Report, doc.racesCompared)
	if doc.racesCompared {
		doc.raceRows, doc.raceUnchangedRows = diffPackageProblems(
			raceEntries(before.Report),
			raceEntries(after.Report),
		)
	}
	doc.flakeRows, doc.flakeUnchangedRows = diffPackageProblems(
		flakeEntries(before.Report),
		flakeEntries(after.Report),
	)
	doc.packageSpeedRows, doc.packageSpeedSameRows = diffPackageSpeed(before.Report, after.Report)
	doc.slowRows = diffTimings(before.Report, after.Report, slowEntries)
	return doc
}

func raceEntries(rep *runner.Report) []runner.TestEntry {
	if rep == nil {
		return nil
	}
	return rep.Races
}

func flakeEntries(rep *runner.Report) []runner.TestEntry {
	if rep == nil {
		return nil
	}
	return rep.Flakes
}

func slowEntries(rep *runner.Report) []runner.TestEntry {
	if rep == nil {
		return nil
	}
	return rep.Slow
}

func slowestPackages(rep *runner.Report) []runner.TestEntry {
	if rep == nil {
		return nil
	}
	return rep.SlowestPackages
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
	if b.DiagnoseIterations != a.DiagnoseIterations {
		reasons = append(reasons, "iteration counts differ")
	}
	if b.Race != a.Race {
		reasons = append(reasons, "only one run used -race")
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
	testsRun       int
	hasSummary     bool
	broken         int
	flaky          int
	races          int
	timeouts       int
	slow           int
	flakeFailIters int
	flakeIters     int
	durations      []time.Duration
	p50            time.Duration
	max            time.Duration
	race           bool
}

func statsOf(rep *runner.Report) repStats {
	if rep == nil {
		return repStats{}
	}
	stats := repStats{
		broken:   rep.BrokenNamedCount(),
		timeouts: namedEntryCount(rep.Timeouts),
	}
	if rep.Summary != nil {
		stats.hasSummary = true
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
		if it.Duration > 0 {
			stats.durations = append(stats.durations, it.Duration)
		}
	}
	if stats.flakeIters == 0 {
		stats.flakeIters = rep.Iterations
	}
	if rep.Run != nil {
		stats.race = rep.Run.Race
	}
	return stats
}

// namedEntryCount counts entries that name a test. report.json also carries
// package-level entries (Test == "") for build and binary failures, which are
// outside the distinct-named-test denominator.
func namedEntryCount(entries []runner.TestEntry) int {
	n := 0
	for _, e := range entries {
		if e.Test != "" {
			n++
		}
	}
	return n
}

func diffSummaryRows(before, after *runner.Report) []row {
	b, a := statsOf(before), statsOf(after)
	rows := []row{
		testsRunRow(b, a),
		withEvidence(
			problemCountRow("Broken tests", b.broken, a.broken),
			proportionEvidence(b.broken, b.testsRun, a.broken, a.testsRun),
		),
		withEvidence(
			problemCountRow("Flaky tests", b.flaky, a.flaky),
			proportionEvidence(b.flaky, b.testsRun, a.flaky, a.testsRun),
		),
	}
	if b.race || a.race {
		rows = append(rows, racesRow(b, a))
	}
	rows = append(rows,
		withEvidence(
			problemCountRow("Timeouts", b.timeouts, a.timeouts),
			proportionEvidence(b.timeouts, b.testsRun, a.timeouts, a.testsRun),
		),
		withEvidence(
			problemCountRow("Slow tests", b.slow, a.slow),
			proportionEvidence(b.slow, b.testsRun, a.slow, a.testsRun),
		),
		withEvidence(
			compareFraction("Flaky iterations", b.flakeFailIters, b.flakeIters, a.flakeFailIters, a.flakeIters, true),
			proportionEvidence(b.flakeFailIters, b.flakeIters, a.flakeFailIters, a.flakeIters),
		),
		withEvidence(
			compareDuration("Iteration wall time p50", b.p50, a.p50),
			durationEvidence(b.durations, a.durations),
		),
		compareDuration("Iteration wall time max", b.max, a.max),
	)
	return rows
}

func testsRunRow(b, a repStats) row {
	r := row{label: "Tests run", before: testsRunCell(b), after: testsRunCell(a)}
	if b.hasSummary && a.hasSummary {
		r.change = strconv.Itoa(a.testsRun - b.testsRun)
	}
	return r
}

func testsRunCell(s repStats) string {
	if !s.hasSummary {
		return "—"
	}
	return strconv.Itoa(s.testsRun)
}

// racesRow compares race counts. A direction is only meaningful when both runs
// used -race; otherwise the run that did not measure races shows a dash.
func racesRow(b, a repStats) row {
	if !b.race || !a.race {
		return row{
			label:  "Races",
			before: countIfMeasured(b.races, b.race),
			after:  countIfMeasured(a.races, a.race),
		}
	}
	return withEvidence(
		problemCountRow("Races", b.races, a.races),
		proportionEvidence(b.races, b.testsRun, a.races, a.testsRun),
	)
}

func countIfMeasured(n int, measured bool) string {
	if !measured {
		return "—"
	}
	return strconv.Itoa(n)
}

func withEvidence(r row, evidence string) row {
	// No change means statistical evidence is moot; a dash reads better than
	// "unclear" next to "0".
	if r.change == "" || r.change == "0" {
		r.evidence = "—"
		return r
	}
	r.evidence = evidence
	return r
}

func proportionEvidence(beforeN, beforeD, afterN, afterD int) string {
	if beforeD <= 0 || afterD <= 0 ||
		beforeN < 0 || afterN < 0 || beforeN > beforeD || afterN > afterD {
		return "—"
	}
	return formatEvidence(fisherExact(beforeN, beforeD-beforeN, afterN, afterD-afterN))
}

func durationEvidence(before, after []time.Duration) string {
	p, ok := mannWhitneyP(before, after)
	if !ok {
		return "—"
	}
	return formatEvidence(p)
}

// problemCountRow renders a count where fewer is better. Counts are distinct
// flagged tests, which do not grow with iteration count, so they stay raw; the
// iteration-count warning from diffWarnings covers runs of different lengths.
func problemCountRow(label string, before, after int) row {
	return row{
		label:  label,
		before: strconv.Itoa(before),
		after:  strconv.Itoa(after),
		change: countChange(after - before),
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

func countChange(delta int) string {
	if delta == 0 {
		return "0"
	}
	return fmt.Sprintf("%+d", delta) + changeIcon(float64(delta), false, true)
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

func stripChangeIcon(s string) string {
	s = strings.TrimSuffix(s, " ✅")
	return strings.TrimSuffix(s, " ❌")
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

func diffProblemTests(before, after *runner.Report, racesCompared bool) []testSection {
	beforeCats := categories(before, racesCompared)
	afterCats := categories(after, racesCompared)
	keys := unionKeys(beforeCats, afterCats)
	sortKeys(keys)

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

func categories(rep *runner.Report, includeRaces bool) map[testKey]testCategory {
	out := make(map[testKey]testCategory)
	if rep == nil {
		return out
	}
	for _, group := range rep.TestGroups() {
		if !includeRaces && group.CSVCategory == "race" {
			continue
		}
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
		return verdictUnflagged
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
	keys := unionKeys(beforeMap, afterMap)
	sortKeys(keys)

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

// Speed changes smaller than this (relative and absolute) go in the collapsed
// "little or no change" table instead of the main one.
const (
	speedChangePct = 0.10
	speedChangeMin = 10 * time.Millisecond
)

func diffPackageSpeed(before, after *runner.Report) (changed, unchanged []timingRow) {
	beforeMap := entryMap(slowestPackages(before))
	afterMap := entryMap(slowestPackages(after))
	keys := unionKeys(beforeMap, afterMap)

	for _, k := range keys {
		b, bok := beforeMap[k]
		a, aok := afterMap[k]
		r := timingRow{name: k.pkg, pkg: k.pkg}
		r.beforeP50, r.afterP50, r.p50Change = durationCells(b, bok, a, aok, func(e runner.TestEntry) time.Duration {
			return e.P50Elapsed
		})
		r.beforeMax, r.afterMax, r.maxChange = durationCells(b, bok, a, aok, func(e runner.TestEntry) time.Duration {
			return e.MaxElapsed
		})
		r.sortDelta = a.P50Elapsed - b.P50Elapsed
		switch {
		case !bok:
			r.sortDelta = 1 << 62 // new slow package: show first
		case !aok:
			r.sortDelta = -(1 << 62) // no longer slow: show last
		}
		if speedChanged(b, bok, a, aok) {
			changed = append(changed, r)
		} else {
			// These rows are collapsed; a ✅/❌ on a 1% change is just noise.
			r.p50Change = stripChangeIcon(r.p50Change)
			r.maxChange = stripChangeIcon(r.maxChange)
			unchanged = append(unchanged, r)
		}
	}

	sort.SliceStable(changed, func(i, j int) bool {
		if changed[i].sortDelta != changed[j].sortDelta {
			return changed[i].sortDelta > changed[j].sortDelta
		}
		return changed[i].pkg < changed[j].pkg
	})
	sort.SliceStable(unchanged, func(i, j int) bool {
		return unchanged[i].pkg < unchanged[j].pkg
	})
	return changed, unchanged
}

func speedChanged(before runner.TestEntry, beforeOK bool, after runner.TestEntry, afterOK bool) bool {
	if beforeOK != afterOK {
		return true
	}
	return significantSpeedChange(before.P50Elapsed, after.P50Elapsed) ||
		significantSpeedChange(before.MaxElapsed, after.MaxElapsed)
}

func significantSpeedChange(before, after time.Duration) bool {
	delta := after - before
	if delta < 0 {
		delta = -delta
	}
	if delta < speedChangeMin {
		return false
	}
	if before <= 0 {
		return after > 0
	}
	return float64(delta)/float64(before) >= speedChangePct
}

type packageProblemStat struct {
	tests int
	fails int
}

// diffPackageProblems aggregates flagged tests (races or flakes) per package and
// splits rows into meaningful changes and no-change rows.
func diffPackageProblems(before, after []runner.TestEntry) (changed, unchanged []packageProblemRow) {
	beforeStats := problemStatsByPackage(before)
	afterStats := problemStatsByPackage(after)
	pkgs := make(map[string]struct{}, len(beforeStats)+len(afterStats))
	for pkg := range beforeStats {
		pkgs[pkg] = struct{}{}
	}
	for pkg := range afterStats {
		pkgs[pkg] = struct{}{}
	}

	rows := make([]packageProblemRow, 0, len(pkgs))
	for pkg := range pkgs {
		b, a := beforeStats[pkg], afterStats[pkg]
		rows = append(rows, packageProblemRow{
			pkg:         pkg,
			beforeTests: b.tests,
			afterTests:  a.tests,
			beforeFails: b.fails,
			afterFails:  a.fails,
			change:      problemChange(b, a),
		})
	}

	for _, r := range rows {
		if r.change == "" {
			unchanged = append(unchanged, r)
		} else {
			changed = append(changed, r)
		}
	}
	sort.SliceStable(changed, func(i, j int) bool {
		di := changed[i].afterFails - changed[i].beforeFails
		dj := changed[j].afterFails - changed[j].beforeFails
		if di != dj {
			return di > dj
		}
		return changed[i].pkg < changed[j].pkg
	})
	sort.SliceStable(unchanged, func(i, j int) bool {
		return unchanged[i].pkg < unchanged[j].pkg
	})
	return changed, unchanged
}

func problemStatsByPackage(entries []runner.TestEntry) map[string]packageProblemStat {
	out := make(map[string]packageProblemStat)
	for _, e := range entries {
		s := out[e.Package]
		if e.Test != "" {
			s.tests++
		}
		s.fails += e.Fails
		out[e.Package] = s
	}
	return out
}

func problemChange(before, after packageProblemStat) string {
	icon := func(delta int) string { return changeIcon(float64(delta), false, true) }
	if delta := after.fails - before.fails; delta != 0 {
		return fmt.Sprintf("%+d%s%s", delta, plural(" failure", " failures", delta), icon(delta))
	}
	if delta := after.tests - before.tests; delta != 0 {
		return fmt.Sprintf("%+d%s%s", delta, plural(" test", " tests", delta), icon(delta))
	}
	return ""
}

func plural(singular, pluralForm string, n int) string {
	if n == 1 || n == -1 {
		return singular
	}
	return pluralForm
}

func unionKeys[V any](a, b map[testKey]V) []testKey {
	keys := make([]testKey, 0, len(a)+len(b))
	seen := make(map[testKey]struct{}, len(a)+len(b))
	for _, m := range []map[testKey]V{a, b} {
		for k := range m {
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			keys = append(keys, k)
		}
	}
	return keys
}

func sortKeys(keys []testKey) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].pkg != keys[j].pkg {
			return keys[i].pkg < keys[j].pkg
		}
		return keys[i].test < keys[j].test
	})
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
