---
slug: prebuilt-test-binaries
run: 20261111-162249-prebuilt-test-binaries
outcome: done
generated: 2026-11-11T17:12:18Z
---

# Run summary: prebuilt-test-binaries

## Outcome

- Status: done
- Branch: prebuilt-test-binaries
- Final verdict: claude@claude-opus-5-5 approved (issues: 0)

## Totals

- Tasks: 7 of 7
- Phases: 3
- Checkpoints: 8
- Duration: 44m17s
- Cost: $0.94
- Input tokens: 45909421
- Output tokens: 282445
- Cache read tokens: 44803966

## Phases

### Phase 1: Build phase and build metrics

Tasks:

- [x] Add `internal/gobuild` with `Plan`, `Compile`, `Link`, and the actiongraph parser. Wire a build phase into `runDiagnoseIterations` behind `--build-once` (default `false` in this phase), with bindir cleanup on success, error, and cancel. Write `Report.Build`, the terminal build line, and the `--ai-output` field. Iterations still use `go test`. Include the plan table tests, actiongraph parser tests, and the name-collision fixture.
- [x] Add the `buildProgress` helper and the build output from "Terminal output": live and non-TTY step lines, the `[n/total]` link count from the bin dir, the floor line, the summary build line, and the `--ai-output` tokens. Include golden tests for live, non-TTY, and AI output, with styles checked through the existing test printer.
- [x] Move build failures to the build phase when `--build-once` is on: write `build.log.jsonl`, fail fast with `build-failure` before iteration 0, and point `printBuildError` at the new file. Show `❌` on the failed step and `stopped` on interrupt. Include the compile-error fixture and the cancel-during-link test, both checking the printed lines.

Phase totals: 3 checkpoints, 18m52s, $0.27, 13262017 input / 91020 output / 12972256 cache tokens

Checkpoints: task-1, task-2, task-3 Review fixes: 0 Integration checks: 0

#### Done

- Added `internal/gobuild` with:
  - `Plan`, a flag table that classifies `go help testflag` / `go help build` flags into build flags, link-only flags,
    `-test.*` translations, patterns, and passthrough after `-args`. Unknown flags and explicit choices such as
    `-coverprofile`, `-exec`, and `-c` fall back with a token and reason. Resource env that changes build inputs
    (`GOFLAGS`, `CGO_*`, `GOOS`, `GOARCH`, `GOEXPERIMENT`, `CC`, `CXX`) falls back. Defaults copy `cmd/go`:
    `-test.paniconexit0`, `-test.v=test2json`, a 10m timeout when unset, and `-test.count=1` when unset.
  - `Compile`, using `go list -export -test -deps -json -debug-actiongraph`. It keeps pattern-matched packages with
    test files, derives each binary name from the last import path element (skipping a `/vN` element), and flags likely
    external links from test-variant cgo deps.
  - `Link`, grouping packages so no `go test -c -o dir/` call writes two binaries with the same base name, stripping
    with `-ldflags=-s -w` unless the user supplied `-ldflags` in args or `GOFLAGS`, and collecting link/vet metrics from
    one actiongraph per group.
  - Actiongraph parser and `CacheState` boundaries: warm at 95%+ hits, cold at 5% or less, partial between, and
    `unknown` when the graph is missing, malformed, or has no compile actions.
- Wired the build phase into `runDiagnoseIterations` before the iteration table, behind `--build-once` (default
  `false`). A temp `testrig-bin-*` dir is created for the run and removed on success, build failure, and cancel.
- `Report.Build`, the completion JSON `build` field, and the summary build line above the scope line are in place.
- `buildProgress` renders live and non-TTY compile/link steps, the `[n/total]` link count from the binary dir, the floor
  line, cache styling, and vet detail. `--ai-output` gets `build_start`, `build_compile`, and `build_link` tokens.
- Build failures stop before iteration 0 and use the existing `build-failure` fail-fast reason. The failing `go` output
  goes to `build.log.jsonl` as `build-output` plus `build-fail` events, and `printBuildError` now reads whatever path the
  run state points at. Failed steps render `❌`; a canceled build renders `stopped` and writes no build log.
- Tests cover the plan table and defaults, actiongraph warm/cold/partial/malformed/missing, the name-collision fixture,
  live/non-TTY/AI/summary goldens, the real compile-error fixture, a link failure, cancel during link, and config flag
  binding.
- `go test ./...` and `golangci-lint run ./...` pass.

### Phase 2: Run prebuilt binaries

Tasks:

- [x] Vendor `cmd/internal/test2json` into `internal/test2json` with its license. Add the `iterationExecutor` seam, move today's code into `goTestExecutor`, and add `prebuiltExecutor`: `-p` scheduling, package dir and `PWD`, env, test-flag translation and defaults, finish lines, timeout kill, no-test-files events, per-package buffering, and process-group cancel. Land the full equivalence suite with it.
- [x] Turn on `--build-once` by default and add `--strip-binaries`. Make go-test mode record its fallback reason and print the `Accent` "building per iteration" notice, the summary build line, and the `build_mode` AI token. Set `SavedEstimate`. Include the parallel-iterations env test and the cancel-mid-iteration test.

Phase totals: 2 checkpoints, 17m25s, $0.45, 26783758 input / 134007 output / 26334432 cache tokens

Checkpoints: task-4, task-5 Review fixes: 0 Integration checks: 0

#### Done

- Vendored `cmd/internal/test2json` from Go 1.27.3 as `internal/test2json` with its BSD `LICENSE`. The header names the
  Go version and says to keep it in sync with `go.mod`.
