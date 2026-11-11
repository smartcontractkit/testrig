# Phase 1 handoff: Build phase and build metrics

## Done

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

## Deviations

- The build phase is `runDiagnoseBuildPhase` plus an injectable `buildPhaseOps` struct, not a `buildPhase` type. The
  `iterationExecutor` seam is still deferred to phase 2, as planned.
- `--build-once` defaults to `false` for this phase, per the phase instruction. The design's CLI section describes the
  final default of `true`; phase 2 flips it.
- `--strip-binaries` is not exposed yet. `runDiagnoseBuildPhase` passes `Strip: true`, and `Plan` turns stripping off
  when the user supplied `-ldflags` in args or `GOFLAGS`. Phase 2 adds the flag.
- `SavedEstimate` is already computed here (`Floor × (iterations − 1)`), even though the phase 2 task mentions setting
  it. It is only reporting.
- The link grouping key is the binary base name (last import path element), not the package clause. Verified against the
  toolchain: `a/foo` with `package bar` writes `foo.test`, and `a/util` plus `b/util` collides regardless of package
  name. The design's Context and Approach now say this.
- A fallback returns `Mode: "go-test"` with its reason, but the `Accent` "building per iteration" notice and the
  `build_mode` AI token are not printed yet; those are phase 2 tasks.
- On a compile failure, the build log uses `stderr` diagnostics (falling back to `stdout`) instead of the partial
  `go list -json` dump that `go list` writes before failing.
- Wall times reuse `formatDiagnoseWallClock`, so lines read `41.20s` and `1m18.40s` rather than the one-decimal samples
  in the design. This keeps the build lines consistent with the rest of the output.
- `printBuildError` takes a log path now. `DiagnoseRunState.buildErrorLogPath` keeps the old
  `iteration-N.log.jsonl` behavior for go-test build failures and points at `build.log.jsonl` for build-phase failures.
- The `build_fail step=...` AI token prints only when the failing step is known. A go-test-mode build failure keeps the
  pre-existing `bf_stop` token.

## Open issues

- **Shuffle precedence.** `Plan` translates a user `-shuffle` to `-test.shuffle=<value>`, while `runDiagnoseIterations`
  still appends `-shuffle=<seed>` per iteration through `buildDiagnoseArgs`. Phase 2 must make the per-iteration seed
  win and keep recording it in `report.json`. Strip the harness `-shuffle` before `Plan` or override the test flag.
- **`-v` duplication.** A user `-v` produces both `-test.v=true` and `-test.v=test2json`; the latter is last, so the
  binary ends up in test2json mode. Confirm this matches `cmd/go` in the equivalence suite before relying on it.
- **Toolchain coverage.** Actiongraph tests use recorded JSON only. No test currently runs the real
  `-debug-actiongraph` flag against the toolchain. The equivalence suite in phase 2 will exercise it; until then the
  hidden-flag risk is untested.
- **Cancel coverage.** Cancel during link is tested at the build phase. There is no full `RunIterations` test for a
  build canceled during compile/link; phase 2's cancel-mid-iteration test should not regress it.
- **Empty graph.** `CacheState` returns `unknown` when the graph parses but has zero compile actions. That is
  intentional, but reports will show `unknown` for tiny modules with nothing to compile.
- **Bin dir ownership.** Cleanup lives in `runDiagnoseIterations` via `defer` around the returned `binDir`. If phase 2
  moves binary lifetime behind an executor, keep one owner for that defer.

## Next-phase notes

- `gobuild.Build.Binaries` already carries `ImportPath`, `NodeName`, `Path`, `Dir`, and `ExternalLink`.
  `gobuild.BuildPlan` carries `TestFlags`, `Passthrough`, and `P`, so the prebuilt executor can consume both without
  re-parsing args.
- `state.Build` is set before iterations and copied into `Report.Build` in `FinishDiagnoseAnalysis` and the completion
  JSON. Keep setting it in fallback mode too; the summary line and compare depend on the mode and reason.
- `BuildMetrics.Floor` is currently `LinkWall`. If the link step is split or timed differently, update the metrics and
  the floor line together.
- `overallBuildLine` already handles `go-test` mode using `FallbackReason`, so the phase 2 notice and AI token can reuse
  that reason rather than adding another source of truth.
- Add new build steps to `buildProgress`; it owns live redraw, the non-TTY started line, the failed `❌`, and the
  canceled `stopped` line.
- `writeBuildFailureLog` writes only `build-output` and `build-fail` events and skips blank lines. `printBuildError` and
  `extractBuildOutput` consume that shape.
- The compile-error fixture is `internal/runner/testdata/prebuilt/compileerror`; the name-collision fixture is
  `internal/gobuild/testdata/namecollision`. Reuse them for the phase 2 integration suite.

## Files touched

- `internal/cmd/diagnose.go`
- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/gobuild/actiongraph.go`
- `internal/gobuild/actiongraph_test.go`
- `internal/gobuild/compile.go`
- `internal/gobuild/exec.go`
- `internal/gobuild/exec_other.go`
- `internal/gobuild/exec_unix.go`
- `internal/gobuild/link.go`
- `internal/gobuild/link_test.go`
- `internal/gobuild/plan.go`
- `internal/gobuild/plan_test.go`
- `internal/gobuild/testdata/actiongraph/cold.json`
- `internal/gobuild/testdata/actiongraph/malformed.json`
- `internal/gobuild/testdata/actiongraph/partial.json`
- `internal/gobuild/testdata/actiongraph/warm.json`
- `internal/gobuild/testdata/namecollision/a/util/util_test.go`
- `internal/gobuild/testdata/namecollision/b/util/util_test.go`
- `internal/gobuild/testdata/namecollision/go.mod`
- `internal/runner/analyze.go`
- `internal/runner/build_metrics.go`
- `internal/runner/diagnose_build.go`
- `internal/runner/diagnose_build_failure_test.go`
- `internal/runner/diagnose_build_golden_test.go`
- `internal/runner/diagnose_build_progress.go`
- `internal/runner/diagnose_build_test.go`
- `internal/runner/diagnose_output.go`
- `internal/runner/diagnose_overall_table.go`
- `internal/runner/runner.go`
- `internal/runner/runner_test.go`
- `internal/runner/testdata/build/ai.golden`
- `internal/runner/testdata/build/live.golden`
- `internal/runner/testdata/build/nontty.golden`
- `internal/runner/testdata/build/summary.golden`
- `internal/runner/testdata/prebuilt/compileerror/bad/bad_test.go`
- `internal/runner/testdata/prebuilt/compileerror/go.mod`

## Run log

- Mode: 
- Checkpoints: task-1, task-2, task-3
- Verify: verify passed: no checks configured
- Rounds used: 1 of 2; verify failures: 0
