# Native Go testrig hooks

Define test lifecycle hooks in Go instead of shell flags by building a small `main` package. The binary is a full testrig CLI (default `go test`, `gotestsum`, `diagnose`) with your hooks wired in.

## Why

- Type-safe setup (testcontainers, clients, config) without shell quoting
- Share hook code with the rest of your module
- Same diagnose output and `report.json` as the installed `testrig` CLI

## Setup

### 1. Create the module

From your repository root:

```sh
mkdir -p tools/test
cd tools/test
go mod init github.com/yourOrg/yourProject/tools/test
```

Add a `replace` so the child module resolves your repo (adjust the path if testrig is a normal dependency instead):

```go
// tools/test/go.mod
module github.com/yourOrg/yourProject/tools/test

go 1.26

replace github.com/yourOrg/yourProject => ../..

require github.com/smartcontractkit/testrig v0.0.0
```

Run `go mod tidy` in `tools/test`.

### 2. Add main.go

Minimal starting point:

```go
package main

import "github.com/smartcontractkit/testrig"

func main() {
	testrig.Run(
		testrig.GlobalSetup(func(ctx context.Context) error { ... }),
		testrig.GlobalTeardown(func(ctx context.Context) error { ... }),
		testrig.IterationSetup(func(ctx context.Context) error { ... }),
		testrig.IterationTeardown(func(ctx context.Context) error { ... }),
	)
}
```

See [main.go](./main.go) in this repo for a testcontainers Postgres example.

Hook semantics match the CLI. See [HOOKS.md](../../HOOKS.md) (table generated from [`hooks.go`](../../hooks.go) via `go generate`).

<!-- testrig:gendocs:table -->
| Option | When it runs | CLI equivalent |
| ------ | ------------ | -------------- |
| `testrig.GlobalSetup` | Run once before any tests | `--global-setup` |
| `testrig.GlobalTeardown` | Run once after all tests finish | `--global-teardown` |
| `testrig.IterationSetup` | Run before each diagnose iteration | `--iteration-setup` |
| `testrig.IterationTeardown` | Run after each diagnose iteration | `--iteration-teardown` |
<!-- /testrig:gendocs:table -->

`testrig.NewShellHook("cmd")` wraps a shell command if you still need one hook in bash.

### 3. Wire into the repo root go.mod

In the **repository root** `go.mod`, add a `tool` directive and `replace` (module path must match `tools/test/go.mod`):

```go
tool github.com/yourOrg/yourProject/tools/test

replace github.com/yourOrg/yourProject/tools/test => ./tools/test
```

Then from the repo root:

```sh
go mod tidy
```

## Run

**Preferred** — from repo root via `go tool`:

```sh
go tool test diagnose --iterations 5 -- ./...
go tool test -v -count=1 ./...
```

The `run` subcommand was removed; pass `go test` flags directly after the tool name.

**Alternative** — without a `tool` line (or when debugging the binary):

```sh
go -C tools/test run . diagnose --iterations 5 -- ./...
```

Harness flags go before `--`; `go test` flags and packages after `--` (same as the `testrig` CLI). See the [root README](../../README.md) for diagnose output and `jq` examples.

## Dogfood in this repo

This repository registers the tool in [go.mod](../../go.mod):

```go
tool github.com/smartcontractkit/testrig/tools/test
```

Run native hooks against testrig itself:

```sh
just dogfood_native
# equivalent:
go tool test diagnose --iterations 3 --parallel-iterations 3 -- ./...
```

## Optional: agent skill

If you use Cursor agents for flake fixing, install the bundled skill from the repo root (requires the `testrig` CLI):

```sh
testrig init-skill
```

That is separate from this `tools/test` binary.
