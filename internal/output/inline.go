package output

import (
	"fmt"
	"io"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
)

// eraseInlineLines clears n physical terminal lines ending at the cursor (no trailing newline).
// The cursor is assumed to be at the end of the bottom line of the block.
func eraseInlineLines(w io.Writer, lines int) {
	if lines <= 0 {
		return
	}
	if lines > 1 {
		_, _ = fmt.Fprintf(w, "\033[%dA", lines-1)
	}
	for i := range lines {
		_, _ = fmt.Fprint(w, "\r\033[2K")
		if i < lines-1 {
			_, _ = fmt.Fprint(w, "\033[1B")
		}
	}
}

func inlineVisualLines(line string, cols int) int {
	if cols < 1 {
		cols = 80
	}
	w := ansi.StringWidth(line)
	if w == 0 {
		return 0
	}
	return (w + cols - 1) / cols
}

func (p *Printer) termColumns() int {
	if p.testTermColumns > 0 {
		return p.testTermColumns
	}
	if p.stderrFD == 0 {
		return 80
	}
	cols, _, err := term.GetSize(p.stderrFD)
	if err != nil || cols < 1 {
		return 80
	}
	return cols
}

// inlineEraseLineCount is how many physical rows to clear before redraw.
// Uses max(tracked lines, lines occupied at the width of the last draw) so a
// terminal resize between ticks still clears the old wrap layout.
func (p *Printer) inlineEraseLineCount() int {
	n := p.inlineLastLines
	if p.inlineLastLine != "" && p.inlineLastCols > 0 {
		if atLast := inlineVisualLines(p.inlineLastLine, p.inlineLastCols); atLast > n {
			n = atLast
		}
	}
	return n
}

// TermColumns returns stderr width for progress fitting (defaults to 80 when unknown).
func (p *Printer) TermColumns() int {
	return p.termColumns()
}

// RedrawInline replaces the live progress block on stderr when live inline mode is active.
// The line should already be fitted to the terminal width by the caller.
func (p *Printer) RedrawInline(line string) {
	if !p.liveInline {
		return
	}
	cols := p.termColumns()
	eraseInlineLines(p.stderr, p.inlineEraseLineCount())
	_, _ = fmt.Fprint(p.stderr, "\r\033[2K", line)
	p.inlineLastLine = line
	p.inlineLastCols = cols
	p.inlineLastLines = inlineVisualLines(line, cols)
}
