// Package config holds the harness's flag-bound application config. It reads
// Cobra flags via a small registry and exposes a single App struct consumed by
// the runner.
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

	"github.com/smartcontractkit/testrig/internal/hooks"
)

// App is the flag-bound configuration shared across testrig subcommands.
type App struct {
	RepoRoot           string
	AIOutput           bool
	Iterations         int
	ParallelIterations int
	SlowThreshold      time.Duration
	FailFast           bool
	FailFastOn         []string
	Shuffle            bool
	IterationSetup     string
	IterationTeardown  string
	shellHooks         map[string]string
}

// ShellCommand returns the shell command bound to a catalog flag (e.g. iteration-setup).
func (a *App) ShellCommand(flag string) string {
	if a == nil || a.shellHooks == nil {
		return ""
	}
	return a.shellHooks[flag]
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

type flagBinder func(*App, *pflag.FlagSet) error

var flagRegistry = map[string]flagBinder{
	"ai-output": func(conf *App, flags *pflag.FlagSet) error {
		v, err := flags.GetBool("ai-output")
		if err != nil {
			return err
		}
		conf.AIOutput = v
		return nil
	},
	"iterations": func(conf *App, flags *pflag.FlagSet) error {
		v, err := flags.GetInt("iterations")
		if err != nil {
			return err
		}
		conf.Iterations = v
		return nil
	},
	"parallel-iterations": func(conf *App, flags *pflag.FlagSet) error {
		v, err := flags.GetInt("parallel-iterations")
		if err != nil {
			return err
		}
		conf.ParallelIterations = v
		return nil
	},
	"slow-threshold": func(conf *App, flags *pflag.FlagSet) error {
		v, err := flags.GetDuration("slow-threshold")
		if err != nil {
			return err
		}
		conf.SlowThreshold = v
		return nil
	},
	"fail-fast": func(conf *App, flags *pflag.FlagSet) error {
		v, err := flags.GetBool("fail-fast")
		if err != nil {
			return err
		}
		conf.FailFast = v
		return nil
	},
	"fail-fast-on": func(conf *App, flags *pflag.FlagSet) error {
		v, err := flags.GetStringSlice("fail-fast-on")
		if err != nil {
			return err
		}
		conf.FailFastOn = v
		return nil
	},
	"shuffle-seed": func(conf *App, flags *pflag.FlagSet) error {
		v, err := flags.GetBool("shuffle-seed")
		if err != nil {
			return err
		}
		conf.Shuffle = v
		return nil
	},
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

func defaultApp() *App {
	conf := &App{
		Iterations:         1,
		ParallelIterations: 1,
		SlowThreshold:      30 * time.Second,
	}
	if cwd, err := os.Getwd(); err == nil {
		conf.RepoRoot = cwd
	}
	conf.AIOutput = !term.IsTerminal(os.Stdout.Fd())
	return conf
}

func bindCatalogShellHooks(conf *App, flags *pflag.FlagSet) error {
	if conf.shellHooks == nil {
		conf.shellHooks = make(map[string]string)
	}
	for _, e := range hooks.Catalog {
		if e.Scope != hooks.ScopeIteration {
			continue
		}
		if flags.Lookup(e.Flag) == nil {
			continue
		}
		v, err := flags.GetString(e.Flag)
		if err != nil {
			return err
		}
		conf.shellHooks[e.Flag] = v
		switch e.Name {
		case "IterationSetup":
			conf.IterationSetup = v
		case "IterationTeardown":
			conf.IterationTeardown = v
		}
	}
	return nil
}

func applyFlags(cmd *cobra.Command, conf *App) error {
	flags := cmd.Flags()
	var errs []error
	if err := bindCatalogShellHooks(conf, flags); err != nil {
		errs = append(errs, err)
	}
	for name, bind := range flagRegistry {
		if flags.Lookup(name) == nil {
			continue
		}
		if err := bind(conf, flags); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Load reads the active command's flags into App.
func Load(cmd *cobra.Command) (*App, error) {
	if cmd == nil {
		return nil, errors.New("command is required")
	}
	conf := defaultApp()
	if err := applyFlags(cmd, conf); err != nil {
		return nil, err
	}
	return conf, nil
}
