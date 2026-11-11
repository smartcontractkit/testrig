package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/gobuild"
	"github.com/smartcontractkit/testrig/internal/output"
	"github.com/smartcontractkit/testrig/internal/termstyle"
)

// buildPhaseOps are the compile and link entry points. Tests inject fakes to
// control step timing, such as canceling during the link step.
type buildPhaseOps struct {
	compile func(context.Context, gobuild.BuildPlan) (*gobuild.CompileResult, error)
	link    func(context.Context, gobuild.BuildPlan, []gobuild.Package) (*gobuild.Build, error)
}

func defaultBuildPhaseOps() buildPhaseOps {
	return buildPhaseOps{compile: gobuild.Compile, link: gobuild.Link}
}

// runDiagnoseBuildPhase compiles and links test binaries once when configured.
// It returns metrics even in go-test mode so report.json records why the run
// fell back. A non-empty binDir must be removed by the caller when the run ends.
func runDiagnoseBuildPhase(
	ctx context.Context,
	conf *config.App,
	out *output.Printer,
	resultsDir string,
	moduleDir string,
	goTestArgs []string,
	resources []diagnoseIterationResource,
) (metrics *BuildMetrics, binDir string, err error) {
	return runDiagnoseBuildPhaseWithOps(
		ctx,
		conf,
		out,
		resultsDir,
		moduleDir,
		goTestArgs,
		resources,
		defaultBuildPhaseOps(),
	)
}

func runDiagnoseBuildPhaseWithOps(
	ctx context.Context,
	conf *config.App,
	out *output.Printer,
	resultsDir string,
	moduleDir string,
	goTestArgs []string,
	resources []diagnoseIterationResource,
	ops buildPhaseOps,
) (metrics *BuildMetrics, binDir string, err error) {
	if conf == nil || !conf.BuildOnce {
		return &BuildMetrics{
			Mode:           buildModeGoTest,
			FallbackReason: "--build-once=false",
		}, "", nil
	}
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	plan, err := gobuild.Plan(gobuild.PlanOptions{
		ModuleDir:   moduleDir,
		Args:        goTestArgs,
		GOFLAGS:     os.Getenv("GOFLAGS"),
		ResourceEnv: flattenResourceEnv(resources),
		BuildOnce:   conf.BuildOnce,
		Strip:       true,
	})
	if err != nil {
		return nil, "", err
	}
	if plan.Fallback != nil {
		return &BuildMetrics{
			Mode:           buildModeGoTest,
			FallbackReason: plan.Fallback.Reason,
		}, "", nil
	}

	progress := newBuildProgress(out)
	progress.begin()

	binDir, err = os.MkdirTemp("", "testrig-bin-")
	if err != nil {
		return nil, "", err
	}
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(binDir)
		}
	}()
	plan.BinDir = binDir

	finishCompile := progress.start("compiling", "compile started", nil)
	compiled, err := ops.compile(ctx, plan)
	if err != nil {
		finishBuildStep(ctx, finishCompile, resultsDir, err)
		return nil, binDir, fmt.Errorf("build test binaries: %w", err)
	}
	finishCompile(buildStepOK)

	metrics = &BuildMetrics{
		Mode:           buildModePrebuilt,
		Cache:          gobuild.CacheUnknown,
		CompileActions: compiled.CompileActions,
		CompileCached:  compiled.CompileCached,
		CompileWall:    compiled.Wall,
		CompileTotal:   compiled.CompileTotal,
		Stripped:       plan.Strip,
	}
	if compiled.GraphOK {
		metrics.Cache = gobuild.CacheState(compiled.CompileActions, compiled.CompileCached)
	}
	printBuildCompiled(out, metrics)

	linkTotal := len(compiled.Packages)
	finishLink := progress.start(
		"linking",
		fmt.Sprintf("link started · %d test binaries", linkTotal),
		func() (int, int) { return countTestBinaries(binDir), linkTotal },
	)
	linked, err := ops.link(ctx, plan, compiled.Packages)
	if err != nil {
		finishBuildStep(ctx, finishLink, resultsDir, err)
		return nil, binDir, fmt.Errorf("build test binaries: %w", err)
	}
	finishLink(buildStepOK)
	metrics.LinkWall = linked.Metrics.LinkWall
	metrics.LinkTotal = linked.Metrics.LinkTotal
	metrics.VetTotal = linked.Metrics.VetTotal
	metrics.Binaries = linked.Metrics.Binaries
	metrics.ExternalLink = linked.Metrics.ExternalLink
	metrics.Floor = linked.Metrics.Floor
	if savings := max(conf.Iterations-1, 0); savings > 0 {
		metrics.SavedEstimate = metrics.Floor * time.Duration(savings)
	}
	printBuildLinked(out, metrics)
	printBuildFloor(out, metrics, conf.Iterations)

	success = true
	return metrics, binDir, nil
}

