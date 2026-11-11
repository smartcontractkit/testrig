---
id: build
title: Build Report and Flags
type: reference
summary: Field reference for the build object in report.json and the flags that control the build phase.
tags: [diagnose, build, report]
sources:
  - internal/gobuild/**
  - internal/runner/analyze.go
  - internal/runner/diagnose_build.go
  - internal/cmd/diagnose.go
  - internal/config/config.go
  - internal/compare/diff.go
last_verified: 2026-11-11
---

# Build Report and Flags

## Summary

`diagnose` writes a top-level `build` object in `report.json`. It records the mode, any fallback reason, the compile
and link work, the per-iteration floor, and the estimated savings. Two flags control the phase.

## Details

### `build` fields

Durations are Go `time.Duration` values, so `report.json` stores them as integer nanoseconds. The terminal renders
them as clock strings. A report written before the build phase has no `build` key at all.

| Field | Type | Meaning |
| --- | --- | --- |
| `mode` | string | `prebuilt` when one compile and link served every iteration; `go-test` when each iteration ran `go test`. |
| `fallback_reason` | string | Why the run used go-test mode. Omitted when empty. |
| `cache` | string | Compile cache state: `warm`, `partial`, `cold`, or `unknown`. |
| `compile_actions` | int | Compile actions in the compile action graph, including generated test mains. |
| `compile_cached` | int | Compile actions that were cache hits and ran no command. |
| `compile_wall` | duration | Wall time of the compile step. |
| `compile_total` | duration | Sum of `CmdReal` over compile actions that actually ran. |
| `link_wall` | duration | Wall time of the link step. This is the floor. |
| `link_total` | duration | Sum of `CmdReal` over link actions. |
| `vet_total` | duration | Sum of `CmdReal` over vet actions. |
| `binaries` | int | Test binaries written by the link step. |
| `external_link` | int | Binaries likely to link against non-standard cgo dependencies. Heuristic. |
| `stripped` | bool | Whether the link step added `-ldflags=-s -w`. |
| `floor` | duration | Link and vet overhead a `go test` iteration would pay. Equal to `link_wall`. |
| `saved_estimate` | duration | `floor × (iterations − 1)`. Omitted when zero. |

In go-test mode only `mode` and `fallback_reason` are meaningful. The remaining fields keep their zero values, so
`cache` is empty rather than `unknown`, and `compare` shows dashes for them.

An example:

```json
{
  "build": {
    "mode": "prebuilt",
    "cache": "cold",
    "compile_actions": 1910,
    "compile_cached": 78,
    "compile_wall": 41200000000,
    "compile_total": 39900000000,
    "link_wall": 78400000000,
    "link_total": 74200000000,
    "vet_total": 4100000000,
    "binaries": 175,
    "external_link": 12,
    "stripped": true,
    "floor": 78400000000,
    "saved_estimate": 1489600000000
  }
}
```

### Cache values

| Value | Condition |
| --- | --- |
| `warm` | At least 95% of compile actions were cache hits. |
| `partial` | More than 5% and less than 95% were hits. |
| `cold` | At most 5% of compile actions were cache hits. |
| `unknown` | The action graph was missing, malformed, or had no compile actions. |

`unknown` means the metrics degraded, not that the build failed. Wall times are still recorded.

### Flags

| Flag | Default | Behavior |
| --- | --- | --- |
| `--build-once` | `true` | Build test binaries once before the iterations. `false` forces go-test mode and records `fallback_reason: "--build-once=false"`. |
| `--strip-binaries` | `true` | Add `-ldflags=-s -w` to the link step. Ignored when `-ldflags` appears in the `go test` arguments or `GOFLAGS`; a user link flag wins and the binaries are not stripped. |

Both are ordinary `diagnose` flags, not lifecycle hooks. They are available anywhere `diagnose` accepts the other run
flags.

`--strip-binaries=false` keeps symbols and debug information. The run still removes the binaries at the end, but peak
disk use is higher.

### Fallback reasons

The reason is human text; the `--ai-output` token is a short form. Examples:

| Trigger | `fallback_reason` | AI token |
| --- | --- | --- |
| `--build-once=false` | `--build-once=false` | `build-once-off` |
| `-coverprofile` | `-coverprofile needs go test` | `coverprofile` |
| Unknown flag `-foo` | `unknown flag -foo` | `foo` |
| Resource env sets `CGO_ENABLED` | `resource env sets CGO_ENABLED` | `cgo_enabled` |

In go-test mode, iteration durations include compile and link time. In prebuilt mode they contain test execution only,
which is why `compare` warns when it compares runs from different modes.

### Build failure log

A compile or link failure writes `build.log.jsonl` next to the iteration logs. Each line is a `go test -json` shaped
event: `build-output` events carry the go command output and the final `build-fail` event names the failed step
(`compile` or `link`). `build.log.jsonl` is absent when the build succeeds.

The failure sets the run's fail-fast reason to `build-failure` and stops the run before iteration 0. The binary
directory is removed on the way out.

### Compare table

`compare` adds a **Build** table with mode, cache state, compile wall, link wall, floor, binaries, external links, and
stripped. When both reports predate the build phase, the table is omitted. When one report lacks `build`, its cells
show dashes. When both reports have build metrics and the modes differ, the speed section says the iteration durations are not
comparable.
