# gsvalidator — rule-driven validation for SQLite databases
# Install `just` from https://github.com/casey/just

# Default: show available recipes.
default:
    @just --list

# Run the full test suite with race detection.
test:
    go test -race -count=1 ./...

# Verbose test output (for debugging a single flake).
test-v:
    go test -v -race -count=1 ./...

# Coverage report, written to coverage.html.
test-cover:
    go test -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html

# gofmt every package.
fmt:
    gofmt -w .

# Static analysis via `go vet`.
vet:
    go vet ./...

# staticcheck at a pinned version via `go run` (no install needed).
lint:
    go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...

# Format + vet + tidy — cheap pre-commit hygiene.
check: fmt vet
    go mod tidy

# Tidy go.mod on its own.
tidy:
    go mod tidy

# Check that downloaded modules match go.sum.
verify:
    go mod verify
