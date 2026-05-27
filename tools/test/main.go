// Package main runs the testrig tool using native Go hooks.
// This is a living example of how to use testrig natively in Go.
package main

import (
	"context"

	"github.com/smartcontractkit/testrig"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Dogfood testrig itself.
func main() {
	var postgresContainer *postgres.PostgresContainer

	testrig.Run(
		// Setup a single Postgres container for the entire test suite.
		// (testrig's tests doesn't actually need this, this is just for demonstration purposes.)
		testrig.GlobalSetup(func(ctx context.Context) error {
			dbName := "users"
			dbUser := "user"
			dbPassword := "password"

			var err error
			postgresContainer, err = postgres.Run(ctx,
				"postgres:16-alpine",
				postgres.WithDatabase(dbName),
				postgres.WithUsername(dbUser),
				postgres.WithPassword(dbPassword),
				postgres.BasicWaitStrategies(),
			)
			if err != nil {
				return err
			}
			return nil
		}),
		testrig.IterationSetup(func(_ context.Context) error {
			return nil
		}),
		testrig.IterationTeardown(func(_ context.Context) error {
			return nil
		}),
		// Teardown the Postgres container after the test suite.
		testrig.GlobalTeardown(func(ctx context.Context) error {
			if err := postgresContainer.Terminate(ctx); err != nil {
				return err
			}
			return nil
		}),
	)
}
