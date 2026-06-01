package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/hooks"
)

func TestRootCommandName(t *testing.T) {
	t.Parallel()

	if got := rootCmd.Name(); got != "testrig" {
		t.Fatalf("root Name: got %q want %q", got, "testrig")
	}
}

func TestSubcommandCommandPaths(t *testing.T) {
	t.Parallel()

	var gotestsum *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "gotestsum" {
			gotestsum = c
			break
		}
	}
	if gotestsum == nil {
		t.Fatal("gotestsum subcommand not found")
	}
	want := "testrig gotestsum"
	if got := gotestsum.CommandPath(); got != want {
		t.Fatalf("gotestsum CommandPath: got %q want %q", got, want)
	}
}

func TestRootNoRunSubcommand(t *testing.T) {
	t.Parallel()
	for _, c := range rootCmd.Commands() {
		assert.NotEqual(t, "run", c.Name(), "run subcommand should be removed")
	}
}

func TestRootDisableFlagParsing(t *testing.T) {
	t.Parallel()
	assert.True(t, rootCmd.DisableFlagParsing)
}

//nolint:paralleltest // mutates package-level rootCmd IO and args
func TestRootHelpShowsTestrigUsage(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"-h"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	_, err := rootCmd.ExecuteC()
	require.NoError(t, err)

	out := buf.String()
	assert.True(t, strings.Contains(out, "gotestsum") || strings.Contains(out, "diagnose"),
		"help should describe testrig subcommands, got: %q", out)
	assert.NotContains(t, out, "go help testflag")
}

//nolint:paralleltest // mutates package-level cobra commands
func TestApplyRootCommandCustomName(t *testing.T) {
	applyRootCommand("cltest")
	t.Cleanup(func() { applyRootCommand("") })

	assert.Equal(t, "cltest", rootCmd.Name())
	assert.Contains(t, rootCmd.Example, "cltest -v")
	assert.NotContains(t, rootCmd.Example, "cltest run")
	assert.NotContains(t, rootCmd.Example, "testrig run")
	assert.Contains(t, rootCmd.Long, "cltest --ai-output -v")

	var gotestsum *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "gotestsum" {
			gotestsum = c
			break
		}
	}
	require.NotNil(t, gotestsum)
	assert.Equal(t, "cltest gotestsum", gotestsum.CommandPath())
	assert.Contains(t, gotestsum.Long, "cltest --ai-output gotestsum")
	assert.Contains(t, diagnoseCmd.Example, "cltest diagnose")
}

//nolint:paralleltest // mutates package-level cobra commands
func TestApplyRootCommandEmptyUsesDefault(t *testing.T) {
	applyRootCommand("")
	assert.Equal(t, "testrig", rootCmd.Name())
}

func TestRootCommandHasCatalogHookFlags(t *testing.T) {
	t.Parallel()
	for _, e := range hooks.Catalog {
		assert.NotNilf(t, rootCmd.PersistentFlags().Lookup(e.Flag), "flag --%s missing", e.Flag)
	}
}
