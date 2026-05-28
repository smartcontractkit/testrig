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

# Run benchmarks with memory stats and specific CPU counts
bench:
    go test -bench=. -benchmem -run=^$ ./...

# Compare diagnose overhead vs raw `go test -json` on the dummy target.
# Overhead = ns/op, B/op, allocs/op delta between BenchmarkDiagnose and BenchmarkBaselineGoTest.
bench_overhead:
    go test ./internal/runner/ -run='^$' -bench='^Benchmark(BaselineGoTest|Diagnose)$' -benchmem -count=3

# Memory profile of BenchmarkDiagnose (CPU is low-signal; subprocess-bound).
bench_overhead_profile:
    #!/usr/bin/env bash
    set -euo pipefail
    out_dir=out/bench
    mkdir -p "$out_dir"
    go test ./internal/runner/ -run='^$' -bench='^BenchmarkDiagnose$' -benchmem -count=1 \
        -memprofile="$out_dir/diagnose_mem.prof" 2>&1 | tee "$out_dir/diagnose_bench.txt"
    go tool pprof -top -nodecount=30 -focus='testrig/internal/runner' "$out_dir/diagnose_mem.prof" \
        > "$out_dir/diagnose_mem_runner.txt"
    echo "Mem hotspots: $out_dir/diagnose_mem_runner.txt"

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
