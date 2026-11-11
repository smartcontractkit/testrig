package gobuild

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupPackagesAvoidsNameCollisions(t *testing.T) {
	t.Parallel()

	groups := groupPackages([]Package{
		{ImportPath: "example.com/a/util", Name: "util", BinaryName: "util"},
		{ImportPath: "example.com/b/util", Name: "util", BinaryName: "util"},
		{ImportPath: "example.com/c/solo", Name: "solo", BinaryName: "solo"},
	})
	require.Len(t, groups, 2)
	assert.Len(t, groups[0], 2)
	assert.Len(t, groups[1], 1)
	assert.Equal(t, "example.com/a/util", groups[0][0].ImportPath)
	assert.Equal(t, "example.com/c/solo", groups[0][1].ImportPath)
	assert.Equal(t, "example.com/b/util", groups[1][0].ImportPath)

	// Binary names come from import paths, so differing package clauses do not
	// save two packages with the same final path element.
	groups = groupPackages([]Package{
		{ImportPath: "example.com/a/util", Name: "alpha", BinaryName: "util"},
		{ImportPath: "example.com/b/util", Name: "beta", BinaryName: "util"},
	})
	assert.Len(t, groups, 2)

	// A package with no BinaryName still gets its name from the import path,
	// not the package clause.
	groups = groupPackages([]Package{
		{ImportPath: "example.com/a/foo", Name: "bar"},
		{ImportPath: "example.com/b/foo", Name: "baz"},
	})
	assert.Len(t, groups, 2)
}

func TestPackageBinaryNameUsesImportPath(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "foo", packageBinaryName(Package{ImportPath: "example.com/a/foo", Name: "bar"}))
	assert.Equal(
		t,
		"name",
		packageBinaryName(Package{ImportPath: "example.com/a/foo", BinaryName: "name", Name: "bar"}),
	)
}

func TestLinkNameCollisionFixture(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("compiles a fixture module")
	}

	moduleDir, err := filepath.Abs(filepath.Join("testdata", "namecollision"))
	require.NoError(t, err)

	plan, err := Plan(PlanOptions{
		ModuleDir: moduleDir,
		Args:      []string{"./..."},
		BuildOnce: true,
		Strip:     false,
	})
	require.NoError(t, err)
	require.Nil(t, plan.Fallback)
	plan.BinDir = t.TempDir()

	compiled, err := Compile(context.Background(), plan)
	require.NoError(t, err)
	require.Len(t, compiled.Packages, 2)
	assert.Equal(t, "util", compiled.Packages[0].Name)
	assert.Equal(t, "util", compiled.Packages[1].Name)

	build, err := Link(context.Background(), plan, compiled.Packages)
	require.NoError(t, err)
	require.Len(t, build.Binaries, 2)
	assert.Equal(t, 2, build.Metrics.Binaries)
	assert.False(t, build.Metrics.Stripped)
	assert.Positive(t, build.Metrics.LinkWall)

	dirs := map[string]bool{}
	for _, binary := range build.Binaries {
		assert.Equal(t, "util", binary.NodeName)
		assert.Equal(t, "util.test", filepath.Base(binary.Path))
		dirs[filepath.Dir(binary.Path)] = true
		info, err := os.Stat(binary.Path)
		require.NoError(t, err)
		assert.True(t, info.Mode().IsRegular())
	}
	assert.Len(t, dirs, 2, "colliding package names must link in separate groups")
}
