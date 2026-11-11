// Package gobuild compiles and links test binaries once so diagnose can reuse
// them across iterations.
package gobuild

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Build modes recorded in BuildMetrics.Mode.
const (
	ModePrebuilt = "prebuilt"
	ModeGoTest   = "go-test"
)

// Cache states recorded in BuildMetrics.Cache.
const (
	CacheWarm    = "warm"
	CachePartial = "partial"
	CacheCold    = "cold"
	CacheUnknown = "unknown"
)

// Fallback explains why a run must keep using `go test` for every iteration.
type Fallback struct {
	// Token is the short --ai-output token, such as "coverprofile" or
	// "build-once-off".
	Token string
	// Reason is the human-readable fallback detail, such as
	// "-coverprofile needs go test".
	Reason string
}

// BuildPlan is a classified `go test` command line. It separates flags the
// compile and link steps need from test-binary flags and package patterns.
type BuildPlan struct {
	// ModuleDir is the working directory for go commands.
	ModuleDir string
	// BuildFlags are accepted by both `go list` and `go test -c`.
	BuildFlags []string
	// LinkFlags are accepted by `go test -c` but not `go list`.
	LinkFlags []string
	// TestFlags are translated `-test.*` flags for the test binary.
	TestFlags []string
	// Patterns are package patterns (the final unclassified arguments).
	Patterns []string
	// Passthrough holds arguments after -args.
	Passthrough []string
	// BinDir receives the linked test binaries. Link creates one subdirectory
	// per name group below it.
	BinDir string
	// Strip is true when Link should add `-ldflags=-s -w`.
	Strip bool
	// P is the maximum number of test binaries to run at once.
	P int
	// Fallback is non-nil when the run is not eligible for prebuilt binaries.
	Fallback *Fallback
}

// PlanOptions carries the inputs Plan classifies.
type PlanOptions struct {
	// ModuleDir is the resolved module directory.
	ModuleDir string
	// Args are module-relative `go test` arguments (after modresolve).
	Args []string
	// GOFLAGS is the base environment's GOFLAGS. The go command applies it
	// itself; Plan only inspects it for -ldflags.
	GOFLAGS string
	// ResourceEnv is the union of diagnose resource environment entries.
	// A resource that changes build inputs forces go-test mode.
	ResourceEnv []string
	// BuildOnce is the --build-once flag. False forces go-test mode.
	BuildOnce bool
	// Strip is the --strip-binaries value (true by default in this phase).
	Strip bool
}

// Plan classifies go test arguments and decides whether prebuilt binaries are
// possible. A nil error with a non-nil Fallback means "run go test as before".
func Plan(opts PlanOptions) (BuildPlan, error) {
	plan := BuildPlan{
		ModuleDir: opts.ModuleDir,
		Strip:     opts.Strip,
		P:         runtime.GOMAXPROCS(0),
	}
	if !opts.BuildOnce {
		plan.Fallback = &Fallback{Token: "build-once-off", Reason: "--build-once=false"}
		return plan, nil
	}
	if env := riskyResourceEnv(opts.ResourceEnv); env != "" {
		plan.Fallback = &Fallback{Token: strings.ToLower(env), Reason: "resource env sets " + env}
		return plan, nil
	}

	beforeArgs, passthrough := splitAtArgs(opts.Args)
	plan.Passthrough = passthrough
	state := planFlagState{plan: &plan, args: beforeArgs}

	for i := 0; i < len(beforeArgs); i++ {
		arg := beforeArgs[i]
		if arg == "--" {
			plan.Passthrough = append(plan.Passthrough, beforeArgs[i+1:]...)
			break
		}
		name, value, hasValue := splitFlag(arg)
		if name == "" {
			plan.Patterns = append(plan.Patterns, arg)
			continue
		}
		next, err := state.apply(i, arg, name, value, hasValue)
		if err != nil {
			return BuildPlan{}, err
		}
		i = next
		if plan.Fallback != nil {
			return plan, nil
		}
	}

	if !state.setP {
		plan.BuildFlags = append(plan.BuildFlags, "-p="+strconv.Itoa(plan.P))
	}
	injected := []string{"-test.paniconexit0", "-test.v=test2json"}
	if !state.userSetTimeout {
		injected = append(injected, "-test.timeout="+defaultGoTestTimeout.String())
	}
	if !state.userSetCount {
		injected = append(injected, "-test.count=1")
	}
	if state.needOutputDir {
		// The binary runs in its package directory, but go test writes
		// artifacts to the go command's working directory.
		outputDir := opts.ModuleDir
		if abs, err := filepath.Abs(outputDir); err == nil {
			outputDir = abs
		}
		injected = append(injected, "-test.outputdir="+outputDir)
	}
	// Injected flags come before user flags, like cmd/go, so an explicit -v
	// (or -v=false) wins over the test2json default.
	plan.TestFlags = append(plan.TestFlags, injected...)
	plan.TestFlags = append(plan.TestFlags, state.testFlags...)
	plan.Strip = opts.Strip && !hasLDFlags(plan.BuildFlags) && !goflagsHasLDFlags(opts.GOFLAGS)
	return plan, nil
}

