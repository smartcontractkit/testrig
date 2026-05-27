package config

import (
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateDiagnose(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		conf    *App
		wantErr string
	}{
		{
			name: "valid default",
			conf: &App{Iterations: 1, ParallelIterations: 1},
		},
		{
			name:    "invalid iterations",
			conf:    &App{Iterations: 0, ParallelIterations: 1},
			wantErr: "--iterations must be >= 1",
		},
		{
			name:    "invalid parallel iterations",
			conf:    &App{Iterations: 1, ParallelIterations: 0},
			wantErr: "--parallel-iterations must be >= 1",
		},
		{
			name:    "parallel iterations cannot exceed iterations",
			conf:    &App{Iterations: 2, ParallelIterations: 3},
			wantErr: "--parallel-iterations must be <= --iterations",
		},
		{
			name:    "invalid fail fast category",
			conf:    &App{Iterations: 1, ParallelIterations: 1, FailFastOn: []string{"timeout", "banana"}},
			wantErr: `--fail-fast-on must contain only "any", "failure", "timeout", or "slow"; got "banana"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateDiagnose(tc.conf)
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestLoadBindsPersistentAndLocalFlags(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{Use: "root"}
	root.PersistentFlags().Bool("ai-output", false, "")
	root.PersistentFlags().String("iteration-setup", "", "")
	sub := &cobra.Command{
		Use: "sub",
		Run: func(*cobra.Command, []string) {},
	}
	sub.Flags().Int("iterations", 1, "")
	sub.Flags().Int("parallel-iterations", 1, "")
	sub.Flags().StringSlice("fail-fast-on", nil, "")
	sub.Flags().Bool("shuffle-seed", false, "")
	root.AddCommand(sub)
	root.SetArgs([]string{
		"sub",
		"--ai-output",
		"--iteration-setup", "echo setup",
		"--iterations", "7",
		"--parallel-iterations", "3",
		"--fail-fast-on", "timeout,slow",
		"--shuffle-seed",
	})

	cmd, err := root.ExecuteC()
	require.NoError(t, err)

	conf, err := Load(cmd)
	require.NoError(t, err)
	assert.True(t, conf.AIOutput)
	assert.Equal(t, "echo setup", conf.IterationSetup)
	assert.Equal(t, 7, conf.Iterations)
	assert.Equal(t, 3, conf.ParallelIterations)
	assert.Equal(t, []string{"timeout", "slow"}, conf.FailFastOn)
	assert.True(t, conf.Shuffle)
}

func TestLoadDefaultsWhenFlagsMissing(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{Use: "root"}
	sub := &cobra.Command{
		Use: "sub",
		Run: func(*cobra.Command, []string) {},
	}
	root.AddCommand(sub)
	root.SetArgs([]string{"sub"})

	cmd, err := root.ExecuteC()
	require.NoError(t, err)

	conf, err := Load(cmd)
	require.NoError(t, err)
	assert.Equal(t, 1, conf.Iterations)
	assert.Equal(t, 1, conf.ParallelIterations)
	assert.Equal(t, 30*time.Second, conf.SlowThreshold)
	assert.False(t, conf.FailFast)
	assert.Nil(t, conf.FailFastOn)
	assert.False(t, conf.Shuffle)
}
