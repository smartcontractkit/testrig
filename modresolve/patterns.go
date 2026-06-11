package modresolve

import (
	"slices"
	"strings"
)

// testFilterTwoArgSuffixFlags are go test filter flags that consume the following
// argv token when scanning backward for package patterns.
var testFilterTwoArgSuffixFlags = map[string]bool{
	"-run":   true,
	"-bench": true,
	"-skip":  true,
	"-fuzz":  true,
}

func singleArgTestBinaryFlagPrefix(arg string) (prefix string, ok bool) {
	for _, p := range []string{"-run=", "-bench=", "-skip=", "-fuzz="} {
		if strings.HasPrefix(arg, p) {
			return p, true
		}
	}
	return "", false
}

func looksLikeGoPackagePattern(arg string) bool {
	return strings.Contains(arg, ".") ||
		strings.Contains(arg, "/") ||
		strings.Contains(arg, "...")
}

// GoTestFlagsBeforeArgs returns the portion of argv that belongs to `go test`
// itself, stopping before -args (flags after -args are passed to the test binary).
func GoTestFlagsBeforeArgs(args []string) []string {
	for i, a := range args {
		if a == "-args" {
			return args[:i]
		}
	}
	return args
}

// PackagePatternsFromEnd returns trailing arguments that look like package patterns.
// It scans backward from the end of GoTestFlagsBeforeArgs(args), skipping standard
// test binary flags and their values so `./pkg -run TestName` still yields `./pkg`.
func PackagePatternsFromEnd(args []string) []string {
	args = GoTestFlagsBeforeArgs(args)
	var pkgs []string
	inPkgs := false
	for i := len(args) - 1; i >= 0; i-- {
		arg := args[i]
		if _, ok := singleArgTestBinaryFlagPrefix(arg); ok {
			if inPkgs {
				break
			}
			continue
		}
		if i >= 1 && testFilterTwoArgSuffixFlags[args[i-1]] {
			if inPkgs {
				break
			}
			i--
			continue
		}
		isFlag := strings.HasPrefix(arg, "-")
		if isFlag {
			if inPkgs {
				break
			}
			continue
		}
		if !looksLikeGoPackagePattern(arg) {
			if inPkgs {
				break
			}
			continue
		}
		inPkgs = true
		pkgs = append(pkgs, arg)
	}
	slices.Reverse(pkgs)
	return pkgs
}
