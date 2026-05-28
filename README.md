# testrig

A Go test harness for running your tests with universal setup and teardown, with a focus on hunting and fixing flaky tests.

## Quickstart: Find Flaky Tests

```sh
go install github.com/smartcontractkit/testrig/cmd/testrig@latest # Install
testrig diagnose --iterations 5 -- ./... # Run your test suite 5 times and see detailed stats
```

### How Many `iterations` Do I Need?

If you want to be sure that you have fixed a flaky test, or are chasing down a rare flake scenario, you'll need to re-run the test a bunch of times. The [math gets a little complicated](https://reliabilityanalyticstoolkit.appspot.com/sample_size) on exactly how many, so here's a simplified table to show you how confident you should be in your results.

| Iterations | Chance you missed a flake |
| ---------- | ------------------------- |
| 5          | 50%                       |
| 30         | 10%                       |
| 60         | 5%                        |
| 150        | 2%                        |
| 300        | 1%                        |
| 500+       | < 1%                      |

## Run

`testrig` enables a few QoL features for running large Go test suites efficiently. You can define test lifecycle hooks, and run from the CLI, or from a lightweight Go setup.

### CLI

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

### Native Go

You can import `testrig` as a Go package and define your hooks and defaults entirely in Go! See [the pkg.go.dev docs](https://pkg.go.dev/github.com/smartcontractkit/testrig#pkg-examples) for an example setup.

### Lifecycle Hooks

Run setup and teardown scripts during your test lifecycle.

<!-- testrig:gendocs:table -->
| Option | When it runs | CLI equivalent |
| ------ | ------------ | -------------- |
| `testrig.GlobalSetup` | Run once before any tests | `--global-setup` |
| `testrig.GlobalTeardown` | Run once after all tests finish | `--global-teardown` |
| `testrig.IterationSetup` | Run before each diagnose iteration | `--iteration-setup` |
| `testrig.IterationTeardown` | Run after each diagnose iteration | `--iteration-teardown` |
<!-- /testrig:gendocs:table -->

## Contributing

Use [just](https://github.com/casey/just) instead of `make`.

```sh
just lefthook # Install pre-commit and pre-push hooks
```
