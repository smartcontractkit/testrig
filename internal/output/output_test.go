package output

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/config"
)

func TestNew_liveInline_requiresTTYAndHuman(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	// Builder is not a TTY — human mode but no live inline.
	p := NewForTest(false, &b, &b, false)
	require.False(t, p.LiveInlineProgress())

	// AI mode never enables live inline.
	pAI := NewForTest(true, &b, &b, true)
	require.False(t, pAI.LiveInlineProgress())
}

func TestNewForTest_liveInline(t *testing.T) {
	t.Parallel()
	var stderr strings.Builder
	p := NewForTest(false, io.Discard, &stderr, true)
	require.True(t, p.LiveInlineProgress())
	p.inlineLastLines = 1
	p.ClearInline()
	require.Equal(t, "\r\x1b[2K", stderr.String())

	var err2 strings.Builder
	pAI := NewForTest(true, io.Discard, &err2, true)
	require.False(t, pAI.LiveInlineProgress())
}

func TestSparseStdoutln_onlyWhenAI(t *testing.T) {
	t.Parallel()
	var out, err strings.Builder
	p := NewForTest(false, &out, &err, false)
	p.SparseStdoutln("x")
	require.Empty(t, out.String())

	pAI := NewForTest(true, &out, &err, false)
	pAI.SparseStdoutln("path")
	require.Contains(t, out.String(), "path")
}

func TestStderrf_always(t *testing.T) {
	t.Parallel()
	var out, err strings.Builder
	p := NewForTest(true, &out, &err, false)
	p.Stderrf("err: %d\n", 1)
	require.Contains(t, err.String(), "err: 1")
}

func TestHumanStderrWriter_discardWhenAI(t *testing.T) {
	t.Parallel()
	var out, err strings.Builder
	p := NewForTest(true, &out, &err, false)
	w := p.HumanStderrWriter()
	n, e := w.Write([]byte("note\n"))
	require.NoError(t, e)
	require.Equal(t, 5, n)
	require.Empty(t, err.String())
}

func TestNewFromApp(t *testing.T) {
	t.Parallel()
	p := NewFromApp(&config.App{AIOutput: true})
	require.True(t, p.AIOutput())
}

func TestNew_nilWriters(t *testing.T) {
	t.Parallel()
	require.NotPanics(t, func() {
		p := NewForTest(false, nil, nil, false)
		p.HumanStderr("x")
		p.Stderrf("y")
	})
}
