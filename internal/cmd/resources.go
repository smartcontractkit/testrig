package cmd

import (
	"context"
	"errors"
	"sync"

	"github.com/smartcontractkit/testrig/internal/hooks"
)

// provisionResources invokes the configured resource provider for count
// resources and returns them with a cleanup that tears each one down. When no
// provider is configured it returns no resources and a no-op cleanup. The
// cleanup is always non-nil on success so callers can defer it unconditionally.
func provisionResources(ctx context.Context, opts hooks.RunOptions, count int) ([]hooks.Resource, func() error, error) {
	if opts.ResourceProvider == nil {
		return nil, func() error { return nil }, nil
	}
	resources, err := opts.ResourceProvider(ctx, count)
	if err != nil {
		return nil, func() error { return nil }, err
	}
	cleanup := func() error {
		var wg sync.WaitGroup
		errs := make([]error, len(resources))
		for i, r := range resources {
			if r.Cleanup != nil {
				wg.Add(1)
				go func(i int, r hooks.Resource) {
					defer wg.Done()
					errs[i] = r.Cleanup()
				}(i, r)
			}
		}
		wg.Wait()
		return errors.Join(errs...)
	}
	return resources, cleanup, nil
}

// resourceEnv provisions a single resource (count == 1) for the run/gotestsum
// subcommands and returns its Env to append to the child process, plus a
// cleanup to defer. When no provider is configured it returns nil env and a
// no-op cleanup.
func resourceEnv(ctx context.Context, opts hooks.RunOptions) ([]string, func() error, error) {
	resources, cleanup, err := provisionResources(ctx, opts, 1)
	if err != nil {
		return nil, cleanup, err
	}
	if len(resources) == 0 {
		return nil, cleanup, nil
	}
	return resources[0].Env, cleanup, nil
}
