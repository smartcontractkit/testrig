## Goals

- An easy to import and use test rig for Go projects
- Can be used as a package or CLI
- Used to find and fix flaky tests

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

## Diagnose overhead benchmark

`internal/runner/runner_bench_test.go` defines a side-by-side pair against `internal/runner/testdata/dummy/...`:

- `BenchmarkBaselineGoTest` — raw `go test -json` (the floor).
- `BenchmarkDiagnose` — one `Diagnose` iteration with `parallel=1`.

The overhead `Diagnose` adds is the **delta** in `ns/op`, `B/op`, and `allocs/op` between the two. Run with `just bench_overhead` (uses `-count=3` for variance). For a memory profile of the diagnose pipeline, `just bench_overhead_profile` writes `out/bench/diagnose_mem_runner.txt` (look for `scanIterationJSONL`, `applyTestEvent`, `buildReportFromAggs`). CPU profile is low-signal — parent blocks on the child subprocess.

Drill down into the parent-only pipeline (no subprocess) via the micro-benches in `internal/runner/analyze_bench_test.go`.
