// Package modresolve resolves Go module roots for package patterns in multi-module
// repositories. Given relative patterns like ./deployment/..., it walks up from
// the pattern's base directory to find the nearest go.mod (bounded at repoRoot),
// rewrites patterns relative to that module, and returns the directory to use
// as cmd.Dir for go test or go list.
package modresolve

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolvePatterns returns the Go module root for relative package patterns and
// rewrites those patterns relative to the module directory. When all patterns
// belong to repoRoot's module, repoRoot is returned with patterns unchanged.
// Returns an error when patterns span more than one module.
func ResolvePatterns(repoRoot string, patterns []string) (moduleDir string, rewritten []string, err error) {
	repoRoot = filepath.Clean(repoRoot)
	moduleDir, err = resolveModuleFromPatterns(repoRoot, patterns)
	if err != nil {
		return "", nil, err
	}
	if moduleDir == repoRoot {
		return repoRoot, patterns, nil
	}
	return moduleDir, rewritePatterns(repoRoot, moduleDir, patterns), nil
}

// ResolveArgs extracts trailing package patterns from go test arguments (see
// PackagePatternsFromEnd), resolves the module directory from those patterns,
// and rewrites all relative package-pattern arguments in the full slice. Module
// resolution uses only trailing patterns; rewriting applies to every relative
// pattern argument, not only trailing ones. Flags and non-pattern args are preserved.
func ResolveArgs(repoRoot string, goTestArgs []string) (moduleDir string, rewrittenArgs []string, err error) {
	repoRoot = filepath.Clean(repoRoot)
	patterns := PackagePatternsFromEnd(goTestArgs)

	moduleDir, err = resolveModuleFromPatterns(repoRoot, patterns)
	if err != nil {
		return "", nil, err
	}
	if moduleDir == repoRoot {
		return repoRoot, goTestArgs, nil
	}
	return moduleDir, rewriteRelativePatterns(repoRoot, moduleDir, goTestArgs), nil
}

// NearestModuleRoot walks up from dir toward stopAt looking for a go.mod.
// Returns an error if no intermediate or stopAt go.mod is found.
func NearestModuleRoot(dir, stopAt string) (string, error) {
	return nearestModuleRoot(dir, stopAt)
}

func resolveModuleFromPatterns(repoRoot string, patterns []string) (moduleDir string, err error) {
	var relative []string
	for _, p := range patterns {
		if isRelativePackagePattern(p) {
			relative = append(relative, p)
		}
	}
	if len(relative) == 0 {
		return repoRoot, nil
	}

	var moduleRoot string
	for _, p := range relative {
		dir := patternBaseDir(p)
		abs := filepath.Join(repoRoot, dir)
		mod, err := nearestModuleRoot(abs, repoRoot)
		if err != nil {
			return "", err
		}
		if moduleRoot == "" {
			moduleRoot = mod
		} else if moduleRoot != mod {
			return "", fmt.Errorf(
				"package patterns span multiple Go modules (%s and %s): run go test for each module separately",
				moduleRoot, mod,
			)
		}
	}
	return moduleRoot, nil
}

func isRelativePackagePattern(p string) bool {
	return strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") || p == "." || p == ".."
}

func patternBaseDir(p string) string {
	if s, ok := strings.CutSuffix(p, "/..."); ok {
		return s
	}
	if s, ok := strings.CutSuffix(p, "/."); ok {
		return s
	}
	return p
}

func nearestModuleRoot(dir, stopAt string) (string, error) {
	d := filepath.Clean(dir)
	stop := filepath.Clean(stopAt)
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d, nil
		}
		if d == stop {
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return "", fmt.Errorf("no go.mod found between %s and %s", dir, stopAt)
}

func rewriteOneRelativePattern(repoRoot, moduleDir, p string) string {
	base := patternBaseDir(p)
	suffix := p[len(base):]
	abs := filepath.Join(repoRoot, base)
	rel, err := filepath.Rel(moduleDir, abs)
	if err != nil {
		return p
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return "." + suffix
	}
	return "./" + rel + suffix
}

func rewritePatterns(repoRoot, moduleDir string, patterns []string) []string {
	result := make([]string, len(patterns))
	for i, p := range patterns {
		if !isRelativePackagePattern(p) {
			result[i] = p
			continue
		}
		result[i] = rewriteOneRelativePattern(repoRoot, moduleDir, p)
	}
	return result
}

func rewriteRelativePatterns(repoRoot, moduleDir string, goTestArgs []string) []string {
	result := make([]string, len(goTestArgs))
	for i, arg := range goTestArgs {
		if !isRelativePackagePattern(arg) || !looksLikeGoPackagePattern(arg) {
			result[i] = arg
			continue
		}
		result[i] = rewriteOneRelativePattern(repoRoot, moduleDir, arg)
	}
	return result
}
