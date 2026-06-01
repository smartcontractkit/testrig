package hooks

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/spf13/pflag"
)

// Scope is when a hook runs relative to testrig subcommands.
type Scope uint8

const (
	// ScopeGlobal hooks run for the default go test invocation, gotestsum, and diagnose.
	ScopeGlobal Scope = iota
	// ScopeIteration hooks run only for diagnose iterations.
	ScopeIteration
)

// Phase is whether the hook runs before or after its scope's work.
type Phase uint8

const (
	// PhaseSetup hooks run before the scope's work.
	PhaseSetup Phase = iota
	// PhaseTeardown hooks run after the scope's work.
	PhaseTeardown
)

// Entry describes one lifecycle hook in the catalog: public Option name, CLI flag, and scope.
type Entry struct {
	Name      string // e.g. GlobalSetup; matches testrig.GlobalSetup and RunOptions field.
	Flag      string // e.g. global-setup (persistent root flag, no leading dashes).
	Scope     Scope
	Phase     Phase
	FlagUsage string // Cobra flag description.
}

// Catalog is the single source of truth for lifecycle hooks. Add an Entry here to get a
// CLI flag, config binding (iteration scope), and docs row (via go generate).
var Catalog = []Entry{
	{
		Name:      "GlobalSetup",
		Flag:      "global-setup",
		Scope:     ScopeGlobal,
		Phase:     PhaseSetup,
		FlagUsage: "Shell command to run before tests",
	},
	{
		Name:      "GlobalTeardown",
		Flag:      "global-teardown",
		Scope:     ScopeGlobal,
		Phase:     PhaseTeardown,
		FlagUsage: "Shell command to run after tests",
	},
	{
		Name:      "IterationSetup",
		Flag:      "iteration-setup",
		Scope:     ScopeIteration,
		Phase:     PhaseSetup,
		FlagUsage: "Shell command to run before each diagnose iteration",
	},
	{
		Name:      "IterationTeardown",
		Flag:      "iteration-teardown",
		Scope:     ScopeIteration,
		Phase:     PhaseTeardown,
		FlagUsage: "Shell command to run after each diagnose iteration",
	},
}

func init() {
	if err := validateCatalog(); err != nil {
		panic("hooks catalog: " + err.Error())
	}
}

func validateCatalog() error {
	seenFlag := make(map[string]string)
	seenName := make(map[string]struct{})
	for _, e := range Catalog {
		if e.Name == "" || e.Flag == "" {
			return fmt.Errorf("entry missing name or flag: %+v", e)
		}
		if _, ok := seenName[e.Name]; ok {
			return fmt.Errorf("duplicate catalog name %q", e.Name)
		}
		seenName[e.Name] = struct{}{}
		if other, ok := seenFlag[e.Flag]; ok {
			return fmt.Errorf("duplicate catalog flag %q (%s and %s)", e.Flag, other, e.Name)
		}
		seenFlag[e.Flag] = e.Name
		if camelToKebab(e.Name) != e.Flag {
			return fmt.Errorf(
				"catalog entry %q: flag %q does not match kebab name %q",
				e.Name,
				e.Flag,
				camelToKebab(e.Name),
			)
		}
	}
	return nil
}

// RegisterPersistentFlags adds every catalog entry as a root persistent string flag.
func RegisterPersistentFlags(fs *pflag.FlagSet) {
	for _, e := range Catalog {
		fs.String(e.Flag, "", e.FlagUsage)
	}
}

// Entries returns catalog entries matching scope and phase, in catalog order.
func Entries(scope Scope, phase Phase) []Entry {
	var out []Entry
	for _, e := range Catalog {
		if e.Scope == scope && e.Phase == phase {
			out = append(out, e)
		}
	}
	return out
}

// EntryByName returns a catalog entry by Option/func name.
func EntryByName(name string) (Entry, bool) {
	for _, e := range Catalog {
		if e.Name == name {
			return e, true
		}
	}
	return Entry{}, false
}

// Label returns a short phrase for error messages (e.g. "global setup").
func (e Entry) Label() string {
	scope := "global"
	if e.Scope == ScopeIteration {
		scope = "iteration"
	}
	phase := "setup"
	if e.Phase == PhaseTeardown {
		phase = "teardown"
	}
	return scope + " " + phase
}

// CLIFlag returns the flag with leading dashes.
func (e Entry) CLIFlag() string { return "--" + e.Flag }

// CatalogNames returns Option registrar names in catalog order.
func CatalogNames() []string {
	names := make([]string, len(Catalog))
	for i, e := range Catalog {
		names[i] = e.Name
	}
	return names
}

// camelToKebab converts GlobalSetup to global-setup.
func camelToKebab(name string) string {
	var b strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte('-')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}
