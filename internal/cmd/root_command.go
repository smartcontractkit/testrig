package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

const defaultRootCommandName = "testrig"

func effectiveRootCommand(name string) string {
	if name == "" {
		return defaultRootCommandName
	}
	return name
}

// rootCLIName returns the configured root command name (e.g. testrig or cltest).
func rootCLIName(cmd *cobra.Command) string {
	if cmd == nil {
		return defaultRootCommandName
	}
	return cmd.Root().Name()
}

const (
	rootExampleTmpl = `# Use vanilla go test commands
%s -v -count=1 -p 4 ./...
# Use gotestsum as the runner
%s gotestsum --format=dots -- -count=1 ./...
# Run the full test suite 10 times and collect statistics, debug logs, and more
%s diagnose --iterations 10 -- --timeout=15m ./...`

	rootLongTmpl = `Run Go tests with a single command.

By default, %s runs go test from the repo root. All go test flags and package patterns
are passed through. Global options (--ai-output, lifecycle hooks) are parsed by testrig;
all other flags are forwarded to go test. For example:
  %s --ai-output -v -count=1 ./...

Subcommands:

- gotestsum: Run tests using gotestsum for those that prefer its output and tools
- diagnose: Run tests multiple times to collect statistics, debug logs, and more to help find flakes, races, panics, timeouts, and other issues`

	gotestsumLongTmpl = `Runs gotestsum from the repo root.

Because this subcommand does not parse flags, global options (--ai-output) must appear
on the root command before gotestsum, for example:
  %s --ai-output gotestsum --format=testname -- -count=1 ./...`

	gotestsumExampleTmpl = `%s gotestsum --format=dots -- -count=1 ./...
%s --ai-output gotestsum --format=testname -- -count=1 ./...`

	diagnoseExampleTmpl = `# Run the full test suite 10 times.
%s diagnose --iterations 10 -- ./...`
)

func applyRootCommand(rootCmd, gotestsumCmd, diagnoseCmd *cobra.Command, cliName string) {
	rootCmd.Use = cliName
	rootCmd.Long = fmt.Sprintf(rootLongTmpl, cliName, cliName)
	rootCmd.Example = fmt.Sprintf(rootExampleTmpl, cliName, cliName, cliName)
	gotestsumCmd.Long = fmt.Sprintf(gotestsumLongTmpl, cliName)
	gotestsumCmd.Example = fmt.Sprintf(gotestsumExampleTmpl, cliName, cliName)
	diagnoseCmd.Example = fmt.Sprintf(diagnoseExampleTmpl, cliName)
}
