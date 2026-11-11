# Phase 2 handoff: Run prebuilt binaries

## Done

- Vendored `cmd/internal/test2json` from Go 1.27.3 as `internal/test2json` with its BSD `LICENSE`. The header names the
  Go version and says to keep it in sync with `go.mod`.
- Added the `iterationExecutor` seam (`Run(ctx, iterationRun, io.Writer)`) and moved the old `go test -json` process
  handling into `goTestExecutor`. `diagnoseIteration` now owns only the results file, buffering, and progress UI.
- Added `prebuiltExecutor`:
  - Runs at most `P` binaries at once (`-p` when set, else `runtime.GOMAXPROCS(0)`).
  - Runs each binary in its package directory with `PWD` set and `os.Environ()` plus the resource env.
  - Applies the translated `-test.*` flags, with a non-zero harness shuffle seed replacing any user `-test.shuffle`.
  - Writes the `ok` / `FAIL` finish line and lets the converter emit the final package event with `Elapsed`.
  - Kills wedged binaries at `cmd/go`'s backup deadline (`timeout + max(1min, timeout/10)`), unless bench or fuzz flags
    are present.
  - Emits `start` / `?   pkg [no test files]` / `skip` events for packages with no test files, without a process.
  - Buffers events per package and flushes each finished package under one lock, including on cancel.
  - Interrupts the process group on `runCtx` cancel, waits 5s, then kills it.
- Added process-group interrupt and kill helpers in `diagnose_process_{unix,other}.go`; `goTestExecutor` uses the same
  helpers so cancel behavior is shared.
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

## Deviations

- The phase 1 shuffle issue is resolved inside the executor, not by stripping the harness flag before `Plan`:
  `iterationTestFlags` drops any existing `-test.shuffle=` when the harness supplies a non-zero seed. A user
  `-shuffle` is still translated by `Plan` for the seed-0 case. The fixed-seed equivalence scenario verifies order.
- The phase 1 `-v` duplication concern is resolved: injected flags come before user flags, so an explicit `-v` becomes
  `-test.v=true` after the `-test.v=test2json` default. The verbose equivalence scenario passes.
- The binary is treated as having no test output, not as missing a `FAIL` line, when deciding to write `exit status N`.
  A quick check against `go test -json` confirms that a `TestMain` that prints and then exits 1 gets no `exit status`
  line; only a silent exit does. The design's Approach now says "no test output".
- The timeout kill grace uses `timeout + max(1min, timeout/10)`, matching `cmd/go`, rather than a flat plus-one-minute.
  The design's Approach now says so.
- On a non-zero exit the executor always writes the `^VFAIL` framing line. A failing binary's own `FAIL` output stays
  in the log, but the executor does not try to detect it first.
- `prebuiltExecutor` is built from `gobuild.BuildPlan` plus the binary and no-test package lists, not from
  `gobuild.Build`. It holds no temp-dir ownership; `runDiagnoseIterations` still owns the single `RemoveAll` defer.
- `goTestExecutor` also uses the shared process-group helpers, so a hard cancel interrupts the whole `go test` group
  rather than only its leader.
- The `build_mode` AI token falls back to `reason=go-test` if a fallback has no short token, so the token is still
  emitted when the reason is unknown.

## Open issues

- Windows and other non-unix targets fall back to signaling only the direct child, not a process group. The phase does
  not add a Windows-specific test.
- The long-timeout kill path is covered by `TestTestKillTimeout` only; no integration test waits out a multi-minute
  timeout.
- Cancel during compile or link is still tested at the build-phase level, not through a full `RunIterations` call.
  `TestPrebuiltCancelMidIteration` covers cancel during an iteration only.
- The equivalence suite does not cover bench, fuzz, `-cover`, or `-exec`, because those fall back to go-test mode by
  design. It also does not compare stdout ordering between packages, only per-package event sets and test run order.
- The race fixture depends on the race detector being available on the host; the test is skipped only by `-short`, not
  by race support.
- The hooks-change-sources caveat and the temp-dir location are still undocumented. Both are phase 3 docs work.
- `compare` still has no Build table or mixed-mode note, so two reports with different modes currently look comparable.

## Next-phase notes

