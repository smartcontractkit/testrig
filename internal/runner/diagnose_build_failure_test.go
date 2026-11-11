package runner

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/gobuild"
	"github.com/smartcontractkit/testrig/internal/output"
)

// TestBuildPhaseCompileErrorStopsBeforeIterations runs the real compile step
// against the compileerror fixture. The run must stop before iteration 0, write
// build.log.jsonl, and print the failed step and the compiler output.
func TestBuildPhaseCompileErrorStopsBeforeIterations(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("compiles a fixture module")
	}

	repoRoot := t.TempDir()
	require.NoError(t, os.CopyFS(
		repoRoot,
		os.DirFS(filepath.Join("testdata", "prebuilt", "compileerror")),
	))

	var stderr strings.Builder
	out := output.NewForTest(false, io.Discard, &stderr, false)
	conf := &config.App{
		RepoRoot:   repoRoot,
		BuildOnce:  true,
		Iterations: 3,
	}

	state, _, resultsDir, err := RunIterations(
		context.Background(),
		conf,
		out,
		[]string{"./..."},
		nil,
		nil,
		nil,
	)
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, 0, state.Completed(), "build failure must stop before iteration 0")
	assert.True(t, state.failedFast)
	assert.Equal(t, failFastReasonBuildFailure, state.failedFastReason)

	buildLog := filepath.Join(resultsDir, "build.log.jsonl")
	logBytes, err := os.ReadFile(buildLog) //nolint:gosec // G304: temp results dir
	require.NoError(t, err, "build failure must write build.log.jsonl")
	assert.Contains(t, string(logBytes), "undefined: missingSymbol")
	assert.Contains(t, string(logBytes), `"build-fail"`)
	assert.NotContains(t, string(logBytes), `"ImportPath"`, "build log must hold diagnostics, not go list JSON")

	_, err = os.Stat(filepath.Join(resultsDir, "iteration-0.log.jsonl"))
	assert.True(t, os.IsNotExist(err), "no iteration log should be written")

	plain := stripANSI(stderr.String())
	assert.Contains(t, plain, "compiling")
	assert.Contains(t, plain, "❌")
	assert.Contains(t, plain, "BUILD ERROR")
	assert.Contains(t, plain, "undefined: missingSymbol")
}

// TestBuildPhaseLinkFailureShowsCross checks a link error leaves a failed step
// line and writes the linker output to build.log.jsonl.
func TestBuildPhaseLinkFailureShowsCross(t *testing.T) {
	t.Parallel()

	ops := buildPhaseOps{
		compile: func(context.Context, gobuild.BuildPlan) (*gobuild.CompileResult, error) {
			return &gobuild.CompileResult{
				Packages: []gobuild.Package{{ImportPath: "example.com/a", Name: "a", BinaryName: "a"}},
				GraphOK:  true,
			}, nil
		},
		link: func(context.Context, gobuild.BuildPlan, []gobuild.Package) (*gobuild.Build, error) {
			return nil, &gobuild.BuildError{
				Step:   "link",
				Output: "linker exploded\n",
				Err:    errors.New("exit status 1"),
			}
		},
	}

	var stderr strings.Builder
	out := output.NewForTest(false, io.Discard, &stderr, false)
	resultsDir := t.TempDir()
	_, binDir, err := runDiagnoseBuildPhaseWithOps(
		context.Background(),
		&config.App{BuildOnce: true, Iterations: 2},
		out,
		resultsDir,
		t.TempDir(),
		[]string{"./..."},
		nil,
		ops,
	)
	require.Error(t, err)
	assert.Equal(t, "link", buildFailureStep(err))

	plain := stripANSI(stderr.String())
	assert.Contains(t, plain, "linking")
	assert.Contains(t, plain, "❌")

	buildLogPath := filepath.Join(resultsDir, "build.log.jsonl")
	//nolint:gosec // G304: temp results dir
	logBytes, readErr := os.ReadFile(buildLogPath)
	require.NoError(t, readErr)
	assert.Contains(t, string(logBytes), "linker exploded")
	assert.Contains(t, string(logBytes), `"build-fail"`)

	_, statErr := os.Stat(binDir)
	assert.True(t, os.IsNotExist(statErr), "binary dir is removed on failure")
}

// TestBuildPhaseCancelDuringLinkShowsStopped cancels while Link is running. The
// link step must render "stopped" instead of a failed step, and the binary dir
// must be removed.
func TestBuildPhaseCancelDuringLinkShowsStopped(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	linkStarted := make(chan struct{})
	ops := buildPhaseOps{
		compile: func(_ context.Context, plan gobuild.BuildPlan) (*gobuild.CompileResult, error) {
			// Leave one linked binary behind so the live line shows [1/1].
			dir := filepath.Join(plan.BinDir, "g0")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, err
			}
			if err := os.WriteFile(filepath.Join(dir, "a.test"), []byte("x"), 0o600); err != nil {
				return nil, err
			}
			return &gobuild.CompileResult{
				Packages: []gobuild.Package{{ImportPath: "example.com/a", Name: "a", BinaryName: "a"}},
				GraphOK:  true,
			}, nil
		},
		link: func(ctx context.Context, _ gobuild.BuildPlan, _ []gobuild.Package) (*gobuild.Build, error) {
			close(linkStarted)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	var stderr strings.Builder
	out := output.NewForTest(false, io.Discard, &stderr, true)
	resultsDir := t.TempDir()

	go func() {
		<-linkStarted
		cancel()
	}()

	_, binDir, err := runDiagnoseBuildPhaseWithOps(
		ctx,
		&config.App{BuildOnce: true, Iterations: 2},
		out,
		resultsDir,
		t.TempDir(),
		[]string{"./..."},
		nil,
		ops,
	)
	require.ErrorIs(t, err, context.Canceled)

	plain := stripANSI(stderr.String())
	assert.Contains(t, plain, "linking")
	assert.Contains(t, plain, "[1/1]")
	assert.Contains(t, plain, "stopped")
	assert.NotContains(t, plain, "❌")

	_, statErr := os.Stat(binDir)
	assert.True(t, os.IsNotExist(statErr), "binary dir is removed on cancel")
	_, logErr := os.Stat(filepath.Join(resultsDir, "build.log.jsonl"))
	assert.True(t, os.IsNotExist(logErr), "a cancel is not a build failure")
}
