// Package main runs the testrig tool using native Go execution.
package main

import (
	"github.com/smartcontractkit/testrig"
)

// Dogfood testrig on itself.
func main() {
	testrig.Run()
}
