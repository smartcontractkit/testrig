package modresolve_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/modresolve"
)

func makeTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "go.mod"),
		[]byte("module github.com/smartcontractkit/chainlink/v2\n"),
		0600,
	))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "core", "store"), 0700))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "deployment", "ccip"), 0700))
	require.NoError(t, os.WriteFile(
		filepath.Join(root, "deployment", "go.mod"),
		[]byte("module github.com/smartcontractkit/chainlink/deployment\n"),
		0600,
	))
	return root
}

func TestResolveArgs(t *testing.T) {
	t.Parallel()
	root := makeTestRepo(t)

	tests := []struct {
		name       string
		goTestArgs []string
		wantDir    string
		wantArgs   []string
		wantErr    bool
	}{
		{
			name:       "core package stays at repo root",
			goTestArgs: []string{"./core/..."},
			wantDir:    root,
			wantArgs:   []string{"./core/..."},
		},
		{
			name:       "deployment top-level rewrites pattern to dot-slash-dot-dot-dot",
			goTestArgs: []string{"./deployment/..."},
			wantDir:    filepath.Join(root, "deployment"),
			wantArgs:   []string{"./..."},
		},
		{
			name:       "deployment subdirectory rewrites pattern relative to deployment root",
			goTestArgs: []string{"./deployment/ccip/..."},
			wantDir:    filepath.Join(root, "deployment"),
			wantArgs:   []string{"./ccip/..."},
		},
		{
			name:       "flags before pattern are preserved unchanged",
			goTestArgs: []string{"-v", "-timeout=10m", "./deployment/..."},
			wantDir:    filepath.Join(root, "deployment"),
			wantArgs:   []string{"-v", "-timeout=10m", "./..."},
		},
		{
			name:       "dot-slash-dot-dot-dot at repo root stays at repo root",
			goTestArgs: []string{"./..."},
			wantDir:    root,
			wantArgs:   []string{"./..."},
		},
		{
			name:       "no package patterns returns repo root unchanged",
			goTestArgs: []string{"-v", "-count=1"},
			wantDir:    root,
			wantArgs:   []string{"-v", "-count=1"},
		},
		{
			name:       "mixed core and deployment patterns error",
			goTestArgs: []string{"./core/...", "./deployment/..."},
			wantErr:    true,
		},
		{
			name:       "specific deployment package without wildcard",
			goTestArgs: []string{"./deployment/ccip"},
			wantDir:    filepath.Join(root, "deployment"),
			wantArgs:   []string{"./ccip"},
		},
		{
			name:       "import path only stays at repo root",
			goTestArgs: []string{"-v", "github.com/smartcontractkit/chainlink/deployment/..."},
			wantDir:    root,
			wantArgs:   []string{"-v", "github.com/smartcontractkit/chainlink/deployment/..."},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, args, err := modresolve.ResolveArgs(root, tc.goTestArgs)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantDir, dir)
			require.Equal(t, tc.wantArgs, args)
		})
	}
}

func TestResolvePatterns(t *testing.T) {
	t.Parallel()
	root := makeTestRepo(t)
	depDir := filepath.Join(root, "deployment")

	tests := []struct {
		name     string
		patterns []string
		wantDir  string
		wantPats []string
		wantErr  bool
	}{
		{
			name:     "core stays at root",
			patterns: []string{"./core/..."},
			wantDir:  root,
			wantPats: []string{"./core/..."},
		},
		{
			name:     "deployment top-level rewrites",
			patterns: []string{"./deployment/..."},
			wantDir:  depDir,
			wantPats: []string{"./..."},
		},
		{
			name:     "deployment subpackage rewrites",
			patterns: []string{"./deployment/ccip/..."},
			wantDir:  depDir,
			wantPats: []string{"./ccip/..."},
		},
		{
			name:     "cross-module error",
			patterns: []string{"./core/...", "./deployment/..."},
			wantErr:  true,
		},
		{
			name:     "import path only stays at repo root",
			patterns: []string{"github.com/smartcontractkit/chainlink/deployment/..."},
			wantDir:  root,
			wantPats: []string{"github.com/smartcontractkit/chainlink/deployment/..."},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, pats, err := modresolve.ResolvePatterns(root, tc.patterns)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantDir, dir)
			require.Equal(t, tc.wantPats, pats)
		})
	}
}

func TestNearestModuleRoot(t *testing.T) {
	t.Parallel()
	root := makeTestRepo(t)

	tests := []struct {
		name    string
		dir     string
		wantDir string
	}{
		{name: "repo root", dir: root, wantDir: root},
		{name: "core no intermediate go.mod", dir: filepath.Join(root, "core", "store"), wantDir: root},
		{name: "deployment dir", dir: filepath.Join(root, "deployment"), wantDir: filepath.Join(root, "deployment")},
		{
			name:    "deployment subdir",
			dir:     filepath.Join(root, "deployment", "ccip"),
			wantDir: filepath.Join(root, "deployment"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := modresolve.NearestModuleRoot(tc.dir, root)
			require.NoError(t, err)
			require.Equal(t, tc.wantDir, got)
		})
	}
}

func TestNearestModuleRootNoGoMod(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "core"), 0700))

	_, err := modresolve.NearestModuleRoot(filepath.Join(root, "core"), root)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no go.mod found")
}

func TestResolveArgsNoGoMod(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	_, _, err := modresolve.ResolveArgs(root, []string{"./core/..."})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no go.mod found")
}

func TestResolvePatternsNoGoMod(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	_, _, err := modresolve.ResolvePatterns(root, []string{"./core/..."})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no go.mod found")
}

func TestResolveArgsRewrittenPatternsUseForwardSlashes(t *testing.T) {
	t.Parallel()
	root := makeTestRepo(t)

	_, args, err := modresolve.ResolveArgs(root, []string{"./deployment/ccip/..."})
	require.NoError(t, err)
	for _, arg := range args {
		if strings.Contains(arg, "/") || strings.HasPrefix(arg, "./") {
			require.NotContains(t, arg, `\`, "pattern %q should use forward slashes", arg)
		}
	}
}