// planFlagState accumulates the classification of one `go test` argument list.
type planFlagState struct {
	plan           *BuildPlan
	args           []string
	testFlags      []string
	userSetCount   bool
	userSetTimeout bool
	needOutputDir  bool
	setP           bool
}

// apply classifies one flag. It returns the index of the last argument it
// consumed, because a value flag in split form also consumes the next argument.
func (s *planFlagState) apply(i int, arg, name, value string, hasValue bool) (int, error) {
	switch classifyFlag(name) {
	case flagBuildBool:
		s.plan.BuildFlags = append(s.plan.BuildFlags, arg)
	case flagBuildValue:
		val, next, err := consumeValue(s.args, i, name, value, hasValue)
		if err != nil {
			return i, err
		}
		i = next
		if !hasValue {
			s.plan.BuildFlags = append(s.plan.BuildFlags, arg, val)
		} else {
			s.plan.BuildFlags = append(s.plan.BuildFlags, arg)
		}
		if name == "-p" {
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				s.plan.P = n
				s.setP = true
			}
		}
	case flagTestBool:
		if !hasValue {
			value = "true"
		}
		s.testFlags = append(s.testFlags, "-test."+strings.TrimPrefix(name, "-")+"="+value)
		// cmd/go injects -test.outputdir when -artifacts asks the binary to
		// write files, because the binary otherwise uses its own working
		// directory (the package dir) instead of the go command's.
		if name == "-artifacts" && boolFlagEnabled(value) {
			s.needOutputDir = true
		}
	case flagTestValue:
		val, next, err := consumeValue(s.args, i, name, value, hasValue)
		if err != nil {
			return i, err
		}
		i = next
		s.testFlags = append(s.testFlags, "-test."+strings.TrimPrefix(name, "-")+"="+val)
		switch name {
		case "-count":
			s.userSetCount = true
		case "-timeout":
			s.userSetTimeout = true
		}
	case flagTestOptional:
		if !hasValue {
			value = "on"
		}
		s.testFlags = append(s.testFlags, "-test.shuffle="+value)
	case flagVet:
		val, next, err := consumeValue(s.args, i, name, value, hasValue)
		if err != nil {
			return i, err
		}
		i = next
		s.plan.LinkFlags = append(s.plan.LinkFlags, "-vet="+val)
	case flagIgnore:
		// The harness supplies -json itself.
	case flagFallback:
		s.plan.Fallback = &Fallback{
			Token:  strings.TrimPrefix(name, "-"),
			Reason: name + " needs go test",
		}
	default:
		s.plan.Fallback = &Fallback{
			Token:  sanitizeToken(strings.TrimPrefix(name, "-")),
			Reason: "unknown flag " + name,
		}
	}
	return i, nil
}

func boolFlagEnabled(value string) bool {
	enabled, err := strconv.ParseBool(value)
	return err == nil && enabled
}

const defaultGoTestTimeout = 10 * time.Minute

