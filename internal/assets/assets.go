// Package assets contains embedded files and templates.
package assets

import "embed"

// SkillsFS provides access to embedded skill templates.
//
//go:embed skills/*
var SkillsFS embed.FS
