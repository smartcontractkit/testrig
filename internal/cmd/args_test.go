package cmd

import (
	"testing"

	"github.com/spf13/cobra"
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

func TestGoTestArgsFromRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "go test verbose after persistent",
			args: []string{"--ai-output", "-v", "./pkg"},
			want: []string{"-v", "./pkg"},
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd := &cobra.Command{Use: "testrig"}
			cmd.PersistentFlags().Bool("ai-output", false, "")
			hooks.RegisterPersistentFlags(cmd.PersistentFlags())

			got, err := goTestArgsFromRoot(cmd, tc.args)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			if tc.name == "go test verbose after persistent" {
				v, err := cmd.PersistentFlags().GetBool("ai-output")
				require.NoError(t, err)
				assert.True(t, v)
			}
		})
	}
}
