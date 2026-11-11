# Phase 3 handoff: Compare and docs

## Done

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

## Deviations

- The design did not say what to do when neither report has `build`. The implementation omits the Build section entirely
  rather than printing an all-dash table. The `no-build` golden locks that in.
- Go-test-mode rows are dashes for every metric except mode, not `unknown` cache or zeros. A zero floor or a zero binary
  count would read like measured data; a dash says "not measured in this mode."
- The mode cell appends the fallback reason in parentheses instead of adding a separate row. It keeps the table to eight
  rows and puts the reason next to the mode it explains.
- The mixed-mode note is attached after the Summary table (the speed section) rather than next to the Build table, so it
  sits beside the iteration-time rows it warns about.
- `docs/llms.txt` is generated output from `good-vibes docs index`, not a hand-edited file.

## Open issues

- The golden tests do not cover `cache: unknown`; the cell logic is shared with the other dash cases, but a golden for a
  prebuilt report with `unknown` cache would lock the wording in.
- The Build table has no change column, so compile and link deltas are not computed. The design only asked for the
  values.
- `compare` does not surface binary size or disk use, even though that is a real cost of prebuilt mode.
- The docs describe the flags and report fields, but there is no worked example of reading a real `report.json` build
  section. The reference example is synthetic.

## Next-phase notes

- Phase 3 is the last phase. Close-out should note that `docs/llms.txt` is regenerated, not edited.
- If a later change alters `BuildMetrics`, update three places together: `internal/runner/build_metrics.go`, the field
  table in `docs/reference/build.md`, and `diffBuildRows` in `internal/compare/diff.go` plus its goldens.
- Keep compare's "is this mode meaningful" check (`buildMetricsApply`) in sync with any new modes. A new mode with
  partial metrics needs an explicit decision here, not a silent dash.

## Files touched

- `docs/concepts/build-phase.md`
- `docs/llms.txt`
- `docs/reference/build.md`
- `internal/compare/build_test.go`
- `internal/compare/diff.go`
- `internal/compare/render.go`
- `internal/compare/testdata/build-compare/no-build.golden`
- `internal/compare/testdata/build-compare/prebuilt-go-test.golden`
- `internal/compare/testdata/build-compare/prebuilt-prebuilt.golden`

## Run log

- Mode: 
- Checkpoints: task-6, task-7
- Verify: verify passed: no checks configured
- Rounds used: 1 of 2; verify failures: 0
