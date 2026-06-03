package cmd

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/spf13/cobra"

	"github.com/smartcontractkit/testrig/internal/hooks"
)

// finishResourceCleanup runs cleanup after a command. On failure it logs to stderr
// and sets *err when the command itself succeeded.
func finishResourceCleanup(cmd *cobra.Command, err *error, cleanup func() error) {
	if cleanup == nil {
		return
	}
	if cerr := cleanup(); cerr != nil {
		_, printErr := fmt.Fprintf(cmd.ErrOrStderr(), "%s: resource cleanup failed: %v\n", rootCLIName(cmd), cerr)
		if printErr != nil {
			if *err == nil {
				*err = printErr
			}
		}
		if *err == nil {
			*err = cerr
		}
	}
}

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
	if len(resources) != count {
		countErr := fmt.Errorf(
			"resource provider returned %d resources, want %d", len(resources), count)
		return nil, func() error { return nil }, errors.Join(countErr, cleanupResources(resources))
	}
	cleanup := func() error { return cleanupResources(resources) }
	return resources, cleanup, nil
}

func cleanupResources(resources []hooks.Resource) error {
	var wg sync.WaitGroup
	errs := make([]error, len(resources))
	for i, r := range resources {
		if r.Cleanup != nil {
			wg.Go(func() {
				errs[i] = r.Cleanup()
			})
		}
	}
	wg.Wait()
	return errors.Join(errs...)
}

// resourceEnv provisions a single resource (count == 1) for the default go test
// invocation and gotestsum, and returns its Env to append to the child process, plus a
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
