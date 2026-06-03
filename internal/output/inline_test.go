package output

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInlineVisualLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		line string
		cols int
		want int
	}{
		{name: "empty", line: "", cols: 80, want: 0},
		{name: "zero cols defaults", line: "hello", cols: 0, want: 1},
		{name: "single row", line: "12345", cols: 10, want: 1},
		{name: "exact width", line: "1234567890", cols: 10, want: 1},
		{name: "wraps second row", line: "12345678901", cols: 10, want: 2},
		{name: "wide rune", line: "日本語", cols: 4, want: 2},
		{name: "ansi codes width", line: "\x1b[1mhello\x1b[0m", cols: 3, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, inlineVisualLines(tt.line, tt.cols))
		})
	}
}

func TestEraseInlineLines_singleLine(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	eraseInlineLines(&b, 1)
	require.Equal(t, "\r\x1b[2K", b.String())
}

func TestEraseInlineLines_multipleLines(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	eraseInlineLines(&b, 3)
	got := b.String()
	require.Contains(t, got, "\x1b[2A")
	require.Equal(t, 3, strings.Count(got, "\r\x1b[2K"))
}

func TestRedrawInline_erasesPreviousLines(t *testing.T) {
	t.Parallel()
	var stderr strings.Builder
	p := NewForTest(false, io.Discard, &stderr, true)
	p.SetInlineLastLinesForTest(2)
	p.RedrawInline("next")
	got := stderr.String()
	require.Contains(t, got, "\x1b[1A")
	require.Contains(t, got, "\r\x1b[2Knext")
	require.Equal(t, 1, p.InlineLastLinesForTest())
}

func TestRedrawInline_secondRedrawNoExtraNewlines(t *testing.T) {
	t.Parallel()
	var stderr strings.Builder
	p := NewForTest(false, io.Discard, &stderr, true)
	p.RedrawInline("a")
	p.RedrawInline("b")
	require.NotContains(t, stderr.String(), "\n")
}

func TestRedrawInline_wrapsAtTestColumns(t *testing.T) {
	t.Parallel()
	var stderr strings.Builder
	p := NewForTest(false, io.Discard, &stderr, true)
	p.SetTermColumnsForTest(40)
	long := strings.Repeat("x", 100)
	p.RedrawInline(long)
	require.GreaterOrEqual(t, p.InlineLastLinesForTest(), 2)
	got := stderr.String()
	require.GreaterOrEqual(t, strings.Count(got, "\r\x1b[2K"), 1)
}

func TestRedrawInline_terminalResize_erasesOldWrap(t *testing.T) {
	t.Parallel()
	var stderr strings.Builder
	p := NewForTest(false, io.Discard, &stderr, true)
	p.SetTermColumnsForTest(40)
	p.RedrawInline(strings.Repeat("x", 100))
	require.GreaterOrEqual(t, p.InlineLastLinesForTest(), 2)

	p.SetTermColumnsForTest(80)
	p.RedrawInline(strings.Repeat("y", 50))
	// Second draw must clear all rows from the 40-col wrap, not just the new 1-row count.
	require.Contains(t, stderr.String(), "\x1b[2A")
}

func TestClearInline_clearsMultipleTrackedLines(t *testing.T) {
	t.Parallel()
	var stderr strings.Builder
	p := NewForTest(false, io.Discard, &stderr, true)
	p.SetInlineLastLinesForTest(2)
	p.ClearInline()
	got := stderr.String()
	require.Contains(t, got, "\x1b[1A")
	require.Equal(t, 0, p.InlineLastLinesForTest())
}

func TestInlineEraseLineCount_usesLastDrawWidth(t *testing.T) {
	t.Parallel()
	p := NewForTest(false, io.Discard, io.Discard, true)
	p.SetTermColumnsForTest(40)
	p.RedrawInline(strings.Repeat("a", 80))
	p.SetTermColumnsForTest(80)
	p.inlineLastLines = 1 // stale count after resize before next redraw
	require.GreaterOrEqual(t, p.inlineEraseLineCount(), 2)
}
