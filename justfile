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
    go test -bench=. -benchmem -run=^$ ./... -cpu=2,4,8

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
dogfood iterations="3":
    go run ./cmd/testrig diagnose --iterations {{iterations}} -- ./...





