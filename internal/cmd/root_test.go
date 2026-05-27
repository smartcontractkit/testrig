package cmd

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"

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

func TestRootCommandHasCatalogHookFlags(t *testing.T) {
	t.Parallel()
	for _, e := range hooks.Catalog {
		assert.NotNilf(t, rootCmd.PersistentFlags().Lookup(e.Flag), "flag --%s missing", e.Flag)
	}
}
