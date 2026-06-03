package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/smartcontractkit/testrig/internal/hooks"
)

func TestIsHelpRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "short help", args: []string{"-h"}, want: true},
		{name: "long help", args: []string{"--help"}, want: true},
		{name: "go test args", args: []string{"-v", "./..."}, want: false},
		{name: "after separator", args: []string{"--", "-h", "./..."}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, isHelpRequest(tc.args))
		})
	}
}

func newRootArgsTestCmd(t *testing.T) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "testrig"}
	cmd.PersistentFlags().Bool("ai-output", false, "")
	hooks.RegisterPersistentFlags(cmd.PersistentFlags())
	return cmd
}

func TestGoTestArgsFromRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		want       []string
		wantGlobal string
		wantAIOut  bool
	}{
		{
			name:      "go test verbose after persistent",
			args:      []string{"--ai-output", "-v", "./pkg"},
			want:      []string{"-v", "./pkg"},
			wantAIOut: true,
		},
		{
			name: "go test count flag",
			args: []string{"-count=1", "./..."},
			want: []string{"-count=1", "./..."},
		},
		{
			name: "go test run flag",
			args: []string{"-run", "^TestFoo$", "./pkg"},
			want: []string{"-run", "^TestFoo$", "./pkg"},
		},
		{
			name:       "global setup then go test flags",
			args:       []string{"--global-setup", "docker compose up -d", "-run", "^TestFoo$", "./pkg"},
			want:       []string{"-run", "^TestFoo$", "./pkg"},
			wantGlobal: "docker compose up -d",
		},
		{
			name:       "global setup equals form",
			args:       []string{"--global-setup=docker compose up -d", "-v", "./..."},
			want:       []string{"-v", "./..."},
			wantGlobal: "docker compose up -d",
		},
		{
			name:      "double dash forwards remainder including go test help",
			args:      []string{"--ai-output", "--", "-h", "./pkg"},
			want:      []string{"--", "-h", "./pkg"},
			wantAIOut: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd := newRootArgsTestCmd(t)

			got, err := goTestArgsFromRoot(cmd, tc.args)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)

			if tc.wantAIOut {
				v, err := cmd.PersistentFlags().GetBool("ai-output")
				require.NoError(t, err)
				assert.True(t, v)
			}
			if tc.wantGlobal != "" {
				v, err := cmd.PersistentFlags().GetString("global-setup")
				require.NoError(t, err)
				assert.Equal(t, tc.wantGlobal, v)
			}
		})
	}
}

func TestRunRootAfterParsingAppliesFlagsBeforeHooks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	marker := filepath.Join(dir, "setup")
	cmd := newRootArgsTestCmd(t)
	cmd.SetContext(context.Background())

	err := runRootAfterParsing(
		cmd,
		[]string{"--global-setup", touchCmd(marker)},
		hooks.RunOptions{},
		func([]string) error {
			return nil
		},
	)
	require.NoError(t, err)
	assert.FileExists(t, marker)
}

func TestRunRootAfterParsingHelpSkipsHooks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	marker := filepath.Join(dir, "setup")
	cmd := newRootArgsTestCmd(t)
	cmd.SetContext(context.Background())

	err := runRootAfterParsing(
		cmd,
		[]string{"--global-setup", touchCmd(marker), "-h"},
		hooks.RunOptions{},
		func([]string) error {
			return nil
		},
	)
	require.ErrorIs(t, err, pflag.ErrHelp)
	_, statErr := os.Stat(marker)
	assert.True(t, os.IsNotExist(statErr))
}