- `Report.Build` is set in both modes. In go-test mode only `Mode` and `FallbackReason` are meaningful; compare must
  handle `nil` for old reports and `Cache == "unknown"` for missing actiongraphs.
- `overallBuildLine` already renders both modes. Reuse `Build.FallbackReason` for labels rather than adding another
  source of truth.
- The `build_mode` token is printed once by `printBuildModeNotice` before the iteration table. Do not print it again in
  `FinishDiagnoseAnalysis`.
- `runPrebuiltEquivalenceCase` and the `testdata/prebuilt/` modules are reusable for any later end-to-end check. The
  `basic` module covers pass, fail, panic, `TestMain` exits, testdata paths, no tests, name collisions, shuffle, args,
  worker env, and empty packages.
- `internal/test2json` is a direct copy. If the Go version in `go.mod` changes, resync it and the license together.
- `prebuiltExecutor.iterationTestFlags` is the single place harness shuffle overrides user flags; keep report seeds and
  binary flags derived from the same value.
- The build phase still owns metrics. If phase 3 changes the floor or link timing, update `BuildMetrics` and the floor
  line together.
- `--build-once` and `--strip-binaries` are ordinary diagnose flags (not hooks), so the hook catalog stays untouched.

## Files touched

- `.golangci.yaml`
- `internal/cmd/diagnose.go`
- `internal/config/config.go`
- `internal/config/config_test.go`
- `internal/gobuild/compile.go`
- `internal/gobuild/plan.go`
- `internal/gobuild/plan_test.go`
- `internal/runner/diagnose_build.go`
- `internal/runner/diagnose_build_failure_test.go`
- `internal/runner/diagnose_build_test.go`
- `internal/runner/diagnose_process_other.go`
- `internal/runner/diagnose_process_unix.go`
- `internal/runner/go_test_executor.go`
- `internal/runner/iteration_executor.go`
- `internal/runner/prebuilt_cancel_test.go`
- `internal/runner/prebuilt_equivalence_test.go`
- `internal/runner/prebuilt_executor.go`
- `internal/runner/prebuilt_executor_test.go`
- `internal/runner/prebuilt_mode_test.go`
- `internal/runner/prebuilt_parallel_env_test.go`
- `internal/runner/runner.go`
- `internal/runner/testdata/prebuilt/basic/args/args_test.go`
- `internal/runner/testdata/prebuilt/basic/collision/a/util/util_test.go`
- `internal/runner/testdata/prebuilt/basic/collision/b/util/util_test.go`
- `internal/runner/testdata/prebuilt/basic/empty/empty.go`
- `internal/runner/testdata/prebuilt/basic/exit0/exit0_test.go`
- `internal/runner/testdata/prebuilt/basic/exitmain/exitmain_test.go`
- `internal/runner/testdata/prebuilt/basic/fail/fail_test.go`
- `internal/runner/testdata/prebuilt/basic/go.mod`
- `internal/runner/testdata/prebuilt/basic/nomatch/nomatch_test.go`
- `internal/runner/testdata/prebuilt/basic/panic/panic_test.go`
- `internal/runner/testdata/prebuilt/basic/pass/pass_test.go`
- `internal/runner/testdata/prebuilt/basic/shuffle/shuffle_test.go`
- `internal/runner/testdata/prebuilt/basic/testdata/testdata/value.txt`
- `internal/runner/testdata/prebuilt/basic/testdata/testdata_test.go`
- `internal/runner/testdata/prebuilt/basic/worker/worker_test.go`
- `internal/runner/testdata/prebuilt/cancel/go.mod`
- `internal/runner/testdata/prebuilt/cancel/quick/quick_test.go`
- `internal/runner/testdata/prebuilt/cancel/slow/slow_test.go`
- `internal/runner/testdata/prebuilt/race/go.mod`
- `internal/runner/testdata/prebuilt/race/race_test.go`
- `internal/runner/testdata/prebuilt/timeout/go.mod`
- `internal/runner/testdata/prebuilt/timeout/timeout_test.go`
- `internal/test2json/LICENSE`
- `internal/test2json/test2json.go`

## Run log

- Mode: 
- Checkpoints: task-4, task-5
- Verify: verify passed: no checks configured
- Rounds used: 1 of 2; verify failures: 0
