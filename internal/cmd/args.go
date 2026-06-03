package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/smartcontractkit/testrig/internal/hooks"
)

// isHelpRequest reports whether args ask for testrig usage (not go test). Root uses
// DisableFlagParsing, so cobra does not intercept -h/--help; we handle that before
// forwarding to go test. Args after "--" are left for go test (e.g. testrig -- -h).
func isHelpRequest(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		switch arg {
		case "-h", "--help":
			return true
		}
	}
	return false
}

// goTestArgsFromRoot parses testrig persistent flags from args, applies them to the
// root flag set, and returns the remainder for go test. Root uses DisableFlagParsing
// so go test flags (-v, -count, -run, etc.) are never interpreted by pflag.
func goTestArgsFromRoot(cmd *cobra.Command, args []string) ([]string, error) {
	flags := cmd.Root().PersistentFlags()
	var testrigArgs, goTestArgs []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			goTestArgs = append(goTestArgs, args[i:]...)
			break
		}

		if !strings.HasPrefix(arg, "-") {
			goTestArgs = append(goTestArgs, arg)
			continue
		}

		name := strings.TrimLeft(arg, "-")
		if before, _, found := strings.Cut(name, "="); found {
			name = before
		}

		// Only match long flags for now to match current behavior,
		// but using native pflag lookup.
		var f *pflag.Flag
		if strings.HasPrefix(arg, "--") {
			f = flags.Lookup(name)
		} else if len(name) == 1 {
			f = flags.ShorthandLookup(name)
		}

		if f != nil {
			switch {
			case strings.Contains(arg, "="):
				testrigArgs = append(testrigArgs, arg)
			case f.NoOptDefVal != "":
				testrigArgs = append(testrigArgs, arg)
			case i+1 < len(args):
				testrigArgs = append(testrigArgs, arg, args[i+1])
				i++
			default:
				testrigArgs = append(testrigArgs, arg)
			}
			continue
		}

		goTestArgs = append(goTestArgs, arg)
	}

	if len(testrigArgs) > 0 {
		if err := flags.Parse(testrigArgs); err != nil {
			return nil, err
		}
	}

	return goTestArgs, nil
}

// runRootAfterParsing applies persistent root flags from args, then runs fn inside
// withGlobalHooks so global setup/teardown see the parsed flag values. Help requests
// return before any hooks run.
func runRootAfterParsing(
	cmd *cobra.Command,
	args []string,
	runnerOpts hooks.RunOptions,
	fn func(remainder []string) error,
) error {
	if isHelpRequest(args) {
		return pflag.ErrHelp
	}
	remainder, err := goTestArgsFromRoot(cmd, args)
	if err != nil {
		return err
	}
	return withGlobalHooks(runnerOpts, func(*cobra.Command, []string) error {
		return fn(remainder)
	})(cmd, args)
}
