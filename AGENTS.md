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

<!-- good-vibes:begin -->
<!-- Managed by `good-vibes init`. Edits inside this block are overwritten. -->

## good-vibes

This repo uses `good-vibes` for change management. Every significant change moves through five steps, each with a skill
in `.agents/skills/`:

### The Flow

1. **Propose** (`propose`). A change starts in `changes/<slug>/proposal.md`. It explains *why* the change is worth
   making, sets goals and non-goals, and gives the high-level direction and why it beat the alternatives. File paths,
   schemas, and signatures belong in the design. When another change must finish first, record it in the `depends_on`
   front matter with `good-vibes propose <slug> --depends-on a,b` or by editing the list, not in prose. `check` reports
   bad entries, `status` shows the wait, and `implement` refuses to start until they are done.
2. **Accept.** A person runs `good-vibes accept <slug> --by <names> [--in <review-url>]`. Agents leave their own
   proposals for a person to accept.
3. **Design** (`design`). `changes/<slug>/design.md` says *how* the change works. Its `## TODO` section lists the phases
   and tasks in order. Keep order between changes in `depends_on`, not in phases or prose.
4. **Implement** (`implement`, `docs`). Work through the TODO phases. Update `docs/` in the same change, then write the
   phase handoff in `changes/<slug>/handoffs/`.
5. **Close** (`close`). Write `changes/<slug>/closeout.md`. After a person reviews it, `good-vibes done <slug>` folds it
   into an ADR in `docs/decisions/` and deletes the change folder.

### Rules

- Read the change folder before writing code. The design is the contract.
- Only the implement flow and the binary tick checkboxes in `design.md`.
- Leave `design.md` alone while a `good-vibes implement` run is active. The worktree's copy is the truth.
- `docs/` describes the system as it is now. A change rewrites the docs it invalidates and leaves history to the ADR.
- Every doc has YAML front matter. Run `good-vibes docs lint` after writing, and `good-vibes docs index` after adding or
  renaming a doc.
- Use `good-vibes` commands for status and ADR files. Run `good-vibes --help` for the command list.
<!-- good-vibes:end -->
