#!/bin/bash
# Pre-commit checks. golangci-lint (.golangci.yml) covers formatting (gofumpt, goimports),
# line/function length, complexity, gosec, staticcheck, global state and the panic
# policy; only what it cannot do runs separately (tidy, mock drift, deadcode, govulncheck,
# build, tests with the coverage gate, frontend checks).
# Check-only: it never rewrites or stages files. Fix formatting with
# `golangci-lint fmt` and lint findings by hand.

# Must match golangci/golangci-lint-action in .github/workflows/checks.yml.
GOLANGCI_LINT_VERSION="v2.15.0"
MIN_COVERAGE=90

set -u

echo "=== Running Pre-commit Checks ==="
echo ""

# Color codes
RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

# Track failures
FAILED=0
TMPDIR_PC=$(mktemp -d)
trap 'rm -rf "$TMPDIR_PC"' EXIT

# Go packages: the top-level directories holding Go files git knows about (tracked or
# not ignored). `./...` would also walk ignored local folders such as data/,
# whose data a container may own, and fail on them.
PKGS=()
while IFS= read -r dir; do
    if [ "$dir" = "." ]; then PKGS+=("."); else PKGS+=("./$dir/..."); fi
done < <(git ls-files --cached --others --exclude-standard -- '*.go' \
    | xargs -r -n1 dirname | cut -d/ -f1 | sort -u)

# Helper function to print results
print_result() {
    if [ "$1" -eq 0 ]; then
        echo -e "${GREEN}✓${NC} $2"
    else
        echo -e "${RED}✗${NC} $2"
        FAILED=1
    fi
}

if ! command -v golangci-lint &> /dev/null; then
    echo -e "${RED}✗${NC} golangci-lint not installed (https://golangci-lint.run)"
    exit 1
fi
LOCAL_VERSION=$(golangci-lint version --short 2>/dev/null || true)
if [ -n "$LOCAL_VERSION" ] && [ "v${LOCAL_VERSION#v}" != "$GOLANGCI_LINT_VERSION" ]; then
    echo "note: golangci-lint $LOCAL_VERSION installed, CI pins $GOLANGCI_LINT_VERSION"
fi

# 1. Format (gofumpt + goimports): report, never rewrite
echo "Checking formatting..."
FMT=$(golangci-lint fmt --diff --config .golangci.yml "${PKGS[@]}" 2>&1)
if [ -n "$FMT" ]; then
    echo "$FMT"
    print_result 1 "golangci-lint fmt (run 'golangci-lint fmt' to fix)"
else
    print_result 0 "golangci-lint fmt"
fi

# 2. Lint
echo "Linting..."
golangci-lint run --config .golangci.yml "${PKGS[@]}"
print_result $? "golangci-lint run"

# 2b. go vet and module tidiness
echo "Checking vet and go.mod/go.sum..."
go vet "${PKGS[@]}"
print_result $? "go vet"
cp go.mod "$TMPDIR_PC/go.mod" 2>/dev/null || true
if [ -f go.sum ]; then cp go.sum "$TMPDIR_PC/go.sum"; fi
go mod tidy
if diff -q go.mod "$TMPDIR_PC/go.mod" > /dev/null \
    && { [ ! -f go.sum ] || diff -q go.sum "$TMPDIR_PC/go.sum" > /dev/null; }; then
    print_result 0 "go mod tidy"
else
    cp "$TMPDIR_PC/go.mod" go.mod
    if [ -f "$TMPDIR_PC/go.sum" ]; then cp "$TMPDIR_PC/go.sum" go.sum; fi
    print_result 1 "go mod tidy (go.mod/go.sum not tidy; run 'go mod tidy')"
fi

# 2c. Mock drift (mockery fails on an empty package list, so skip until one is listed)
echo "Checking mocks..."
if grep -q '^  github.com' .mockery.yaml; then
    mocks() {
        git ls-files -z --cached --others --exclude-standard -- '*/mocks/*.go' \
            | sort -z | xargs -0 -r cat | cksum
    }
    BEFORE=$(mocks)
    go tool mockery > /dev/null 2>&1
    AFTER=$(mocks)
    [ "$BEFORE" = "$AFTER" ]
    print_result $? "mock drift"
else
    echo "no interfaces in .mockery.yaml yet: skipped"
fi

# 3. Unused exported code (golangci's unused only sees unexported identifiers)
echo "Checking for dead code..."
DEAD=$(go tool deadcode -test "${PKGS[@]}" 2>&1)
if [ -n "$DEAD" ]; then
    echo "$DEAD"
    print_result 1 "deadcode"
else
    print_result 0 "deadcode"
fi

# 4. Known vulnerabilities in dependencies and the standard library
echo "Checking for vulnerabilities..."
go tool govulncheck "${PKGS[@]}"
print_result $? "govulncheck"

# 5. Build
echo "Building..."
go build "${PKGS[@]}"
print_result $? "build"

# 5b. Simulation of the collector with random device data and faults. Local only: CI does
# not run it. Without -race, which would make it more than ten times slower.
echo "Running the simulation..."
go test -tags simulation -run Simulation -count=1 ./internal/collector
print_result $? "simulation"

# 6. Tests (race detector as in CI) with a per-package coverage gate. Packages without
# test files or without statements are not gated; mocks are generated code; `cmd` is
# not gated (main() calls os.Exit and cannot be tested; its logic lives in run() and app).
echo "Running tests..."
TEST_OUT=$(go test -race -cover "${PKGS[@]}" 2>&1)
TEST_RC=$?
echo "$TEST_OUT"
print_result $TEST_RC "tests"
LOW=$(echo "$TEST_OUT" | awk -v min="$MIN_COVERAGE" '
    /^ok/ && /coverage:/ && !/\/mocks/ && !/pvmon\/cmd\t/ && !/no statements/ {
        for (i = 1; i <= NF; i++) if ($i == "coverage:") {
            pct = $(i + 1); sub(/%/, "", pct)
            if (pct + 0 < min) print $2 " " pct "%"
        }
    }')
if [ -n "$LOW" ]; then
    echo "$LOW"
    print_result 1 "coverage >= ${MIN_COVERAGE}% per package"
else
    print_result 0 "coverage >= ${MIN_COVERAGE}% per package"
fi

# 7. Frontend: typecheck, lint (incl. i18n keys), knip, unit tests, build and bundle budget.
# Skipped only while frontend/ does not exist; needs `npm ci` in frontend/ beforehand.
if [ -f frontend/package.json ]; then
    echo "Checking frontend..."
    (cd frontend && npm run typecheck && npm run lint && npm run knip && npm test \
        && npm run build && ../scripts/bundle-budget.sh)
    print_result $? "frontend (typecheck, lint, knip, test, build, bundle budget)"
else
    echo "frontend/ not present yet: frontend checks skipped"
fi

# 8. Frontend screenshots and axe in the CI Playwright image (needs docker or podman).
if [ -f frontend/package.json ] && command -v docker >/dev/null 2>&1; then
    echo "Checking frontend e2e..."
    make e2e
    print_result $? "frontend e2e (screenshots, axe, pwa)"
else
    echo "docker not available: frontend e2e skipped (CI runs it)"
fi

echo ""
echo "=== Pre-commit Checks Complete ==="

if [ $FAILED -ne 0 ]; then
    echo -e "${RED}Some checks failed. Please fix the issues before committing.${NC}"
    exit 1
fi
echo -e "${GREEN}All checks passed!${NC}"
exit 0
