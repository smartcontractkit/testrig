## Goals

- An easy to import and use test rig for Go projects
- Can be used as a package or CLI
- Used to find and fix flaky tests
- Minimal CPU, RAM, and timing overhead added to test execution

## Validate changes

```sh
golangci-lint run ./... --fix # Lint
go test ./...                 # Test
```

## Architecture

- `package testrig` (root) — public library API. `Hook`, `GlobalSetup`, `GlobalTeardown`, `IterationSetup`, `IterationTeardown`, `NewShellHook`. Importable by consumers.
- `internal/cmd/` — Cobra CLI wiring. `root.go` binds lifecycle hook flags and calls the public API.
- `internal/runner/` — core test execution. `Diagnose` is the main entry point; `diagnoseRunHooks` carries iteration hooks as `func(context.Context) error` fields.
- `internal/config/` — Cobra flag registry config loading. `config.App` is the unified config struct.
- `internal/output/` — output printer abstraction. `--ai-output` flag controls format.

## Critical decisions

**Hook type is `func(context.Context) error`.** The context carries cancellation from the runner. `NewShellHook` uses `exec.CommandContext` so shell commands are killed on context cancellation. Never change hooks back to `func() error` — the runner threads `runCtx` through iteration hooks so a SIGINT stops in-flight setup/teardown commands.

**Public API vs CLI are separate layers.** `hooks.go` / `testrig.go` are the library surface. The CLI in `internal/cmd/` wraps that. Don't put CLI logic in the root package.

**Hook catalog drives CLI flags and docs.** Add lifecycle hooks in [`internal/hooks/catalog.go`](internal/hooks/catalog.go) (`Catalog` slice), then wire `RunOptions.Hook`, public `hooks.go` registrar, and `go generate`. Document timing in godoc on registrars in [`hooks.go`](hooks.go) as `// Name registers a hook to <when>.` `hooks.RegisterPersistentFlags` registers Cobra flags from the catalog.
