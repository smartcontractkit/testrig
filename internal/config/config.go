// Package config holds the harness's flag-bound application config. It binds
// Cobra flags via Viper and exposes a single App struct consumed by the runner.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

// App is the flag-bound configuration shared across testrig subcommands.
type App struct {
	RepoRoot           string        `mapstructure:"repo_root"`
	AIOutput           bool          `mapstructure:"ai_output"`
	Iterations         int           `mapstructure:"iterations"`
	ParallelIterations int           `mapstructure:"parallel_iterations"`
	SlowThreshold      time.Duration `mapstructure:"slow_threshold"`
	FailFast           bool          `mapstructure:"fail_fast"`
	FailFastOn         []string      `mapstructure:"fail_fast_on"`
	Shuffle            bool          `mapstructure:"shuffle_seed"`
	IterationSetup     string        `mapstructure:"iteration_setup"`
	IterationTeardown  string        `mapstructure:"iteration_teardown"`
}

// Valid values for --fail-fast-on.
const (
	FailFastOnAny     = "any"
	FailFastOnFailure = "failure"
	FailFastOnTimeout = "timeout"
	FailFastOnSlow    = "slow"
)

var validFailFastOn = map[string]struct{}{
	FailFastOnAny:     {},
	FailFastOnFailure: {},
	FailFastOnTimeout: {},
	FailFastOnSlow:    {},
}

// ValidateDiagnose checks diagnose-mode invariants on conf and normalizes
// FailFastOn in place. Call after Load.
func ValidateDiagnose(conf *App) error {
	if conf.Iterations < 1 {
		return errors.New("--iterations must be >= 1")
	}
	if conf.ParallelIterations < 1 {
		return errors.New("--parallel-iterations must be >= 1")
	}
	if conf.ParallelIterations > conf.Iterations {
		return errors.New("--parallel-iterations must be <= --iterations")
	}
	failFastOn, err := NormalizeFailFastOn(conf.FailFastOn)
	if err != nil {
		return err
	}
	conf.FailFastOn = failFastOn
	return nil
}

// NormalizeFailFastOn validates --fail-fast-on values and returns lowercase,
// de-duplicated categories in first-seen order.
func NormalizeFailFastOn(values []string) ([]string, error) {
	var out []string
	seen := make(map[string]struct{})
	for _, value := range values {
		for part := range strings.SplitSeq(value, ",") {
			category := strings.ToLower(strings.TrimSpace(part))
			if category == "" {
				return nil, errors.New(
					`--fail-fast-on must contain only "any", "failure", "timeout", or "slow"; got ""`,
				)
			}
			if _, ok := validFailFastOn[category]; !ok {
				return nil, fmt.Errorf(
					`--fail-fast-on must contain only "any", "failure", "timeout", or "slow"; got %q`,
					category,
				)
			}
			if _, ok := seen[category]; ok {
				continue
			}
			seen[category] = struct{}{}
			out = append(out, category)
		}
	}
	return out, nil
}

// Load binds Viper to the active command's persistent flags and local flags, then unmarshals into App.
func Load(cmd *cobra.Command) (*App, error) {
	if cmd == nil {
		return nil, errors.New("command is required")
	}
	v := viper.New()

	if cwd, err := os.Getwd(); err == nil {
		v.SetDefault("repo_root", cwd)
	}
	// Enable sparse output when stdout is not a TTY (e.g. redirected or CI).
	v.SetDefault("ai_output", !term.IsTerminal(os.Stdout.Fd()))
	v.SetDefault("iterations", 1)
	v.SetDefault("parallel_iterations", 1)
	v.SetDefault("slow_threshold", 30*time.Second)
	v.SetDefault("fail_fast", false)
	v.SetDefault("fail_fast_on", []string{})

	if err := bindPFlags(v, cmd.PersistentFlags()); err != nil {
		return nil, err
	}
	if err := bindPFlags(v, cmd.Flags()); err != nil {
		return nil, err
	}

	var conf App
	if err := v.Unmarshal(&conf); err != nil {
		return nil, err
	}
	return &conf, nil
}

func bindPFlags(v *viper.Viper, flags *pflag.FlagSet) error {
	var errs []error
	flags.VisitAll(func(f *pflag.Flag) {
		configName := strings.ReplaceAll(f.Name, "-", "_")
		if bindErr := v.BindPFlag(configName, f); bindErr != nil {
			errs = append(errs, bindErr)
		}
	})
	return errors.Join(errs...)
}
