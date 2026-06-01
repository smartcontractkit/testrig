// Package cmd implements the testrig CLI commands.
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"charm.land/fang/v2"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/smartcontractkit/testrig/internal/config"
	"github.com/smartcontractkit/testrig/internal/hooks"
	"github.com/smartcontractkit/testrig/internal/runner"
)

var runnerOpts hooks.RunOptions

var rootCmd = &cobra.Command{
	Use:                defaultRootCommandName,
	Short:              "Run Go tests with complex setups and teardowns with a single command",
	Long:               "",
	Args:               cobra.ArbitraryArgs,
	DisableFlagParsing: true,
	RunE: withGlobalHooks(func(cmd *cobra.Command, args []string) (err error) {
		if isHelpRequest(args) {
			return pflag.ErrHelp
		}
		goTestArgs, err := goTestArgsFromRoot(cmd, args)
		if err != nil {
			return err
		}
		conf, err := config.Load(cmd)
		if err != nil {
			return err
		}
		env, cleanup, err := resourceEnv(cmd.Context(), runnerOpts)
		if err != nil {
			return err
		}
		defer func() {
			if cerr := cleanup(); cerr != nil {
				fmt.Fprintf(os.Stderr, "%s: resource cleanup failed: %v\n", cliName, cerr)
				if err == nil {
					err = cerr
				}
			}
		}()
		return runner.GoTest(cmd.Context(), conf, goTestArgs, env)
	}),
}

func init() {
	applyRootCommand("")
	rootCmd.PersistentFlags().
		Bool("ai-output", !term.IsTerminal(os.Stdout.Fd()), "Use sparse output for agent tooling (and robotic humans)")
	hooks.RegisterPersistentFlags(rootCmd.PersistentFlags())

	rootCmd.AddCommand(gotestsumCmd)
	rootCmd.AddCommand(diagnoseCmd)
}

// Execute runs the root command. A SIGINT or SIGTERM cancels the context so
// long-running subcommands (notably `diagnose`) can stop cleanly and still write
// their post-run analysis. A second signal hits the default handler and
// force-exits.
func Execute(opts ...hooks.Option) {
	runnerOpts = hooks.BuildOptions(opts...)
	applyRootCommand(runnerOpts.RootCommand)
	if runnerOpts.RootFlags != nil {
		runnerOpts.RootFlags(rootCmd.PersistentFlags())
	}
	for _, c := range runnerOpts.Commands {
		rootCmd.AddCommand(c)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	fangOpts := []fang.Option{
		fang.WithoutCompletions(),
		// Root forwards unknown flags (including go test -v) to go test; fang's -v is version.
		fang.WithoutVersion(),
	}
	if err := fang.Execute(ctx, rootCmd, fangOpts...); err != nil {
		stop()
		os.Exit(1)
	}
	stop()
}