type flagKind uint8

const (
	flagUnknown flagKind = iota
	flagBuildBool
	flagBuildValue
	flagTestBool
	flagTestValue
	flagTestOptional
	flagVet
	flagIgnore
	flagFallback
)

var flagKinds = func() map[string]flagKind {
	m := make(map[string]flagKind)

	for _, name := range []string{
		"a", "race", "msan", "asan", "work", "x", "modcacherw", "trimpath", "linkshared",
	} {
		m["-"+name] = flagBuildBool
	}
	for _, name := range []string{
		"p", "asmflags", "buildmode", "buildvcs", "compiler", "gccgoflags", "gcflags",
		"installsuffix", "ldflags", "mod", "modfile", "overlay", "pgo", "pkgdir", "tags",
		"toolexec",
	} {
		m["-"+name] = flagBuildValue
	}
	for _, name := range []string{"artifacts", "benchmem", "failfast", "fullpath", "short", "v"} {
		m["-"+name] = flagTestBool
	}
	for _, name := range []string{
		"bench", "benchtime", "count", "cpu", "list", "parallel", "run", "skip", "timeout",
	} {
		m["-"+name] = flagTestValue
	}
	m["-shuffle"] = flagTestOptional
	m["-vet"] = flagVet
	m["-json"] = flagIgnore
	for _, name := range []string{
		"C", "n", "cover", "covermode", "coverpkg", "exec", "c", "o", "outputdir",
		"fuzz", "fuzztime", "fuzzminimizetime", "blockprofile", "blockprofilerate",
		"coverprofile", "cpuprofile", "memprofile", "memprofilerate", "mutexprofile",
		"mutexprofilefraction", "trace",
	} {
		m["-"+name] = flagFallback
	}
	return m
}()

func classifyFlag(name string) flagKind {
	if kind, ok := flagKinds[name]; ok {
		return kind
	}
	return flagUnknown
}

func splitAtArgs(args []string) (before, passthrough []string) {
	for i, arg := range args {
		if arg == "-args" || arg == "--args" {
			return args[:i], args[i+1:]
		}
	}
	return args, nil
}

// splitFlag returns a canonical single-dash flag name, its inline value, and
// whether a value was attached with "=". Non-flags return an empty name.
func splitFlag(arg string) (name, value string, hasValue bool) {
	if arg == "" || arg[0] != '-' || arg == "-" {
		return "", "", false
	}
	trimmed := strings.TrimPrefix(arg, "-")
	trimmed = strings.TrimPrefix(trimmed, "-")
	if trimmed == "" {
		return "", "", false
	}
	if i := strings.IndexByte(trimmed, '='); i >= 0 {
		return "-" + trimmed[:i], trimmed[i+1:], true
	}
	return "-" + trimmed, "", false
}

func consumeValue(args []string, i int, name, inline string, hasValue bool) (value string, next int, err error) {
	if hasValue {
		return inline, i, nil
	}
	if i+1 >= len(args) {
		return "", i, fmt.Errorf("invalid go test arguments: %s must be followed by a value", name)
	}
	return args[i+1], i + 1, nil
}

// riskyResourceEnv reports the first resource environment key that changes build
// inputs. A resource is applied per worker, so a single build cannot honor it.
func riskyResourceEnv(env []string) string {
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		switch {
		case key == "GOFLAGS", key == "GOOS", key == "GOARCH", key == "GOEXPERIMENT",
			key == "CC", key == "CXX", strings.HasPrefix(key, "CGO_"):
			return key
		}
	}
	return ""
}

func hasLDFlags(buildFlags []string) bool {
	for _, arg := range buildFlags {
		name, _, _ := splitFlag(arg)
		if name == "-ldflags" {
			return true
		}
	}
	return false
}

func goflagsHasLDFlags(goflags string) bool {
	for arg := range strings.FieldsSeq(goflags) {
		name, _, _ := splitFlag(arg)
		if name == "-ldflags" {
			return true
		}
	}
	return false
}

func sanitizeToken(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}
