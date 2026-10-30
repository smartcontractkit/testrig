package compare

import (
	"fmt"
	"strconv"
	"strings"
)

func render(doc comparison) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# testrig compare: %s → %s\n\n", plain(doc.beforeLabel), plain(doc.afterLabel))
	for _, warning := range doc.warnings {
		fmt.Fprintf(&b, "> [!WARNING]\n> %s\n\n", warning)
	}
	renderRuns(&b, doc)
	renderSummary(&b, doc)
	renderProblemTests(&b, doc)
	renderPackageBreakdown(&b, doc)
	renderTimings(&b, "Slow tests", doc.slowRows)
	renderFooter(&b, doc)
	return b.String()
}

func renderRuns(b *strings.Builder, doc comparison) {
	b.WriteString("## Runs\n\n")
	rows := make([][]string, 0, len(doc.runs))
	for _, r := range doc.runs {
		rows = append(rows, []string{r.label, r.before, r.after})
	}
	writeTable(b, []string{"", doc.beforeLabel, doc.afterLabel}, rows)
}

func renderSummary(b *strings.Builder, doc comparison) {
	b.WriteString("## Summary\n\n")
	rows := make([][]string, 0, len(doc.summary))
	for _, r := range doc.summary {
		evidence := r.evidence
		if evidence == "" {
			evidence = "—"
		}
		rows = append(rows, []string{r.label, r.before, r.after, r.change, evidence})
	}
	writeTable(b, []string{"Metric", doc.beforeLabel, doc.afterLabel, "Change", "Evidence"}, rows)
}

func renderProblemTests(b *strings.Builder, doc comparison) {
	b.WriteString("## Problem tests\n\n")
	if len(doc.testSections) == 0 {
		b.WriteString("_No problem tests in either run._\n\n")
		return
	}
	headers := []string{"Test", "Package", "Before", "After", "Fail rate before → after (95% CI)", "Verdict"}
	for _, section := range doc.testSections {
		fmt.Fprintf(b, "### %s\n\n", section.title)
		rows := make([][]string, 0, len(section.rows))
		for _, r := range section.rows {
			rows = append(rows, []string{r.test, r.pkg, r.before, r.after, r.rate, r.verdict.label()})
		}
		writeTable(b, headers, rows)
	}
}

func renderPackageBreakdown(b *strings.Builder, doc comparison) {
	b.WriteString("## Package breakdown\n\n")
	renderPackageRaces(b, doc)
	renderPackageFlakes(b, doc)
	renderPackageSpeed(b, doc)
}

func renderPackageRaces(b *strings.Builder, doc comparison) {
	if !doc.racesCompared {
		return
	}
	b.WriteString("### Races by package\n\n")
	if len(doc.raceRows) == 0 && len(doc.raceUnchangedRows) == 0 {
		b.WriteString("_No races detected in either run._\n\n")
		return
	}
	headers := []string{
		"Package",
		"Racing tests before",
		"Racing tests after",
		"Race failures before",
		"Race failures after",
		"Change",
	}
	if len(doc.raceRows) > 0 {
		writePackageProblemTable(b, headers, doc.raceRows)
	}
	if len(doc.raceUnchangedRows) > 0 {
		writeDetails(
			b,
			fmt.Sprintf("Packages with no race change (%d)", len(doc.raceUnchangedRows)),
			func() { writePackageProblemTable(b, headers, doc.raceUnchangedRows) },
		)
	}
}

func renderPackageFlakes(b *strings.Builder, doc comparison) {
	b.WriteString("### Flakes by package\n\n")
	if len(doc.flakeRows) == 0 && len(doc.flakeUnchangedRows) == 0 {
		b.WriteString("_No flaky tests in either run._\n\n")
		return
	}
	headers := []string{
		"Package",
		"Flaky tests before",
		"Flaky tests after",
		"Flake failures before",
		"Flake failures after",
		"Change",
	}
	if len(doc.flakeRows) > 0 {
		writePackageProblemTable(b, headers, doc.flakeRows)
	}
	if len(doc.flakeUnchangedRows) > 0 {
		writeDetails(
			b,
			fmt.Sprintf("Packages with no flake change (%d)", len(doc.flakeUnchangedRows)),
			func() { writePackageProblemTable(b, headers, doc.flakeUnchangedRows) },
		)
	}
}

