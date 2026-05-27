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

	"github.com/smartcontractkit/testrig/internal/hooks"
)

var runnerOpts hooks.RunOptions

var rootCmd = &cobra.Command{
	Use:   "testrig",
	Short: "Run Go tests with complex setups and teardowns with a single command",
	Long: `Run Go tests with a single command.

Modes:

- run: Run tests using vanilla go test command and arguments
- gotestsum: Run tests using gotestsum for those that prefer its output and tools
- diagnose: Run tests multiple times to collect statistics, debug logs, and more to help find flakes, races, panics, timeouts, and other issues`,
	Example: `# Use vanilla go test commands
testrig run -v -count=1 -p 4 ./...
# Use gotestsum as the runner
testrig gotestsum --format=dots -- -count=1 ./...
# Run the full test suite 10 times and collect statistics, debug logs, and more
testrig diagnose --iterations 10 -- --timeout=15m ./...`,
}

func init() {
	rootCmd.PersistentFlags().
		Bool("ai-output", !term.IsTerminal(os.Stdout.Fd()), "Use sparse output for agent tooling (and robotic humans)")
	hooks.RegisterPersistentFlags(rootCmd.PersistentFlags())

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(gotestsumCmd)
	rootCmd.AddCommand(diagnoseCmd)
}

// Execute runs the root command. A SIGINT or SIGTERM cancels the context so
// long-running subcommands (notably `diagnose`) can stop cleanly and still write
// their post-run analysis. A second signal hits the default handler and
// force-exits.
func Execute(opts ...hooks.Option) {
	runnerOpts = hooks.BuildOptions(opts...)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	fangOpts := []fang.Option{fang.WithoutCompletions()}
	if err := fang.Execute(ctx, rootCmd, fangOpts...); err != nil {
		stop()
		os.Exit(1)
	}
	stop()
}
