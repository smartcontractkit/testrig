// Package cmd implements the testrig CLI commands.
package cmd

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"charm.land/fang/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/hooks"
	"github.com/smartcontractkit/testrig/internal/runner"
)

// NewRootCommand constructs a new command tree dynamically based on RunOptions.
func NewRootCommand(runnerOpts hooks.RunOptions) *cobra.Command {
	cliName := effectiveRootCommand(runnerOpts.RootCommand)

	rootCmd := &cobra.Command{
		Use:                cliName,
		Short:              "Run Go tests with complex setups and teardowns with a single command",
		Long:               "",
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			return runRootAfterParsing(cmd, args, runnerOpts, func(goTestArgs []string) error {
				conf, err := config.Load(cmd)
				if err != nil {
					return err
				}
				env, cleanup, err := resourceEnv(cmd.Context(), runnerOpts)
				if err != nil {
					return err
				}
				defer func() { finishResourceCleanup(cmd, &err, cleanup) }()
				return runner.GoTest(cmd.Context(), conf, goTestArgs, env)
			})
		},
	}

	rootCmd.PersistentFlags().
		Bool("ai-output", !term.IsTerminal(os.Stdout.Fd()), "Use sparse output for agent tooling (and robotic humans)")
	hooks.RegisterPersistentFlags(rootCmd.PersistentFlags())

	gotestsumCmd := newGotestsumCmd(runnerOpts)
	diagnoseCmd := newDiagnoseCmd(runnerOpts)
	initSkillCmd := newInitSkillCmd()
	showSkillCmd := newShowSkillCmd()

	rootCmd.AddCommand(gotestsumCmd)
	rootCmd.AddCommand(diagnoseCmd)
	rootCmd.AddCommand(initSkillCmd)
	rootCmd.AddCommand(showSkillCmd)

	if runnerOpts.RootFlags != nil {
		runnerOpts.RootFlags(rootCmd.PersistentFlags())
	}
	for _, c := range runnerOpts.Commands {
		rootCmd.AddCommand(c)
	}

	applyRootCommand(rootCmd, gotestsumCmd, diagnoseCmd, cliName)

	return rootCmd
}

// Execute runs the root command. A SIGINT or SIGTERM cancels the context so
// long-running subcommands (notably `diagnose`) can stop cleanly and still write
// their post-run analysis. A second signal hits the default handler and
// force-exits.
func Execute(opts ...hooks.Option) {
	if err := runExecute(opts...); err != nil {
		os.Exit(1)
	}
}

func runExecute(opts ...hooks.Option) error {
	runnerOpts := hooks.BuildOptions(opts...)
	rootCmd := NewRootCommand(runnerOpts)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	fangOpts := []fang.Option{
		fang.WithoutCompletions(),
		// Root forwards unknown flags (including go test -v) to go test; fang's -v is version.
		fang.WithoutVersion(),
	}
	return fang.Execute(ctx, rootCmd, fangOpts...)
}
