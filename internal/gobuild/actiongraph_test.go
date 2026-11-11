package gobuild

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseActionGraph(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		file  string
		want  ActionGraphStats
		cache string
	}{
		{
			name: "warm",
			file: "warm.json",
			want: ActionGraphStats{
				CompileActions: 10,
				CompileCached:  10,
				CompileTotal:   0,
				Links:          1,
				LinkTotal:      time.Second,
				VetTotal:       1500 * time.Millisecond,
			},
			cache: CacheWarm,
		},
		{
			name: "cold",
			file: "cold.json",
			want: ActionGraphStats{
				CompileActions: 5,
				CompileCached:  0,
				CompileTotal:   10 * time.Second,
				Links:          1,
				LinkTotal:      3 * time.Second,
				VetTotal:       2 * time.Second,
			},
			cache: CacheCold,
		},
		{
			name: "partial",
			file: "partial.json",
			want: ActionGraphStats{
				CompileActions: 10,
				CompileCached:  5,
				CompileTotal:   5 * time.Second,
				Links:          1,
				LinkTotal:      2 * time.Second,
				VetTotal:       500 * time.Millisecond,
			},
			cache: CachePartial,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stats, err := ParseActionGraph(filepath.Join("testdata", "actiongraph", tc.file))
			require.NoError(t, err)
			assert.Equal(t, tc.want, stats)
			assert.Equal(t, tc.cache, CacheState(stats.CompileActions, stats.CompileCached))
		})
	}
}

func TestParseActionGraphMalformed(t *testing.T) {
	t.Parallel()

	_, err := ParseActionGraph(filepath.Join("testdata", "actiongraph", "malformed.json"))
	require.Error(t, err)
}

func TestParseActionGraphMissing(t *testing.T) {
	t.Parallel()

	_, err := ParseActionGraph(filepath.Join(t.TempDir(), "missing.json"))
	require.Error(t, err)
}

func TestCacheState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		actions int
		cached  int
		want    string
	}{
		{name: "no actions", actions: 0, cached: 0, want: CacheUnknown},
		{name: "all cached", actions: 100, cached: 100, want: CacheWarm},
		{name: "exactly 95 percent", actions: 100, cached: 95, want: CacheWarm},
		{name: "just below warm", actions: 100, cached: 94, want: CachePartial},
		{name: "just above cold", actions: 100, cached: 6, want: CachePartial},
		{name: "exactly 5 percent", actions: 100, cached: 5, want: CacheCold},
		{name: "none cached", actions: 100, cached: 0, want: CacheCold},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, CacheState(tc.actions, tc.cached))
		})
	}
}
