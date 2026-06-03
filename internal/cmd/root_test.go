package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/hooks"
)

func TestRootCommandName(t *testing.T) {
	t.Parallel()
	rootCmd := NewRootCommand(hooks.RunOptions{})

	if got := rootCmd.Name(); got != "testrig" {
		t.Fatalf("root Name: got %q want %q", got, "testrig")
	}
}

func TestSubcommandCommandPaths(t *testing.T) {
	t.Parallel()
	rootCmd := NewRootCommand(hooks.RunOptions{})

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
	rootCmd := NewRootCommand(hooks.RunOptions{})
	for _, c := range rootCmd.Commands() {
		assert.NotEqual(t, "run", c.Name(), "run subcommand should be removed")
	}
}

func TestRootDisableFlagParsing(t *testing.T) {
	t.Parallel()
	rootCmd := NewRootCommand(hooks.RunOptions{})
	assert.True(t, rootCmd.DisableFlagParsing)
}

func TestRootHelpShowsTestrigUsage(t *testing.T) {
	t.Parallel()
	rootCmd := NewRootCommand(hooks.RunOptions{})
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"-h"})

	_, err := rootCmd.ExecuteC()
	require.NoError(t, err)

	out := buf.String()
	assert.True(t, strings.Contains(out, "gotestsum") || strings.Contains(out, "diagnose"),
		"help should describe testrig subcommands, got: %q", out)
	assert.NotContains(t, out, "go help testflag")
}

func TestApplyRootCommandCustomName(t *testing.T) {
	t.Parallel()
	rootCmd := NewRootCommand(hooks.RunOptions{
		RootCommand: "cltest",
	})

	assert.Equal(t, "cltest", rootCmd.Name())
	assert.Contains(t, rootCmd.Example, "cltest -v")
	assert.NotContains(t, rootCmd.Example, "cltest run")
	assert.NotContains(t, rootCmd.Example, "testrig run")
	assert.Contains(t, rootCmd.Long, "cltest --ai-output -v")

	var gotestsum *cobra.Command
	var diagnoseCmd *cobra.Command
	for _, c := range rootCmd.Commands() {
		switch c.Name() {
		case "gotestsum":
			gotestsum = c
		case "diagnose":
			diagnoseCmd = c
		}
	}
	require.NotNil(t, gotestsum)
	require.NotNil(t, diagnoseCmd)
	assert.Equal(t, "cltest gotestsum", gotestsum.CommandPath())
	assert.Contains(t, gotestsum.Long, "cltest --ai-output gotestsum")
	assert.Contains(t, diagnoseCmd.Example, "cltest diagnose")
}

func TestApplyRootCommandEmptyUsesDefault(t *testing.T) {
	t.Parallel()
	rootCmd := NewRootCommand(hooks.RunOptions{})
	assert.Equal(t, "testrig", rootCmd.Name())
}

func TestRootCLIName(t *testing.T) {
	t.Parallel()
	rootCmd := NewRootCommand(hooks.RunOptions{RootCommand: "cltest"})
	assert.Equal(t, "cltest", rootCLIName(rootCmd))

	var gotestsum *cobra.Command
	for _, c := range rootCmd.Commands() {
		if c.Name() == "gotestsum" {
			gotestsum = c
			break
		}
	}
	require.NotNil(t, gotestsum)
	assert.Equal(t, "cltest", rootCLIName(gotestsum))
}

func TestNewRootCommandWithRunOptions(t *testing.T) {
	t.Parallel()
	custom := &cobra.Command{Use: "db", Short: "database tools"}
	rootCmd := NewRootCommand(hooks.RunOptions{
		RootCommand: "cltest",
		Commands:    []*cobra.Command{custom},
		RootFlags: func(fs *pflag.FlagSet) {
			fs.String("db-url", "", "database URL")
		},
	})

	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("db-url"))
	var foundDB bool
	for _, c := range rootCmd.Commands() {
		if c.Name() == "db" {
			foundDB = true
		}
	}
	assert.True(t, foundDB, "custom subcommand should be registered")
}

func TestRootCommandHasCatalogHookFlags(t *testing.T) {
	t.Parallel()
	rootCmd := NewRootCommand(hooks.RunOptions{})
	for _, e := range hooks.Catalog {
		assert.NotNilf(t, rootCmd.PersistentFlags().Lookup(e.Flag), "flag --%s missing", e.Flag)
	}
}