- Added the `iterationExecutor` seam (`Run(ctx, iterationRun, io.Writer)`) and moved the old `go test -json` process
  handling into `goTestExecutor`. `diagnoseIteration` now owns only the results file, buffering, and progress UI.
- Added `prebuiltExecutor`:
  - Runs at most `P` binaries at once (`-p` when set, else `runtime.GOMAXPROCS(0)`). With `-bench` set it runs one at
    a time, as `cmd/go` does.
  - Runs each binary in its package directory with `PWD` set and `os.Environ()` plus the resource env.
  - Applies the translated `-test.*` flags, with a non-zero harness shuffle seed replacing any user `-test.shuffle`.
  - Writes the `ok` / `FAIL` finish line and lets the converter emit the final package event with `Elapsed`.
  - Kills wedged binaries at `cmd/go`'s backup deadline (`timeout + max(1min, timeout/10)`), unless bench or fuzz flags
    are present.
  - Emits `start` / `?   pkg [no test files]` / `skip` events for packages with no test files, without a process.
  - Buffers events per package and flushes each finished package under one lock, including on cancel.
  - Interrupts the process group on `runCtx` cancel, waits 5s, then kills it.
- Added process-group isolate, interrupt, and kill helpers in `internal/procgroup`, shared by `gobuild.runGo` and the
  runner executors so cancel behavior cannot drift between them.
- Turned `--build-once` on by default. `--build-once=false` still records `Mode: "go-test"` and reason
  `--build-once=false`, and prints the notice, summary line, and AI token.
- Added `--strip-binaries` (default `true`) and threaded it through `config.App` to `gobuild.Plan.Strip`. A user
  `-ldflags` still turns stripping off.
- Go-test mode now prints the `Accent` `building per iteration: <reason> · iteration times include compile and link`
  notice, keeps the summary build line (`build per iteration (<reason>)`), and emits
  `build_mode go-test reason=<token>` once in `--ai-output`.
- Kept `SavedEstimate` set from the build phase (`Floor × (iterations − 1)`).
- Tests landed:
  - `TestPrebuiltEquivalence` runs each fixture module through both executors and compares normalized events and
    reports. Scenarios: mixed pass/fail/panic/exit/empty/no-match/collision, no matching tests, `-args` passthrough,
    fixed-seed `-shuffle` (ordered comparison), `-v`, `-p=1`, `-timeout=1s`, and `-race`.
  - `TestPrebuiltParallelIterationsUseWorkerEnv` runs three workers with distinct env and checks each binary sees its
    own worker's env.
  - `TestPrebuiltCancelMidIteration` cancels one running package, checks the finished package is still logged, the
    canceled process is reaped, and the temp binary dir is removed.
  - `TestGoTestModeReportsFallback`, `TestPrebuiltIterationTestFlags`, `TestTestKillTimeout`, `TestTestTimeoutFromFlags`,
    `TestHasBenchOrFuzz`, `TestPrebuiltNoTestFilesEvents`, and `TestWritePrebuiltFinish`.
- `go test ./... -count=1` and `golangci-lint run ./...` pass.

### Phase 3: Compare and docs

Tasks:

- [x] Add the Build table and the mixed-mode speed note to `compare`, with golden tests for prebuilt vs prebuilt, prebuilt vs go-test, and an old report without `build`.
- [x] Docs: add a concept doc for the build phase (modes, fallback rules, floor, cache states, hooks caveat, temp dir) and a reference doc for the `build` report fields and the two flags. Run `good-vibes docs lint` and `good-vibes docs index`.

Phase totals: 3 checkpoints, 8m0s, $0.21, 5863646 input / 57418 output / 5497278 cache tokens

Checkpoints: task-6, task-7, final Review fixes: 0 Integration checks: 0

#### Done

- `compare` now renders a **Build** table between Runs and Summary. Rows: mode, cache, compile wall, link wall, floor,
  binaries, external links, stripped. It reuses the existing `row` shape, `durationOrDash`, and table renderer.
- The table only appears when at least one report has a `build` object. When both reports predate the build phase the
  section is omitted. When one side lacks `build`, its cells render as `—`.
- Go-test-mode reports show `mode` and its fallback reason, for example `go-test (-coverprofile needs go test)`. Their
  per-step cells are dashes, because only `Mode` and `FallbackReason` are meaningful in that mode.
- Added the mixed-mode note after the Summary table: when both reports carry a mode and the modes differ, a
  `> [!NOTE]` warns that iteration wall times are not comparable because go-test durations include the compile and link
  floor.
- Added three golden tests under `internal/compare/testdata/build-compare/`:
  - `prebuilt-prebuilt.golden`: two prebuilt runs, no note.
  - `prebuilt-go-test.golden`: mixed modes, with the note.
  - `no-build.golden`: two old reports, no Build section, no note.
- Added unit tests for the one-sided table (`—` cells) and for the note only firing on mixed modes.
- Added `docs/concepts/build-phase.md`: two modes, plan and fallback rules, compile and link steps, floor and savings
  estimate, iteration execution, cache states, hooks-and-source-changes caveat, failure handling, temp dir location, and
  trade-offs.
- Added `docs/reference/build.md`: every `build` field with type and meaning, cache values, the two flags,
  fallback-reason/AI-token examples, `build.log.jsonl`, and the compare table behavior.
- Regenerated `docs/llms.txt` with `good-vibes docs index`. `good-vibes docs lint`, `go test ./... -count=1`, and
  `golangci-lint run ./...` are clean.

## Final gate

- Rounds used: 1; verify failures: 0
- Final verify: no commands configured
- Open issues: none
