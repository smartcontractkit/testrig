package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
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

func TestRootCommandHasGlobalHookFlags(t *testing.T) {
	t.Parallel()
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("global-setup"), "--global-setup flag missing")
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("global-teardown"), "--global-teardown flag missing")
}

func TestRootCommandHasIterationHookFlags(t *testing.T) {
	t.Parallel()
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("iteration-setup"), "--iteration-setup flag missing")
	assert.NotNil(t, rootCmd.PersistentFlags().Lookup("iteration-teardown"), "--iteration-teardown flag missing")
}
