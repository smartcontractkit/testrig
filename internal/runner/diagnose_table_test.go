package runner

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/output"
	"github.com/smartcontractkit/testrig/internal/termstyle"
)

var ansiEscapeSeq = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiEscapeSeq.ReplaceAllString(s, "")
}

func TestDiagnoseTableHeaderPlain(t *testing.T) {
	t.Parallel()
	got := diagnoseTableHeaderPlain()
	want := fmt.Sprintf("%5s  %-8s  %8s  %8s  %8s  %8s  %8s  %10s",
		"Iter", "Result", "Tests", "Skipped", "Failures", "Timeouts", "Slow", "Runtime")
	assert.Equal(t, want, got)
	assert.Len(t, got, len(want))
}

func TestPrintDiagnoseIterationTableHeader(t *testing.T) {
	t.Parallel()
	var stderr strings.Builder
	p := output.NewForTest(false, &strings.Builder{}, &stderr, false)
	printDiagnoseIterationTableHeader(p)
	s := strings.TrimRight(stripANSI(stderr.String()), "\n")
	lines := strings.Split(s, "\n")
	require.Len(t, lines, 2)
	assert.Equal(t, diagnoseTableHeaderPlain(), lines[0])
	assert.Equal(t, strings.Repeat("─", len(diagnoseTableHeaderPlain())), lines[1])
}

func TestFormatDiagnoseIterationTableRow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		iter     int
		d        IterationDigest
		dur      time.Duration
		wantSans string // stripped ANSI: digit columns and plain tokens only
	}{
		{
			name: "pass_clean",
			iter: 1,
			d: IterationDigest{
				Result: "pass", RanTests: 0, FailTests: 0, TimeoutTests: 0, SkipTests: 0, SlowTests: 0,
			},
			dur:      3*time.Minute + 7*time.Second,
			wantSans: "    1  pass             0         0         0         0         0        3m7s",
		},
		{
			name: "fail_with_counts",
			iter: 12,
			d: IterationDigest{
				Result: "fail", RanTests: 0, FailTests: 1, TimeoutTests: 2, SkipTests: 3, SlowTests: 5,
			},
			dur:      90 * time.Second,
			wantSans: "   12  fail             0         3         1         2         5       1m30s",
		},
		{
			name: "timeout_result",
			iter: 3,
			d: IterationDigest{
				Result: "timeout", RanTests: 0, FailTests: 0, TimeoutTests: 1, SkipTests: 0, SlowTests: 0,
			},
			dur:      time.Hour,
			wantSans: "    3  timeout          0         0         0         1         0      1h0m0s",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := stripANSI(formatDiagnoseIterationTableRow(tc.iter, tc.d, tc.dur))
			assert.Equal(t, tc.wantSans, got)
		})
	}
}

func TestFormatDiagnoseProblemTestsSuffix(t *testing.T) {
	t.Parallel()
	baseRowANSI := formatDiagnoseIterationTableRow(19, IterationDigest{
		Result: "fail", RanTests: 8657, SkipTests: 103, FailTests: 4, TimeoutTests: 0, SlowTests: 14,
	}, 3*time.Minute)
	baseRow := stripANSI(baseRowANSI)
	cases := []struct {
		name      string
		names     []string
		cols      int
		wantPlain string
	}{
		{
			name:      "empty",
			names:     nil,
			cols:      120,
			wantPlain: "",
		},
		{
			name:      "two_names_wide",
			names:     []string{"Testxxx", "Testyyy"},
			cols:      120,
			wantPlain: " (Testxxx, Testyyy)",
		},
		{
			name:      "four_names_narrow",
			names:     []string{"TestA", "TestB", "TestC", "TestD"},
			cols:      ansi.StringWidth(baseRowANSI) + 24,
			wantPlain: " (TestA, TestB, +2)",
		},
		{
			name:      "long_name_truncated",
			names:     []string{"TestVeryLongNameThatDoesNotFit"},
			cols:      len(baseRow) + 20,
			wantPlain: " (TestVeryLongName…)",
		},
		{
			name:      "others_only_when_tight",
			names:     []string{"TestA", "TestB", "TestC"},
			cols:      len(baseRow) + len(" (+3)"),
			wantPlain: " (+3)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := formatDiagnoseProblemTestsSuffix(tc.names, tc.cols, baseRowANSI, termstyle.Bad.Render)
			if tc.wantPlain == "" {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tc.wantPlain, stripANSI(got))
			for _, name := range tc.names {
				if strings.Contains(tc.wantPlain, name) {
					assert.Contains(t, got, termstyle.Bad.Render(name))
				}
			}
			require.LessOrEqual(t, ansi.StringWidth(baseRowANSI+got), tc.cols)
		})
	}
}

func TestPrintIterationDigestHuman_withFailingTests(t *testing.T) {
	t.Parallel()
	var stderr strings.Builder
	out := output.NewForTest(false, io.Discard, &stderr, false)
	out.SetTermColumnsForTest(120)

	d := IterationDigest{
		Result:       "fail",
		RanTests:     10,
		FailTests:    2,
		FailingTests: []string{"Testxxx", "Testyyy"},
	}
	printIterationDigestHuman(out, 19, d, 3*time.Minute)

	got := stderr.String()
	require.Contains(t, got, termstyle.Bad.Render("Testxxx"))
	require.Contains(t, got, termstyle.Bad.Render("Testyyy"))
	require.Contains(t, stripANSI(got), stripANSI(formatDiagnoseIterationTableRow(19, d, 3*time.Minute)))
}
