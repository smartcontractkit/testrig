// Package termstyle holds lipgloss styles shared by diagnose progress, DB setup,
// and summary output so the CLI reads as one palette.
package termstyle

import "charm.land/lipgloss/v2"

// Colors align with runner/diagnose_progress.go (labels, counts, accents).
var (
	OK     = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // checkmarks / success ticks
	Label  = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	Muted  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	Accent = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	Bad    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	// Flaky summary (yellow); Slow uses Muted (grey) in the runner.
	Flaky = lipgloss.NewStyle().Foreground(lipgloss.Color("226"))
)
