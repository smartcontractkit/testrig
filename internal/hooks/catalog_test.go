package hooks

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCatalogMatchesPublicHooksGo(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	names, err := publicHookRegistrarNames(filepath.Join(root, "hooks.go"))
	require.NoError(t, err)

	catalog := make(map[string]struct{}, len(Catalog))
	for _, e := range Catalog {
		catalog[e.Name] = struct{}{}
	}

	assert.Equal(t, catalogNamesSet(), names, "catalog must match hooks.go Option registrars")
	for name := range names {
		_, ok := catalog[name]
		assert.True(t, ok, "hooks.go registrar %q missing from Catalog", name)
	}
}

func TestRunOptionsHookCoversCatalog(t *testing.T) {
	t.Parallel()

	for _, e := range Catalog {
		t.Run(e.Name, func(t *testing.T) {
			t.Parallel()
			// Hook() must recognize every catalog name (compile-time switch in run_options.go).
			_ = RunOptions{}.Hook(e.Name)
		})
	}
}

func TestCatalogFlagMatchesKebabName(t *testing.T) {
	t.Parallel()

	for _, e := range Catalog {
		assert.Equal(t, camelToKebab(e.Name), e.Flag, "flag for %s", e.Name)
	}
}

func catalogNamesSet() map[string]struct{} {
	m := make(map[string]struct{}, len(Catalog))
	for _, e := range Catalog {
		m[e.Name] = struct{}{}
	}
	return m
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, parent, dir)
		dir = parent
	}
}

func publicHookRegistrarNames(hooksGoPath string) (map[string]struct{}, error) {
	src, err := os.ReadFile(hooksGoPath) //nolint:gosec // G304: test reads repo hooks.go
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, hooksGoPath, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	names := make(map[string]struct{})
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name == nil || fn.Doc == nil {
			continue
		}
		if !registrarDoc(fn.Doc.Text()) {
			continue
		}
		names[fn.Name.Name] = struct{}{}
	}
	return names, nil
}

func registrarDoc(doc string) bool {
	return strings.Contains(doc, " registers a hook to ")
}
