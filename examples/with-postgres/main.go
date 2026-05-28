package main

import (
	"context"
	"log"

	"github.com/smartcontractkit/testrig"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Example demonstrating testrig with a Postgres container.
func main() {
	var postgresContainer *postgres.PostgresContainer

	testrig.Run(
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

			log.Printf("Postgres container started (session %s)", postgresContainer.SessionID())
			return nil
		}),
		testrig.IterationSetup(func(_ context.Context) error {
			// e.g. truncate tables or reset state
			return nil
		}),
		testrig.IterationTeardown(func(_ context.Context) error {
			return nil
		}),
		testrig.GlobalTeardown(func(ctx context.Context) error {
			if postgresContainer != nil {
				if err := postgresContainer.Terminate(ctx); err != nil {
					return err
				}
			}
			return nil
		}),
	)
}