// finishBuildStep renders the failed or stopped line and, for a build failure,
// records the go command output in build.log.jsonl so printBuildError can show
// it. A canceled context means the step stopped; it is not a build failure.
func finishBuildStep(
	ctx context.Context,
	finish func(buildStepOutcome),
	resultsDir string,
	err error,
) {
	if ctx.Err() != nil {
		finish(buildStepStopped)
		return
	}
	finish(buildStepFailed)
	if buildErr, ok := errors.AsType[*gobuild.BuildError](err); ok {
		writeBuildFailureLog(resultsDir, buildErr.Step, buildErr.Output)
	}
}

// buildLogEvent mirrors the go test -json event shape for the build phase.
type buildLogEvent struct {
	Time        time.Time `json:"Time"`
	Action      string    `json:"Action"`
	Output      string    `json:"Output,omitempty"`
	FailedBuild string    `json:"FailedBuild,omitempty"`
}

// writeBuildFailureLog writes build.log.jsonl as go test -json style
// build-output and build-fail events. Build reporting never masks the failure.
func writeBuildFailureLog(resultsDir, step, output string) {
	if resultsDir == "" {
		return
	}
	f, err := os.Create(filepath.Join(resultsDir, "build.log.jsonl")) //nolint:gosec // G304: results dir from caller
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	for line := range strings.SplitSeq(output, "\n") {
		if line == "" {
			continue
		}
		_ = enc.Encode(buildLogEvent{Time: time.Now(), Action: "build-output", Output: line + "\n"})
	}
	_ = enc.Encode(buildLogEvent{Time: time.Now(), Action: "build-fail", FailedBuild: step})
}

// buildFailureStep returns the compile or link step from a build error, or an
// empty string when the error did not come from a go build command.
func buildFailureStep(err error) string {
	if buildErr, ok := errors.AsType[*gobuild.BuildError](err); ok {
		return buildErr.Step
	}
	return ""
}

// isBuildFailure reports whether err is a compile or link failure from the go
// command, which diagnose treats as fail-fast.
func isBuildFailure(err error) bool {
	_, ok := errors.AsType[*gobuild.BuildError](err)
	return ok
}

// handleBuildPhaseFailure records a compile or link failure on the run state.
// No iteration has run, so the run stops before iteration 0 with the same
// fail-fast reason go test would have produced. It returns false when err is a
// cancel or another setup error and the caller should report it as-is.
func handleBuildPhaseFailure(
	ctx context.Context,
	state *DiagnoseRunState,
	resultsDir string,
	err error,
) bool {
	if ctx.Err() != nil || !isBuildFailure(err) {
		return false
	}
	state.failedFast = true
	state.failedFastReason = failFastReasonBuildFailure
	state.failedFastIteration = 0
	state.buildLogPath = filepath.Join(resultsDir, "build.log.jsonl")
	state.buildFailureStep = buildFailureStep(err)
	return true
}

func flattenResourceEnv(resources []diagnoseIterationResource) []string {
	var env []string
	for _, resource := range resources {
		env = append(env, resource.Env...)
	}
	return env
}

