package runner

import "github.com/smartcontractkit/testrig/internal/gobuild"

// BuildMetrics is the report shape for compile and link overhead. The fields
// live in gobuild so the build package owns their meaning.
type BuildMetrics = gobuild.BuildMetrics

const (
	buildModePrebuilt = gobuild.ModePrebuilt
	buildModeGoTest   = gobuild.ModeGoTest
)
