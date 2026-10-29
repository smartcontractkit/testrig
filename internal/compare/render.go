package compare

import (
	"fmt"
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
	renderTimings(&b, "Slow tests", false, doc.slowRows)
	renderTimings(&b, "Slowest packages", true, doc.packageRows)
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
		rows = append(rows, []string{r.label, r.before, r.after, r.change})
	}
	writeTable(b, []string{"Metric", doc.beforeLabel, doc.afterLabel, "Change"}, rows)
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

func renderTimings(b *strings.Builder, title string, packages bool, rows []timingRow) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", title)
	tableRows := make([][]string, 0, len(rows))
	if packages {
		for _, r := range rows {
			tableRows = append(tableRows, []string{
				r.pkg, r.beforeP50, r.afterP50, r.p50Change, r.beforeMax, r.afterMax, r.maxChange,
			})
		}
		writeTable(
			b,
			[]string{"Package", "p50 before", "p50 after", "p50 Δ", "Max before", "Max after", "Max Δ"},
			tableRows,
		)
		return
	}
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