func printBuildCompiled(out *output.Printer, metrics *BuildMetrics) {
	if out == nil || metrics == nil {
		return
	}
	if out.AIOutput() {
		out.Stderrf(
			"build_compile s=%d cache=%s pkgs=%d cached=%d\n",
			buildSeconds(metrics.CompileWall),
			metrics.Cache,
			metrics.CompileActions,
			metrics.CompileCached,
		)
		return
	}
	out.HumanStderr(buildCompiledLine(metrics))
}

func buildCompiledLine(metrics *BuildMetrics) string {
	details := []string{
		fmt.Sprintf("%d packages", metrics.CompileActions),
		fmt.Sprintf("%d cached", metrics.CompileCached),
	}
	return termstyle.Label.Render("compiled") + " " +
		progressBracket(termstyle.Muted.Render(formatDiagnoseWallClock(metrics.CompileWall))) + " " +
		termstyle.Muted.Render("· "+strings.Join(details, " · ")+" · ") +
		buildCacheStyle(metrics.Cache).Render(metrics.Cache+" cache")
}

func printBuildLinked(out *output.Printer, metrics *BuildMetrics) {
	if out == nil || metrics == nil {
		return
	}
	if out.AIOutput() {
		out.Stderrf(
			"build_link s=%d bins=%d ext=%d strip=%d floor_s=%d\n",
			buildSeconds(metrics.LinkWall),
			metrics.Binaries,
			metrics.ExternalLink,
			boolInt(metrics.Stripped),
			buildSeconds(metrics.Floor),
		)
		return
	}
	out.HumanStderr(buildLinkedLine(metrics))
}

func buildLinkedLine(metrics *BuildMetrics) string {
	details := []string{
		fmt.Sprintf("%d test binaries", metrics.Binaries),
		fmt.Sprintf("%d likely external link (cgo)", metrics.ExternalLink),
	}
	if metrics.Stripped {
		details = append(details, "stripped")
	} else {
		details = append(details, "not stripped")
	}
	if vetShare := vetDetail(metrics.VetTotal, metrics.LinkWall); vetShare != "" {
		details = append(details, vetShare)
	}
	return termstyle.Label.Render("linked") + " " +
		progressBracket(termstyle.Muted.Render(formatDiagnoseWallClock(metrics.LinkWall))) + " " +
		termstyle.Muted.Render("· "+strings.Join(details, " · "))
}

func printBuildFloor(out *output.Printer, metrics *BuildMetrics, iterations int) {
	if out == nil || metrics == nil || out.AIOutput() || metrics.Floor <= 0 {
		return
	}
	out.HumanStderr(buildFloorLine(metrics, iterations))
}

func buildFloorLine(metrics *BuildMetrics, iterations int) string {
	line := termstyle.Muted.Render("build floor ") +
		termstyle.Label.Render(formatDiagnoseWallClock(metrics.Floor)) +
		termstyle.Muted.Render(" per iteration")
	if metrics.SavedEstimate > 0 && iterations > 1 {
		line += termstyle.Muted.Render(
			fmt.Sprintf(" · saves ~%s over %d iterations", formatDiagnoseWallClock(metrics.SavedEstimate), iterations),
		)
	}
	return line
}

// countTestBinaries counts linked .test binaries under dir. The link step
// writes one group subdirectory at a time, so this is the live link count.
func countTestBinaries(dir string) int {
	if dir == "" {
		return 0
	}
	count := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, ".test") || strings.HasSuffix(name, ".test.exe") {
			count++
		}
		return nil
	})
	return count
}

func buildCacheStyle(cache string) interface{ Render(...string) string } {
	switch cache {
	case gobuild.CacheWarm:
		return termstyle.OK
	case gobuild.CachePartial, gobuild.CacheCold:
		return termstyle.Accent
	default:
		return termstyle.Muted
	}
}

func vetDetail(vet, linkWall time.Duration) string {
	if vet <= 0 || linkWall <= 0 || vet*100 < linkWall*5 {
		return ""
	}
	return "vet " + formatDiagnoseWallClock(vet)
}

func buildSeconds(d time.Duration) int {
	return max(int(d.Round(time.Second)/time.Second), 0)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
