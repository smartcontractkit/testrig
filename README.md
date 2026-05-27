# testrig

A Go test harness for running your tests with universal setup and teardown, with a focus on hunting and fixing flaky tests.

## Find Flaky Tests

```sh
go install github.com/smartcontractkit/testrig/cmd/testrig@latest # Install
testrig diagnose --iterations 5 -- ./... # Run your test suite 5 times and see detailed stats
```

### Lifecycle Hooks

Run shell commands at key points in the test lifecycle. See [HOOKS.md](./HOOKS.md) for details (table is generated from [`hooks.go`](./hooks.go) via `go generate`).

<!-- testrig:gendocs:table -->
| Option | When it runs | CLI equivalent |
| ------ | ------------ | -------------- |
| `testrig.GlobalSetup` | Run once before any tests | `--global-setup` |
| `testrig.GlobalTeardown` | Run once after all tests finish | `--global-teardown` |
| `testrig.IterationSetup` | Run before each diagnose iteration | `--iteration-setup` |
| `testrig.IterationTeardown` | Run after each diagnose iteration | `--iteration-teardown` |
<!-- /testrig:gendocs:table -->

```sh
# Spin up dependencies before any test, tear down after
testrig diagnose --iterations 10 \
  --global-setup "docker compose up -d" \
  --global-teardown "docker compose down" \
  -- ./...

# Reset DB state between each diagnose iteration
testrig diagnose --iterations 10 \
  --iteration-setup "psql -c 'TRUNCATE events'" \
  --iteration-teardown "rm -rf ./tmp/artifacts" \
  -- ./...
```

## Use Go Instead of CLI

You can import `testrig` as a Go package and define your hooks and defaults entirely in Go! See [our example setup](./tools/test/README.md).

## Contributing

Use [just](https://github.com/casey/just) instead of `make`.

```sh
just lefthook # Install pre-commit and pre-push hooks
```
