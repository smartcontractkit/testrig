package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
	known := persistentFlagNames(cmd.Root())
	var testrigArgs, goTestArgs []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			goTestArgs = append(goTestArgs, args[i:]...)
			break
		}
		if name, hasValue, ok := knownLongFlag(arg, known); ok {
			if hasValue {
				testrigArgs = append(testrigArgs, arg)
				continue
			}
			if isBoolPersistentFlag(cmd.Root(), name) {
				testrigArgs = append(testrigArgs, arg)
				continue
			}
			if i+1 < len(args) {
				testrigArgs = append(testrigArgs, arg, args[i+1])
				i++
			} else {
				testrigArgs = append(testrigArgs, arg)
			}
			continue
		}
		goTestArgs = append(goTestArgs, arg)
	}

	if len(testrigArgs) == 0 {
		return goTestArgs, nil
	}

	fs := pflag.NewFlagSet(cmd.Root().Name(), pflag.ContinueOnError)
	cmd.Root().PersistentFlags().VisitAll(func(f *pflag.Flag) {
		fs.AddFlag(f)
	})
	if err := fs.Parse(testrigArgs); err != nil {
		return nil, err
	}
	return goTestArgs, nil
}

func persistentFlagNames(root *cobra.Command) map[string]struct{} {
	names := make(map[string]struct{})
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		names[f.Name] = struct{}{}
	})
	return names
}

func knownLongFlag(arg string, known map[string]struct{}) (name string, hasValue bool, ok bool) {
	if !strings.HasPrefix(arg, "--") {
		return "", false, false
	}
	body := strings.TrimPrefix(arg, "--")
	if body == "" {
		return "", false, false
	}
	if before, _, found := strings.Cut(body, "="); found {
		if _, ok := known[before]; ok {
			return before, true, true
		}
		return "", false, false
	}
	if _, ok := known[body]; ok {
		return body, false, true
	}
	return "", false, false
}

func isBoolPersistentFlag(root *cobra.Command, name string) bool {
	f := root.PersistentFlags().Lookup(name)
	if f == nil {
		return false
	}
	return f.Value.Type() == "bool"
}
