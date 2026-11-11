package gobuild

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Package is a pattern-matched package that has tests and therefore produces a
// test binary.
type Package struct {
	ImportPath string
	Name       string
	// BinaryName is the basename `go test -c -o dir/` uses, derived from the
	// import path (not the package clause). Two packages can share it even
	// when their package names differ.
	BinaryName string
	Dir        string
	// ExternalLink is a heuristic: true when the test binary depends on a
	// package with cgo files outside the common stdlib cgo users.
	ExternalLink bool
}

// CompileResult describes the `go list -export -test -deps` step.
type CompileResult struct {
	Packages []Package
	Wall     time.Duration

	CompileActions int
	CompileCached  int
	CompileTotal   time.Duration
	// GraphOK is false when the action graph was missing or malformed. Wall
	// times still count; cache state degrades to unknown.
	GraphOK bool
}

// Compile fills the build cache with every package and test variant. It does
// not link test binaries.
func Compile(ctx context.Context, plan BuildPlan) (*CompileResult, error) {
	if plan.Fallback != nil {
		return nil, errors.New("gobuild: compile called for a fallback plan")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	graph, err := os.CreateTemp("", "testrig-compile-graph-*.json")
	if err != nil {
		return nil, err
	}
	graphPath := graph.Name()
	_ = graph.Close()
	defer func() { _ = os.Remove(graphPath) }()

	args := []string{"list", "-export", "-test", "-deps", "-json", "-debug-actiongraph=" + graphPath}
	args = append(args, plan.BuildFlags...)
	args = append(args, plan.Patterns...)

	start := time.Now()
	var stdout, stderr bytes.Buffer
	if err := runGo(ctx, plan.ModuleDir, args, &stdout, &stderr); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// `go list -json` writes partial JSON to stdout even when it fails.
		// Keep the compiler diagnostics from stderr so build.log.jsonl and
		// printBuildError show the error, not a package dump.
		output := stderr.String()
		if strings.TrimSpace(output) == "" {
			output = stdout.String()
		}
		return nil, &BuildError{Step: "compile", Output: output, Err: err}
	}
	wall := time.Since(start)

	packages, err := parseListPackages(stdout.Bytes())
	if err != nil {
		output := stderr.String()
		if strings.TrimSpace(output) == "" {
			output = err.Error()
		}
		return nil, &BuildError{Step: "compile", Output: output, Err: err}
	}

	stats, graphErr := ParseActionGraph(graphPath)
	result := &CompileResult{
		Packages: packages,
		Wall:     wall,
		GraphOK:  graphErr == nil,
	}
	if graphErr == nil {
		result.CompileActions = stats.CompileActions
		result.CompileCached = stats.CompileCached
		result.CompileTotal = stats.CompileTotal
	}
	return result, nil
}

type listPackage struct {
	ImportPath   string
	Name         string
	Dir          string
	ForTest      string
	Match        []string
	TestGoFiles  []string
	XTestGoFiles []string
	TestImports  []string
	XTestImports []string
	Deps         []string
	CgoFiles     []string
	Error        *listError
	DepsErrors   []*listError
}

type listError struct {
	ImportPath string
	Pos        string
	Err        string
}

func (e *listError) String() string {
	if e == nil {
		return ""
	}
	pos := e.Pos
	if pos == "" && e.ImportPath != "" {
		pos = e.ImportPath
	}
	if pos == "" {
		return e.Err
	}
	return pos + ": " + e.Err
}

func parseListPackages(raw []byte) ([]Package, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var all []listPackage
	var listErrs []string
	for {
		var lp listPackage
		err := dec.Decode(&lp)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse go list output: %w", err)
		}
		all = append(all, lp)
		if lp.Error != nil {
			listErrs = append(listErrs, lp.Error.String())
		}
		for _, de := range lp.DepsErrors {
			listErrs = append(listErrs, de.String())
		}
	}
	if len(listErrs) > 0 {
		return nil, errors.New(strings.Join(listErrs, "\n"))
	}

	cgo := make(map[string]bool, len(all))
	testVariantDeps := make(map[string][]string)
	for _, lp := range all {
		cgo[lp.ImportPath] = len(lp.CgoFiles) > 0
		if lp.ForTest != "" {
			testVariantDeps[lp.ForTest] = lp.Deps
		}
	}

	var pkgs []Package
	for _, lp := range all {
		if len(lp.Match) == 0 || lp.ForTest != "" {
			continue
		}
		if len(lp.TestGoFiles) == 0 && len(lp.XTestGoFiles) == 0 {
			continue
		}
		deps := testVariantDeps[lp.ImportPath]
		if len(deps) == 0 {
			deps = append(deps, lp.TestImports...)
			deps = append(deps, lp.XTestImports...)
		}
		pkgs = append(pkgs, Package{
			ImportPath:   lp.ImportPath,
			Name:         lp.Name,
			BinaryName:   testBinaryName(lp.ImportPath),
			Dir:          lp.Dir,
			ExternalLink: hasExternalCgoDeps(deps, cgo),
		})
	}
	sort.Slice(pkgs, func(i, j int) bool { return pkgs[i].ImportPath < pkgs[j].ImportPath })
	return pkgs, nil
}

var stdlibCgoAllow = map[string]bool{
	"runtime/cgo": true,
	"net":         true,
	"os/user":     true,
}

func hasExternalCgoDeps(deps []string, cgo map[string]bool) bool {
	for _, dep := range deps {
		if stdlibCgoAllow[dep] {
			continue
		}
		if cgo[dep] {
			return true
		}
	}
	return false
}

// BuildError is a compile or link failure with the go command's output.
type BuildError struct {
	Step   string // "compile" or "link"
	Output string
	Err    error
}

func (e *BuildError) Error() string {
	if e == nil {
		return ""
	}
	if e.Output != "" {
		return fmt.Sprintf("%s failed: %v\n%s", e.Step, e.Err, strings.TrimRight(e.Output, "\n"))
	}
	return fmt.Sprintf("%s failed: %v", e.Step, e.Err)
}

func (e *BuildError) Unwrap() error { return e.Err }

func combinedOutput(stderr, stdout string) string {
	switch {
	case stderr == "":
		return stdout
	case stdout == "":
		return stderr
	default:
		return stderr + stdout
	}
}

// testBinaryName mirrors cmd/go's DefaultExecName for module import paths:
// the last path element, skipping a major-version element like v2.
func testBinaryName(importPath string) string {
	elem := path.Base(importPath)
	if isVersionElement(elem) {
		elem = path.Base(path.Dir(importPath))
	}
	return elem
}

// isVersionElement reports whether s is a Go major-version path element (v2,
// v3, ...), but not v0 or v1.
func isVersionElement(s string) bool {
	if len(s) < 2 || s[0] != 'v' || s[1] == '0' || s[1] == '1' && len(s) == 2 {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// binaryPathForGroup returns the directory that receives a group's binaries.
// The trailing separator tells `go test -c` to treat it as a directory.
func binaryPathForGroup(binDir string, group int) string {
	return filepath.Join(binDir, fmt.Sprintf("g%d", group)) + string(os.PathSeparator)
}
