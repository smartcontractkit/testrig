package gobuild

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Binary is one linked test binary.
type Binary struct {
	ImportPath   string
	NodeName     string
	Path         string
	Dir          string
	ExternalLink bool
}

// Build describes the link step and its metrics.
type Build struct {
	Binaries []Binary
	Metrics  BuildMetrics
}

// BuildMetrics records compile and link overhead for a diagnose run.
type BuildMetrics struct {
	Mode           string        `json:"mode"`
	FallbackReason string        `json:"fallback_reason,omitempty"`
	Cache          string        `json:"cache"`
	CompileActions int           `json:"compile_actions"`
	CompileCached  int           `json:"compile_cached"`
	CompileWall    time.Duration `json:"compile_wall"`
	CompileTotal   time.Duration `json:"compile_total"`
	LinkWall       time.Duration `json:"link_wall"`
	LinkTotal      time.Duration `json:"link_total"`
	VetTotal       time.Duration `json:"vet_total"`
	Binaries       int           `json:"binaries"`
	ExternalLink   int           `json:"external_link"`
	Stripped       bool          `json:"stripped"`
	Floor          time.Duration `json:"floor"`
	SavedEstimate  time.Duration `json:"saved_estimate,omitempty"`
}

// Link builds test binaries into plan.BinDir. Packages are grouped so no
// `go test -c` invocation writes two binaries with the same name.
func Link(ctx context.Context, plan BuildPlan, pkgs []Package) (*Build, error) {
	if plan.Fallback != nil {
		return nil, errors.New("gobuild: link called for a fallback plan")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	build := &Build{}
	if len(pkgs) == 0 {
		return build, nil
	}

	graphDir, err := os.MkdirTemp("", "testrig-link-graphs-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(graphDir) }()

	groups := groupPackages(pkgs)
	start := time.Now()
	for i, group := range groups {
		outDir := binaryPathForGroup(plan.BinDir, i)
		if err := os.MkdirAll(outDir, 0o700); err != nil {
			return nil, err
		}
		graphPath := filepath.Join(graphDir, fmt.Sprintf("g%d.json", i))
		args := []string{"test", "-c", "-o", outDir, "-debug-actiongraph=" + graphPath}
		args = append(args, plan.BuildFlags...)
		args = append(args, plan.LinkFlags...)
		if plan.Strip {
			args = append(args, "-ldflags=-s -w")
		}
		for _, pkg := range group {
			args = append(args, pkg.ImportPath)
		}

		var stdout, stderr bytes.Buffer
		if err := runGo(ctx, plan.ModuleDir, args, &stdout, &stderr); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, &BuildError{Step: "link", Output: combinedOutput(stderr.String(), stdout.String()), Err: err}
		}

		binaries, err := binariesInGroup(outDir, group)
		if err != nil {
			return nil, err
		}
		build.Binaries = append(build.Binaries, binaries...)

		if stats, graphErr := ParseActionGraph(graphPath); graphErr == nil {
			build.Metrics.LinkTotal += stats.LinkTotal
			build.Metrics.VetTotal += stats.VetTotal
		}
	}
	wall := time.Since(start)

	build.Metrics.LinkWall = wall
	build.Metrics.Floor = wall
	build.Metrics.Binaries = len(build.Binaries)
	build.Metrics.Stripped = plan.Strip
	for _, binary := range build.Binaries {
		if binary.ExternalLink {
			build.Metrics.ExternalLink++
		}
	}
	return build, nil
}

// groupPackages greedily packs packages into groups that have no duplicate
// binary names. `go test -c -o dir/` names each binary <importpath-base>.test,
// so a duplicate in one group fails with "cannot write test binary ... for
// multiple packages".
func groupPackages(pkgs []Package) [][]Package {
	sorted := append([]Package(nil), pkgs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ImportPath < sorted[j].ImportPath })

	var groups [][]Package
	var names []map[string]bool
	for _, pkg := range sorted {
		binaryName := packageBinaryName(pkg)
		placed := false
		for i := range groups {
			if names[i][binaryName] {
				continue
			}
			groups[i] = append(groups[i], pkg)
			names[i][binaryName] = true
			placed = true
			break
		}
		if !placed {
			groups = append(groups, []Package{pkg})
			names = append(names, map[string]bool{binaryName: true})
		}
	}
	return groups
}

func packageBinaryName(pkg Package) string {
	if pkg.BinaryName != "" {
		return pkg.BinaryName
	}
	return testBinaryName(pkg.ImportPath)
}

func binariesInGroup(dir string, group []Package) ([]Binary, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]Package, len(group))
	for _, pkg := range group {
		byName[packageBinaryName(pkg)] = pkg
	}
	var binaries []Binary
	for _, entry := range entries {
		if entry.IsDir() || !isTestBinaryName(entry.Name()) {
			continue
		}
		base := strings.TrimSuffix(strings.TrimSuffix(entry.Name(), ".exe"), ".test")
		pkg, ok := byName[base]
		if !ok {
			continue
		}
		binaries = append(binaries, Binary{
			ImportPath:   pkg.ImportPath,
			NodeName:     pkg.Name,
			Path:         filepath.Join(dir, entry.Name()),
			Dir:          pkg.Dir,
			ExternalLink: pkg.ExternalLink,
		})
	}
	sort.Slice(binaries, func(i, j int) bool { return binaries[i].ImportPath < binaries[j].ImportPath })
	return binaries, nil
}

func isTestBinaryName(name string) bool {
	return strings.HasSuffix(name, ".test") || strings.HasSuffix(name, ".test.exe")
}
