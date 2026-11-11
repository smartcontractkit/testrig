package gobuild

import (
	"encoding/json"
	"os"
	"time"
)

// Action is one entry in the hidden -debug-actiongraph output.
type Action struct {
	Mode      string    `json:"Mode"`
	Package   string    `json:"Package"`
	Cmd       []string  `json:"Cmd"`
	CmdReal   int64     `json:"CmdReal"`
	TimeStart time.Time `json:"TimeStart"`
	TimeDone  time.Time `json:"TimeDone"`
}

// ActionGraphStats summarizes compile, link, and vet work from an action graph.
type ActionGraphStats struct {
	CompileActions int
	CompileCached  int
	CompileTotal   time.Duration
	Links          int
	LinkTotal      time.Duration
	VetTotal       time.Duration
}

// ParseActionGraph reads a -debug-actiongraph JSON file and summarizes it.
func ParseActionGraph(path string) (ActionGraphStats, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // G304: caller-owned temp path
	if err != nil {
		return ActionGraphStats{}, err
	}
	var actions []Action
	if err := json.Unmarshal(raw, &actions); err != nil {
		return ActionGraphStats{}, err
	}
	return SummarizeActions(actions), nil
}

// SummarizeActions counts the actions that map to user-visible build work.
func SummarizeActions(actions []Action) ActionGraphStats {
	var stats ActionGraphStats
	for _, action := range actions {
		switch action.Mode {
		case "build":
			stats.CompileActions++
			if len(action.Cmd) == 0 {
				stats.CompileCached++
				continue
			}
			stats.CompileTotal += time.Duration(action.CmdReal)
		case "link":
			stats.Links++
			stats.LinkTotal += time.Duration(action.CmdReal)
		case "vet":
			stats.VetTotal += time.Duration(action.CmdReal)
		}
	}
	return stats
}

// CacheState classifies compile cache hits. Warm and cold use the design's 95%
// and 5% boundaries. A graph with no compile actions is unknown.
func CacheState(actions, cached int) string {
	if actions <= 0 {
		return CacheUnknown
	}
	if cached*100 >= actions*95 {
		return CacheWarm
	}
	if cached*100 <= actions*5 {
		return CacheCold
	}
	return CachePartial
}
