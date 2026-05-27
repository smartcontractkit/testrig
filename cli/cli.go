// Package cli provides the public entry point for the testrig CLI.
// It allows consumers to build custom testrig binaries and inject
// native Go lifecycle hooks using functional options.
package cli

import (
	"github.com/smartcontractkit/testrig"
	"github.com/smartcontractkit/testrig/internal/cmd"
)

// Main executes the testrig CLI. It accepts functional Options to allow
// injecting native Go hooks into the test lifecycle. This enables building
// custom runners (e.g., as a `go tool`) with complex, suite-wide setups.
func Main(opts ...testrig.Option) {
	cmd.Execute(opts...)
}
