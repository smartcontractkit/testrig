package runner

import (
	"context"
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

// runDiagnoseBuildPhase compiles and links test binaries once when configured.
// It returns metrics even in go-test mode so report.json records why the run
// fell back. A non-empty binDir must be removed by the caller when the run ends.
func runDiagnoseBuildPhase(
	ctx context.Context,
	conf *config.App,
	out *output.Printer,
	moduleDir string,
	goTestArgs []string,
	resources []diagnoseIterationResource,
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
	compiled, err := gobuild.Compile(ctx, plan)
	finishCompile()
	if err != nil {
		return nil, binDir, fmt.Errorf("build test binaries: %w", err)
	}

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
	linked, err := gobuild.Link(ctx, plan, compiled.Packages)
	finishLink()
	if err != nil {
		return nil, binDir, fmt.Errorf("build test binaries: %w", err)
	}
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
