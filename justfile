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

# Install lefthook, hook CLI deps, and sync pre-commit/pre-push git hooks.
# Requires: go, golangci-lint (on PATH).
lefthook_version := "v2.1.8"
actionlint_version := "v1.7.12"

lefthook:
    #!/usr/bin/env bash
    set -euo pipefail
    export PATH="$(go env GOPATH)/bin:${PATH}"
    need() { command -v "$1" >/dev/null; }
    go_install() {
      local bin="$1" pkg="$2"
      if need "$bin"; then
        echo "$bin: $(command -v "$bin")"
        return
      fi
      echo "install $pkg"
      go install "$pkg"
    }
    command -v golangci-lint >/dev/null || {
      echo "golangci-lint not on PATH; install it first" >&2
      exit 1
    }
    go mod download
    go_install lefthook "github.com/evilmartians/lefthook/v2@{{lefthook_version}}"
    go_install actionlint "github.com/rhysd/actionlint/cmd/actionlint@{{actionlint_version}}"
    lefthook install
    echo "lefthook hooks installed (pre-commit, pre-push)"

# Run the in-repo testrig CLI against this repository (smoke-test diagnose).
dogfood iterations="3":
    go run ./cmd/testrig diagnose --iterations {{iterations}} -- ./...



