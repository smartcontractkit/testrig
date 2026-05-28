package runner

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/output"
)

const benchOverheadPackage = "./internal/runner/testdata/dummy/..."

// BenchmarkBaselineGoTest is the floor: raw `go test -json` against the same
// target Diagnose runs. Subtract its ns/op, B/op, allocs/op from
// BenchmarkDiagnose to read the overhead Diagnose adds.
func BenchmarkBaselineGoTest(b *testing.B) {
	repoRoot, err := filepath.Abs("../..")
	require.NoError(b, err)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		//nolint:gosec // G204: fixed args against testdata target
		cmd := exec.CommandContext(ctx, "go", "test", "-json", "-count=1", benchOverheadPackage)
		cmd.Dir = repoRoot
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		require.NoError(b, cmd.Run())
	}
}

// BenchmarkDiagnose runs one Diagnose iteration against the same target as
// BenchmarkBaselineGoTest. ns/op minus baseline is the overhead Diagnose adds.
func BenchmarkDiagnose(b *testing.B) {
	repoRoot, err := filepath.Abs("../..")
	require.NoError(b, err)

	out := output.NewForTest(true, io.Discard, io.Discard, false)
	ctx := context.Background()
	args := []string{benchOverheadPackage}
	conf := &config.App{
		RepoRoot:           repoRoot,
		Iterations:         1,
		ParallelIterations: 1,
		SlowThreshold:      time.Second,
	}

	b.ReportAllocs()
	for b.Loop() {
		require.NoError(b, Diagnose(ctx, conf, out, args, nil, nil))
	}
}

func BenchmarkResolveModuleDir(b *testing.B) {
	repoRoot, err := filepath.Abs("../..")
	require.NoError(b, err)
	args := []string{"./internal/runner/..."}

	for b.Loop() {
		_, _, err := resolveModuleDir(repoRoot, args)
		require.NoError(b, err)
	}
}
