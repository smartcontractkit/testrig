package gobuild

import (
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlagClassification(t *testing.T) {
	t.Parallel()

	tests := map[string]flagKind{
		// go help build
		"-a":             flagBuildBool,
		"-n":             flagFallback,
		"-p":             flagBuildValue,
		"-race":          flagBuildBool,
		"-msan":          flagBuildBool,
		"-asan":          flagBuildBool,
		"-cover":         flagFallback,
		"-covermode":     flagFallback,
		"-coverpkg":      flagFallback,
		"-v":             flagTestBool,
		"-work":          flagBuildBool,
		"-x":             flagBuildBool,
		"-asmflags":      flagBuildValue,
		"-buildmode":     flagBuildValue,
		"-buildvcs":      flagBuildValue,
		"-compiler":      flagBuildValue,
		"-gccgoflags":    flagBuildValue,
		"-gcflags":       flagBuildValue,
		"-installsuffix": flagBuildValue,
		"-json":          flagIgnore,
		"-ldflags":       flagBuildValue,
		"-linkshared":    flagBuildBool,
		"-mod":           flagBuildValue,
		"-modcacherw":    flagBuildBool,
		"-modfile":       flagBuildValue,
		"-overlay":       flagBuildValue,
		"-pgo":           flagBuildValue,
		"-pkgdir":        flagBuildValue,
		"-tags":          flagBuildValue,
		"-trimpath":      flagBuildBool,
		"-toolexec":      flagBuildValue,
		"-C":             flagFallback,
		"-o":             flagFallback,
		"-c":             flagFallback,

		// go help testflag
		"-artifacts":            flagTestBool,
		"-bench":                flagTestValue,
		"-benchtime":            flagTestValue,
		"-count":                flagTestValue,
		"-cpu":                  flagTestValue,
		"-failfast":             flagTestBool,
		"-fullpath":             flagTestBool,
		"-fuzz":                 flagFallback,
		"-fuzztime":             flagFallback,
		"-fuzzminimizetime":     flagFallback,
		"-list":                 flagTestValue,
		"-outputdir":            flagFallback,
		"-parallel":             flagTestValue,
		"-run":                  flagTestValue,
		"-short":                flagTestBool,
		"-shuffle":              flagTestOptional,
		"-skip":                 flagTestValue,
		"-timeout":              flagTestValue,
		"-vet":                  flagVet,
		"-benchmem":             flagTestBool,
		"-blockprofile":         flagFallback,
		"-blockprofilerate":     flagFallback,
		"-coverprofile":         flagFallback,
		"-cpuprofile":           flagFallback,
		"-memprofile":           flagFallback,
		"-memprofilerate":       flagFallback,
		"-mutexprofile":         flagFallback,
		"-mutexprofilefraction": flagFallback,
		"-exec":                 flagFallback,
		"-trace":                flagFallback,

		// Unknown flags fall back instead of guessing.
		"-myflag": flagUnknown,
	}
	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, classifyFlag(name))
		})
	}
}

func TestPlanDefaults(t *testing.T) {
	t.Parallel()

	plan, err := Plan(PlanOptions{
		ModuleDir: ".",
		Args:      []string{"./pkg"},
		BuildOnce: true,
		Strip:     true,
	})
	require.NoError(t, err)
	require.Nil(t, plan.Fallback)
	assert.Equal(t, []string{"./pkg"}, plan.Patterns)
	assert.Equal(t, runtime.GOMAXPROCS(0), plan.P)
	assert.Contains(t, plan.BuildFlags, "-p="+strconv.Itoa(runtime.GOMAXPROCS(0)))
	assert.True(t, plan.Strip)
	for _, flag := range []string{
		"-test.paniconexit0",
		"-test.v=test2json",
		"-test.timeout=10m0s",
		"-test.count=1",
	} {
		assert.Contains(t, plan.TestFlags, flag)
	}
}

func TestPlanBuildAndTestFlags(t *testing.T) {
	t.Parallel()

	plan, err := Plan(PlanOptions{
		ModuleDir: ".",
		Args: []string{
			"-race",
			"-tags=foo",
			"-p", "4",
			"-run", "TestX",
			"-v",
			"-count", "3",
			"-timeout=2m",
			"-shuffle=42",
			"./pkg",
		},
		BuildOnce: true,
		Strip:     true,
	})
	require.NoError(t, err)
	require.Nil(t, plan.Fallback)
	assert.Equal(t, 4, plan.P)
	assert.Equal(t, []string{"./pkg"}, plan.Patterns)
	assert.Contains(t, plan.BuildFlags, "-race")
	assert.Contains(t, plan.BuildFlags, "-tags=foo")
	assert.Contains(t, plan.BuildFlags, "-p")
	assert.Contains(t, plan.BuildFlags, "4")
	assert.Contains(t, plan.TestFlags, "-test.run=TestX")
	assert.Contains(t, plan.TestFlags, "-test.v=true")
	assert.Contains(t, plan.TestFlags, "-test.v=test2json")
	assert.Less(
		t,
		slices.Index(plan.TestFlags, "-test.v=test2json"),
		slices.Index(plan.TestFlags, "-test.v=true"),
		"user -v must come after the injected test2json default, like cmd/go",
	)
	assert.Contains(t, plan.TestFlags, "-test.count=3")
	assert.Contains(t, plan.TestFlags, "-test.timeout=2m")
	assert.Contains(t, plan.TestFlags, "-test.shuffle=42")
	assert.NotContains(t, plan.TestFlags, "-test.count=1")
	assert.NotContains(t, plan.TestFlags, "-test.timeout=10m0s")
}

