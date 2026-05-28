package testrig_test

import (
	"context"
	"fmt"

	"github.com/smartcontractkit/testrig"
)

func ExampleRun() {
	testrig.Run(
		// GlobalSetup runs once before any tests start.
		testrig.GlobalSetup(func(_ context.Context) error {
			fmt.Println("Starting mock background service...")
			// Simulate starting a dependency, e.g.:
			// cmd := exec.CommandContext(ctx, "docker", "compose", "up", "-d")
			// return cmd.Run()
			return nil
		}),

		// IterationSetup runs before each diagnose iteration.
		testrig.IterationSetup(func(_ context.Context) error {
			fmt.Println("Resetting database state for next iteration...")
			// Simulate resetting state:
			// cmd := exec.CommandContext(ctx, "psql", "-c", "TRUNCATE events")
			// return cmd.Run()
			return nil
		}),

		// IterationTeardown runs after each diagnose iteration.
		testrig.IterationTeardown(func(_ context.Context) error {
			fmt.Println("Cleaning up iteration artifacts...")
			return nil
		}),

		// GlobalTeardown runs once after all tests finish.
		testrig.GlobalTeardown(func(_ context.Context) error {
			fmt.Println("Stopping mock background service...")
			return nil
		}),
	)
}
