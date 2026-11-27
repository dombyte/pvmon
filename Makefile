BINARY_NAME := pvmon

# Version info: <latest tag without "v">-<short commit> (same format as goreleaser),
# "dev" outside a git checkout or without a tag.
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
TAG := $(patsubst v%,%,$(shell git describe --tags --abbrev=0 2>/dev/null))
VERSION := $(if $(and $(TAG),$(filter-out unknown,$(COMMIT))),$(TAG)-$(COMMIT),dev)
DATE := $(shell date -u +'%Y-%m-%dT%H:%M:%SZ')

LDFLAGS := -s -w
BUILD_FLAGS := -trimpath -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE) $(LDFLAGS)"

# Embeds whatever frontend/dist and docs/dist hold: run `make assets` first (or `make all`).
.PHONY: build
build:
	CGO_ENABLED=0 go build $(BUILD_FLAGS) -o $(BINARY_NAME) ./cmd

# Web assets embedded into the binary; the frontend and the Swagger UI are built with the
# same version (VITE_APP_VERSION) as the binary.
.PHONY: frontend
frontend:
	@if [ -f frontend/package.json ]; then \
		npm --prefix frontend ci && VITE_APP_VERSION=$(VERSION) npm --prefix frontend run build; \
	else echo "frontend/ has no package.json yet: skipping"; fi

.PHONY: docs
docs:
	npm --prefix docs ci && VITE_APP_VERSION=$(VERSION) npm --prefix docs run build

# Builds the documentation site for GitHub Pages. It is not part of the binary.
.PHONY: docs-site
docs-site:
	npm --prefix docs ci && VITE_APP_VERSION=$(VERSION) npm --prefix docs run build:site

.PHONY: assets
assets: frontend docs

# Checks the licenses of the third-party code and that
# LICENSES/THIRD_PARTY_NOTICES.md is up to date. license-update writes the file.
.PHONY: license
license: frontend docs
	node scripts/licenses.mjs --check

.PHONY: license-update
license-update: frontend docs
	node scripts/licenses.mjs

.PHONY: all
all: assets build

# Dev image with the same build information as `make build`.
.PHONY: docker
docker:
	docker build -t $(BINARY_NAME):dev --build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) --build-arg BUILD_DATE=$(DATE) .

# Go packages under the directories git knows (see scripts/pre-commit.sh): `./...` would
# also walk ignored local folders such as data/ and fail on container-owned data.
GO_PKGS := $(addprefix ./,$(addsuffix /...,$(shell git ls-files --cached --others \
	--exclude-standard -- '*.go' | xargs -r -n1 dirname | cut -d/ -f1 | sort -u)))

.PHONY: test
test:
	go test -race $(GO_PKGS)

# Full simulation: every profile in every scenario. PVMON_SIM_SEED=<n> repeats a run.
SIM_TEST := PVMON_SIM=full go test -race -tags simulation -run Simulation -count=1 \
	./internal/collector

.PHONY: sim
sim:
	$(SIM_TEST)

# Full simulation with new random device data; prints the seed to repeat a failure.
.PHONY: sim-rand
sim-rand:
	@seed=$$(od -An -N4 -tu4 /dev/urandom | tr -d ' '); \
	echo "repeat with: make sim PVMON_SIM_SEED=$$seed"; \
	PVMON_SIM_SEED=$$seed $(SIM_TEST)

.PHONY: lint
lint:
	golangci-lint run --config .golangci.yml $(GO_PKGS)

.PHONY: fmt
fmt:
	golangci-lint fmt --config .golangci.yml $(GO_PKGS)

# Regenerate mocks (skipped while .mockery.yaml lists no package).
.PHONY: mocks
mocks:
	@if grep -q '^  github.com' .mockery.yaml; then go tool mockery; \
	else echo ".mockery.yaml lists no package yet: skipping"; fi

.PHONY: clean
clean:
	rm -f $(BINARY_NAME)
	rm -rf frontend/dist docs/dist dist coverage.out

.PHONY: run
run: build
	./$(BINARY_NAME)

# UI screenshots + axe (spec 15) in the same Playwright image as CI, so local baselines
# match CI pixel for pixel. The version follows @playwright/test (Renovate groups both).
PLAYWRIGHT_IMAGE := mcr.microsoft.com/playwright:v1.65.1-noble
E2E_RUN = docker run --rm --ipc=host --user $$(id -u):$$(id -g) -e HOME=/tmp \
	-v $(CURDIR):/work:z -w /work/frontend $(PLAYWRIGHT_IMAGE)

.PHONY: e2e
e2e:
	$(E2E_RUN) npx playwright test

# Rewrites frontend/e2e/__screenshots__; review the images before committing them.
.PHONY: e2e-update
e2e-update:
	$(E2E_RUN) npx playwright test --update-snapshots=all

# Same checks as CI; check-only, never rewrites files.
.PHONY: check
check:
	./scripts/pre-commit.sh
