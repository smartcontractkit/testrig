package output

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

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
	p.inlineLastLines = 2
	p.RedrawInline("next")
	got := stderr.String()
	require.Contains(t, got, "\x1b[1A")
	require.Contains(t, got, "\r\x1b[Knext")
	require.Equal(t, 1, p.inlineLastLines)
}

func TestRedrawInline_secondRedrawNoExtraNewlines(t *testing.T) {
	t.Parallel()
	var stderr strings.Builder
	p := NewForTest(false, io.Discard, &stderr, true)
	p.RedrawInline("a")
	p.RedrawInline("b")
	require.NotContains(t, stderr.String(), "\n")
}

func TestClearInline_clearsMultipleTrackedLines(t *testing.T) {
	t.Parallel()
	var stderr strings.Builder
	p := NewForTest(false, io.Discard, &stderr, true)
	p.inlineLastLines = 2
	p.ClearInline()
	got := stderr.String()
	require.Contains(t, got, "\x1b[1A")
	require.Equal(t, 0, p.inlineLastLines)
}
