# Lifecycle hooks

testrig runs hooks at different points in the test lifecycle. **Global** hooks apply to the default `go test` invocation, `gotestsum`, and `diagnose`. **Iteration** hooks apply only to `diagnose`.

Register hooks via CLI flags or Go options in a [`tools/test`](./tools/test/README.md) binary. Hooks receive `context.Context` and should respect cancellation (SIGINT cancels in-flight setup).

The table below is generated from [`internal/hooks/catalog.go`](./internal/hooks/catalog.go) and godoc on [`hooks.go`](./hooks.go). Run `go generate` after changes.

<!-- testrig:gendocs:table -->
| Option | When it runs | CLI equivalent |
| ------ | ------------ | -------------- |
| `testrig.GlobalSetup` | Run once before any tests | `--global-setup` |
| `testrig.GlobalTeardown` | Run once after all tests finish | `--global-teardown` |
| `testrig.IterationSetup` | Run before each diagnose iteration | `--iteration-setup` |
| `testrig.IterationTeardown` | Run after each diagnose iteration | `--iteration-teardown` |
<!-- /testrig:gendocs:table -->

## Shell hooks

`testrig.NewShellHook("command")` wraps a shell command (`sh -c`) as a `Hook`. Non-zero exit includes combined stdout/stderr in the error.

## Flag placement

For the default `go test` invocation, testrig parses its own flags (`--ai-output`, lifecycle hooks) and forwards unknown flags to `go test`:

```sh
testrig --global-setup "docker compose up -d" -v -count=1 ./...
testrig --global-setup "docker compose up -d" diagnose --iterations 5 -- ./...
```

For `gotestsum`, put root flags before the subcommand; flags after `gotestsum` are forwarded to gotestsum / `go test`.