func writePackageProblemTable(b *strings.Builder, headers []string, rows []packageProblemRow) {
	tableRows := make([][]string, 0, len(rows))
	for _, r := range rows {
		tableRows = append(tableRows, []string{
			r.pkg,
			strconv.Itoa(r.beforeTests),
			strconv.Itoa(r.afterTests),
			strconv.Itoa(r.beforeFails),
			strconv.Itoa(r.afterFails),
			r.change,
		})
	}
	writeTable(b, headers, tableRows)
}

func renderPackageSpeed(b *strings.Builder, doc comparison) {
	b.WriteString("### Speed by package\n\n")
	if len(doc.packageSpeedRows) == 0 && len(doc.packageSpeedSameRows) == 0 {
		b.WriteString("_No package exceeded the slow threshold in either run._\n\n")
		return
	}
	headers := []string{"Package", "p50 before", "p50 after", "p50 Δ", "Max before", "Max after", "Max Δ"}
	if len(doc.packageSpeedRows) > 0 {
		writePackageSpeedTable(b, headers, doc.packageSpeedRows)
	}
	if len(doc.packageSpeedSameRows) > 0 {
		writeDetails(
			b,
			fmt.Sprintf("Packages with little or no speed change (%d)", len(doc.packageSpeedSameRows)),
			func() { writePackageSpeedTable(b, headers, doc.packageSpeedSameRows) },
		)
	}
}

func writePackageSpeedTable(b *strings.Builder, headers []string, rows []timingRow) {
	tableRows := make([][]string, 0, len(rows))
	for _, r := range rows {
		tableRows = append(tableRows, []string{
			r.pkg, r.beforeP50, r.afterP50, r.p50Change, r.beforeMax, r.afterMax, r.maxChange,
		})
	}
	writeTable(b, headers, tableRows)
}

func renderTimings(b *strings.Builder, title string, rows []timingRow) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", title)
	tableRows := make([][]string, 0, len(rows))
	for _, r := range rows {
		tableRows = append(tableRows, []string{
			r.name, r.pkg, r.beforeP50, r.afterP50, r.p50Change, r.beforeMax, r.afterMax, r.maxChange,
		})
	}
	writeTable(
		b,
		[]string{"Test", "Package", "p50 before", "p50 after", "p50 Δ", "Max before", "Max after", "Max Δ"},
		tableRows,
	)
}

func renderFooter(b *strings.Builder, doc comparison) {
	b.WriteString("---\n\n")
	fmt.Fprintf(b, "Before: %s\n\n", mdCode(doc.beforePath))
	fmt.Fprintf(b, "After: %s\n", mdCode(doc.afterPath))
}

// writeDetails renders a collapsible GFM section. GitHub and GitLab both
// collapse <details> by default, keeping long "no change" tables out of the way.
func writeDetails(b *strings.Builder, summary string, body func()) {
	fmt.Fprintf(b, "<details>\n<summary>%s</summary>\n\n", mdCell(summary))
	body()
	b.WriteString("</details>\n\n")
}

func writeTable(b *strings.Builder, headers []string, rows [][]string) {
	b.WriteString("|")
	for _, h := range headers {
		b.WriteString(" " + mdCell(h) + " |")
	}
	b.WriteString("\n|")
	for range headers {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")
	for _, r := range rows {
		b.WriteString("|")
		for _, c := range r {
			b.WriteString(" " + mdCell(c) + " |")
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

// mdCell escapes text for a GFM table cell. Subtests can contain "|", which
// would otherwise split the row.
func mdCell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	return plain(s)
}

func plain(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.ReplaceAll(s, "\r", " ")
}

func mdCode(s string) string {
	s = plain(s)
	if s == "" {
		return "—"
	}
	if strings.Contains(s, "`") {
		return "`` " + s + " ``"
	}
	return "`" + s + "`"
}
