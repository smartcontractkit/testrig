# List available recipes
default:
    @just --list

# Run linting with auto-fixes
lint:
    golangci-lint run ./... --fix

# Run tests with coverage reporting
test:
    go tool gotestsum -- -cover ./...

# Run tests with the race detector enabled
test_race:
    go tool gotestsum -- -race ./...

# Run standard benchmarks
bench:
    go test -bench=. -benchmem -run=^$ ./...

# Diagnose overhead matrix (dummy; fast). 5 runs averaged per cell; TESTRIG_BENCH_OVERHEAD_RUNS=3 to override.
bench_overhead_matrix_dummy:
    TESTRIG_BENCH_OVERHEAD=1 go test ./internal/runner/ -run='^TestDiagnoseOverhead_Dummy$' -count=1 -v

# Run benchmark to measure diagnose overhead against the full testrig module (./...); slow.
bench_overhead_matrix_dogfood:
    TESTRIG_BENCH_OVERHEAD=1 go test ./internal/runner/ -run='^TestDiagnoseOverhead_Dogfood$' -count=1 -v

# Run benchmarks to measure diagnose overhead for both dummy and dogfood targets.
bench_overhead_matrix: bench_overhead_matrix_dummy bench_overhead_matrix_dogfood

# Local GoReleaser dry-run (snapshot)
goreleaser:
    goreleaser release --clean --snapshot

install:
    go install ./cmd/testrig

# Verify lefthook hook CLIs are on PATH, then sync git hooks.
lefthook:
    #!/usr/bin/env bash
    set -euo pipefail
    missing=()
    for cmd in lefthook betterleaks codespell actionlint golangci-lint go; do
      if ! command -v "$cmd" >/dev/null; then
        missing+=("$cmd")
      fi
    done
    if [ "${#missing[@]}" -gt 0 ]; then
      echo "Missing dependencies (install these, then re-run just lefthook):" >&2
      printf '  - %s\n' "${missing[@]}" >&2
      exit 1
    fi
    go mod download
    lefthook install

# Run the in-repo testrig CLI against this repository (smoke-test diagnose).
dogfood_cli iterations="3" parallel-iterations="3":
    go run ./cmd/testrig diagnose --iterations {{iterations}} --parallel-iterations {{parallel-iterations}} -- ./...

dogfood_native iterations="3" parallel-iterations="3":
    go tool test diagnose --iterations {{iterations}} --parallel-iterations {{parallel-iterations}} -- ./...
