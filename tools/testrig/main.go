// Package main runs the testrig tool.
package main

import (
	"context"
	"fmt"

	"github.com/smartcontractkit/testrig"
	"github.com/smartcontractkit/testrig/cli"
)

func main() {
	cli.Main(
		testrig.WithGlobalSetup(func(_ context.Context) error {
			fmt.Println("--> NATIVE GLOBAL SETUP RAN <--")
			return nil
		}),
		testrig.WithIterationSetup(func(_ context.Context) error {
			fmt.Println("--> NATIVE ITERATION SETUP RAN <--")
			return nil
		}),
	)
}
