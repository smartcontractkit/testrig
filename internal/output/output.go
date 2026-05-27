// Package output centralizes harness-owned terminal writes: human-rich vs sparse
// (--ai-output), and whether stderr supports carriage-return progress (TTY).
package output

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/x/term"

	"github.com/smartcontractkit/testrig/internal/config"
)

// Printer writes CLI messages for tools/test. Child processes (go test) still
// attach os.Stdout/os.Stderr directly where passthrough is intended.
type Printer struct {
	aiOutput   bool
	stdout     io.Writer
	stderr     io.Writer
	liveInline bool // human mode and stderr is a TTY (safe for \r progress)
}

// New builds a production Printer. liveInline is enabled when stderrFD points
// at a real terminal and ai-output is off. Tests should use NewForTest.
func New(aiOutput bool, stdout, stderr io.Writer, stderrFD uintptr) *Printer {
	live := !aiOutput && term.IsTerminal(stderrFD)
	return newPrinter(aiOutput, stdout, stderr, live)
}

// NewForTest returns a Printer with explicit live-inline behavior. liveInline
// is ignored when aiOutput is true.
func NewForTest(aiOutput bool, stdout, stderr io.Writer, liveInline bool) *Printer {
	return newPrinter(aiOutput, stdout, stderr, liveInline && !aiOutput)
}

func newPrinter(aiOutput bool, stdout, stderr io.Writer, liveInline bool) *Printer {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return &Printer{
		aiOutput:   aiOutput,
		stdout:     stdout,
		stderr:     stderr,
		liveInline: liveInline,
	}
}

// NewFromApp uses os.Stdout/os.Stderr and stderr's TTY bit from conf.AIOutput.
func NewFromApp(conf *config.App) *Printer {
	return New(conf.AIOutput, os.Stdout, os.Stderr, os.Stderr.Fd())
}

// AIOutput reports sparse / agent-oriented mode.
func (p *Printer) AIOutput() bool {
	return p.aiOutput
}

// LiveInlineProgress is true when human mode may use carriage-return redraws on stderr.
func (p *Printer) LiveInlineProgress() bool {
	return p.liveInline
}

// HumanStderr writes a line to stderr when in human mode.
func (p *Printer) HumanStderr(a ...any) {
	if p.aiOutput {
		return
	}
	_, _ = fmt.Fprintln(p.stderr, a...)
}

// HumanStderrWriter returns stderr in human mode, io.Discard in AI mode.
// Use this for helpers that need an io.Writer (e.g. lipgloss tables, hint lines).
func (p *Printer) HumanStderrWriter() io.Writer {
	if p.aiOutput {
		return io.Discard
	}
	return p.stderr
}

// SparseStdoutln prints one line to stdout in AI mode (machine-oriented).
func (p *Printer) SparseStdoutln(a ...any) {
	if !p.aiOutput {
		return
	}
	_, _ = fmt.Fprintln(p.stdout, a...)
}

// Stderrf always writes formatted text to stderr (errors, diagnostics).
func (p *Printer) Stderrf(format string, a ...any) {
	_, _ = fmt.Fprintf(p.stderr, format, a...)
}

// Stdoutln always writes a line to stdout.
func (p *Printer) Stdoutln(a ...any) {
	_, _ = fmt.Fprintln(p.stdout, a...)
}

// ClearInline clears the current stderr line when live inline progress is active.
func (p *Printer) ClearInline() {
	if !p.liveInline {
		return
	}
	_, _ = fmt.Fprint(p.stderr, "\r\033[K")
}