func TestPlanShuffleWithoutValue(t *testing.T) {
	t.Parallel()

	plan, err := Plan(PlanOptions{
		ModuleDir: ".",
		Args:      []string{"-shuffle", "./pkg"},
		BuildOnce: true,
	})
	require.NoError(t, err)
	assert.Contains(t, plan.TestFlags, "-test.shuffle=on")
}

func TestPlanPassthrough(t *testing.T) {
	t.Parallel()

	plan, err := Plan(PlanOptions{
		ModuleDir: ".",
		Args:      []string{"-run", "TestX", "./pkg", "-args", "-myflag", "value"},
		BuildOnce: true,
	})
	require.NoError(t, err)
	require.Nil(t, plan.Fallback)
	assert.Equal(t, []string{"./pkg"}, plan.Patterns)
	assert.Equal(t, []string{"-myflag", "value"}, plan.Passthrough)
	assert.NotContains(t, plan.TestFlags, "-test.myflag=value")
}

func TestPlanFallbacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		opts      PlanOptions
		wantToken string
		wantIn    string
	}{
		{
			name:      "build once off",
			opts:      PlanOptions{Args: []string{"./pkg"}},
			wantToken: "build-once-off",
			wantIn:    "--build-once=false",
		},
		{
			name:      "coverprofile",
			opts:      PlanOptions{Args: []string{"-coverprofile=cover.out", "./pkg"}, BuildOnce: true},
			wantToken: "coverprofile",
			wantIn:    "-coverprofile needs go test",
		},
		{
			name:      "unknown flag",
			opts:      PlanOptions{Args: []string{"-myflag", "./pkg"}, BuildOnce: true},
			wantToken: "myflag",
			wantIn:    "unknown flag -myflag",
		},
		{
			name: "resource env GOFLAGS",
			opts: PlanOptions{
				Args:        []string{"./pkg"},
				BuildOnce:   true,
				ResourceEnv: []string{"GOFLAGS=-tags=foo"},
			},
			wantToken: "goflags",
			wantIn:    "GOFLAGS",
		},
		{
			name: "resource env CGO_ENABLED",
			opts: PlanOptions{
				Args:        []string{"./pkg"},
				BuildOnce:   true,
				ResourceEnv: []string{"CGO_ENABLED=1"},
			},
			wantToken: "cgo_enabled",
			wantIn:    "CGO_ENABLED",
		},
		{
			name: "resource env CC",
			opts: PlanOptions{
				Args:        []string{"./pkg"},
				BuildOnce:   true,
				ResourceEnv: []string{"CC=clang"},
			},
			wantToken: "cc",
			wantIn:    "CC",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan, err := Plan(tc.opts)
			require.NoError(t, err)
			require.NotNil(t, plan.Fallback)
			assert.Equal(t, tc.wantToken, plan.Fallback.Token)
			assert.Contains(t, plan.Fallback.Reason, tc.wantIn)
		})
	}
}

func TestPlanStrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts PlanOptions
		want bool
	}{
		{
			name: "default strips",
			opts: PlanOptions{Args: []string{"./pkg"}, BuildOnce: true, Strip: true},
			want: true,
		},
		{
			name: "strip disabled",
			opts: PlanOptions{Args: []string{"./pkg"}, BuildOnce: true, Strip: false},
			want: false,
		},
		{
			name: "ldflags in args disables stripping",
			opts: PlanOptions{Args: []string{"-ldflags=-X main.x=1", "./pkg"}, BuildOnce: true, Strip: true},
			want: false,
		},
		{
			name: "ldflags split form disables stripping",
			opts: PlanOptions{Args: []string{"-ldflags", "-w", "./pkg"}, BuildOnce: true, Strip: true},
			want: false,
		},
		{
			name: "ldflags in GOFLAGS disables stripping",
			opts: PlanOptions{Args: []string{"./pkg"}, BuildOnce: true, Strip: true, GOFLAGS: "-ldflags=-s"},
			want: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan, err := Plan(tc.opts)
			require.NoError(t, err)
			assert.Equal(t, tc.want, plan.Strip)
		})
	}
}

func TestPlanArtifactsOutputDir(t *testing.T) {
	t.Parallel()

	moduleDir := t.TempDir()
	plan, err := Plan(PlanOptions{
		ModuleDir: moduleDir,
		Args:      []string{"-artifacts", "./pkg"},
		BuildOnce: true,
	})
	require.NoError(t, err)
	require.Nil(t, plan.Fallback)
	assert.Contains(t, plan.TestFlags, "-test.artifacts=true")

	abs, err := filepath.Abs(moduleDir)
	require.NoError(t, err)
	assert.Contains(
		t,
		plan.TestFlags,
		"-test.outputdir="+abs,
		"artifacts need the go command's working directory, like cmd/go",
	)
}

func TestPlanArtifactsOffNeedsNoOutputDir(t *testing.T) {
	t.Parallel()

	plan, err := Plan(PlanOptions{
		ModuleDir: t.TempDir(),
		Args:      []string{"-artifacts=false", "./pkg"},
		BuildOnce: true,
	})
	require.NoError(t, err)
	require.Nil(t, plan.Fallback)
	for _, flag := range plan.TestFlags {
		assert.NotContains(t, flag, "-test.outputdir=")
	}
}

func TestPlanMissingFlagValue(t *testing.T) {
	t.Parallel()

	_, err := Plan(PlanOptions{Args: []string{"-run"}, BuildOnce: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-run")
}
