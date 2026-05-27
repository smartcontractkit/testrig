package testrig_test

import (
	"context"
	"fmt"

	testrig "github.com/smartcontractkit/testrig"
)

// Combine GlobalSetup + IterationSetup + IterationTeardown + GlobalTeardown
// to manage a full test lifecycle inside TestMain.
func ExampleGlobalSetup() {
	ctx := context.Background()

	// Wire all four lifecycle hooks.
	hooks := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"global setup", func(_ context.Context) error { fmt.Println("global setup"); return nil }},
		{"iteration setup", func(_ context.Context) error { fmt.Println("iteration setup"); return nil }},
		{"iteration teardown", func(_ context.Context) error { fmt.Println("iteration teardown"); return nil }},
		{"global teardown", func(_ context.Context) error { fmt.Println("global teardown"); return nil }},
	}
	_ = hooks

	_ = testrig.GlobalSetup(ctx, func(_ context.Context) error { fmt.Println("global setup"); return nil })
	_ = testrig.IterationSetup(ctx, func(_ context.Context) error { fmt.Println("iteration setup"); return nil })
	_ = testrig.IterationTeardown(ctx, func(_ context.Context) error { fmt.Println("iteration teardown"); return nil })
	_ = testrig.GlobalTeardown(ctx, func(_ context.Context) error { fmt.Println("global teardown"); return nil })
	// Output:
	// global setup
	// iteration setup
	// iteration teardown
	// global teardown
}
