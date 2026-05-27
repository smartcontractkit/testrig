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

# Run the in-repo testrig CLI against this repository (smoke-test diagnose).
dogfood diagnose iterations="3":
    go run ./cmd/testrig diagnose --iterations {{iterations}} -- ./...
