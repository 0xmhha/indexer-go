.PHONY: all build build-all check-chain clean test test-short test-chain test-race test-cache-lifecycle test-postgres test-slo test-examples lint lint-install coverage generate tools help

# Variables
# CHAIN selects the binary: empty builds indexer (generic EVM, ./cmd/indexer),
# a chain under x/ builds indexer-<chain> (./x/<chain>/cmd/indexer-<chain>).
CHAIN?=
CHAINS=$(notdir $(wildcard x/*))
ifeq ($(CHAIN),)
BINARY_NAME=indexer
MAIN_PATH=./cmd/indexer
else
BINARY_NAME=indexer-$(CHAIN)
MAIN_PATH=./x/$(CHAIN)/cmd/indexer-$(CHAIN)
endif
BUILD_DIR=./build
COVERAGE_FILE=coverage.out

# Version information
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT?=$(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
BUILD_TIME?=$(shell date -u '+%Y-%m-%d_%H:%M:%S')

# Go commands
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=$(GOCMD) fmt
GOVET=$(GOCMD) vet

# golangci-lint is built with the active Go toolchain: a binary built with an
# older Go than the one compiling the code fails to analyze it.
GOLANGCI_LINT_VERSION=v2.14.0
GOLANGCI_LINT=$(shell $(GOCMD) env GOPATH)/bin/golangci-lint

# Build flags with version information
LDFLAGS=-ldflags "-s -w \
	-X github.com/0xmhha/indexer-go/cmd/cli.version=$(VERSION) \
	-X github.com/0xmhha/indexer-go/cmd/cli.commit=$(COMMIT) \
	-X github.com/0xmhha/indexer-go/cmd/cli.buildTime=$(BUILD_TIME)"

all: clean build test

# check-chain refuses a CHAIN that has no binary under x/.
check-chain:
	@test -z "$(CHAIN)" || test -d "$(MAIN_PATH)" || (echo "CHAIN=$(CHAIN) has no binary at $(MAIN_PATH); chains: $(CHAINS)"; exit 1)

## build: Build the binary (CHAIN=<chain> for indexer-<chain>; chains are the directories under x/)
build: check-chain
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

## build-all: Build indexer and every indexer-<chain>
build-all:
	@$(MAKE) --no-print-directory build CHAIN=
	@for c in $(CHAINS); do $(MAKE) --no-print-directory build CHAIN=$$c || exit 1; done

## build-dev: Build for development (with debug info; CHAIN as for build)
build-dev: check-chain
	@echo "Building $(BINARY_NAME) (development)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Development build complete: $(BUILD_DIR)/$(BINARY_NAME)"

## clean: Remove build artifacts
clean:
	@echo "Cleaning..."
	$(GOCLEAN)
	@rm -rf $(BUILD_DIR)
	@rm -f $(COVERAGE_FILE)
	@echo "Clean complete"

## test: Run every test
test:
	$(GOTEST) ./...

## test-short: Run the tests without the long ones (process kills, load, repeated measurements)
test-short:
	$(GOTEST) -short ./...

## test-chain: Run one chain's tests and build its binary (CHAIN=<chain>)
test-chain: check-chain
	@test -n "$(CHAIN)" || (echo "Set CHAIN to one of: $(CHAINS)"; exit 1)
	$(GOTEST) ./x/$(CHAIN)/...
	$(GOBUILD) -o /dev/null $(MAIN_PATH)

## test-race: Run every test once with the race detector; run it before each commit and pull request (CI has no race job, to keep CI fast)
test-race:
	$(GOTEST) -race -count=1 ./...
	cd execwire && $(GOTEST) -race -count=1 ./...
	cd x/stablenet/executor && $(GOTEST) -race -count=1 ./...

.PHONY: test-execwire
## test-execwire: Build and check the standalone standard-library execution wire module
test-execwire:
	cd execwire && $(GOBUILD) ./... && $(GOVET) ./... && $(GOTEST) -race -count=1 ./...

.PHONY: test-executor-stablenet
.PHONY: test-executor-arc
## test-executor-arc: Build/lint/test official Arc replay libraries without starting a node
test-executor-arc:
	cd x/arc/executor && cargo fmt --check && cargo build --locked --all-targets && cargo clippy --locked --all-targets -- -D warnings && cargo test --locked --all-targets

## test-executor-stablenet: Check two-pass StableNet replay with offline EVM and WBFT fixtures
test-executor-stablenet:
	cd x/stablenet/executor && $(GOBUILD) ./... && $(GOVET) ./... && $(GOTEST) -race -count=1 ./...

## test-cache-lifecycle: Check Pebble cache ownership with its leak finalizers (race disables these finalizers)
test-cache-lifecycle:
	$(GOTEST) -tags=invariants -count=1 -run '^TestPebbleCacheLifecycle$$' ./pkg/storage

## test-all: Run all tests including integration tests
test-all:
	@echo "Running all tests..."
	$(GOTEST) -v ./...

## test-integration: Run integration tests only
test-integration:
	@echo "Running integration tests..."
	$(GOTEST) -v -tags=integration ./...

## test-postgres: Run the PostgreSQL adapter tests and the end-to-end suite on PostgreSQL (disposable container, or INDEXER_TEST_POSTGRES)
test-postgres:
	@./scripts/test-postgres.sh

## test-examples: Build and test the example modules (examples/*, separate go.mod each)
test-examples:
	@for d in examples/*/; do echo "== $$d"; (cd $$d && $(GOTEST) ./...) || exit 1; done

## test-slo: Check the G3 push latency service level and the real-time notification latency (about four minutes; refactoring plan R5-5, subscriptions design phase 6)
test-slo:
	INDEXER_LOAD_SLO=1 $(GOTEST) -count=1 -v -timeout 10m -run 'TestSLOLoad|TestNotificationLatency' ./pkg/app

## coverage: Generate test coverage report
coverage:
	@echo "Generating coverage report..."
	$(GOTEST) -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
	$(GOCMD) tool cover -html=$(COVERAGE_FILE) -o coverage.html
	@echo "Coverage report generated: coverage.html"

## lint: Run linter
lint: lint-install
	@echo "Running linter..."
	$(GOLANGCI_LINT) run ./...

## lint-install: Install golangci-lint $(GOLANGCI_LINT_VERSION) built with the active Go toolchain
lint-install:
	@$(GOLANGCI_LINT) version 2>/dev/null | grep -q "version $(GOLANGCI_LINT_VERSION:v%=%) built with $$($(GOCMD) env GOVERSION) " || \
		(echo "Installing golangci-lint $(GOLANGCI_LINT_VERSION) with $$($(GOCMD) env GOVERSION)..." && \
		GOBIN=$(dir $(GOLANGCI_LINT)) $(GOCMD) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION))

## fmt: Format code
fmt:
	@echo "Formatting code..."
	$(GOFMT) ./...

## vet: Run go vet
vet:
	@echo "Running go vet..."
	$(GOVET) ./...

## generate: Generate GraphQL code
generate:
	@echo "Generating GraphQL code..."
	@which gqlgen > /dev/null || (echo "gqlgen not found. Install with: make tools" && exit 1)
	$(GOCMD) run github.com/99designs/gqlgen generate
	@echo "Code generation complete"

## mod-download: Download dependencies
mod-download:
	@echo "Downloading dependencies..."
	$(GOMOD) download

## mod-tidy: Tidy go.mod
mod-tidy:
	@echo "Tidying go.mod..."
	$(GOMOD) tidy

## mod-verify: Verify dependencies
mod-verify:
	@echo "Verifying dependencies..."
	$(GOMOD) verify

## tools: Install development tools
tools:
	@echo "Installing development tools..."
	@which gqlgen > /dev/null || $(GOGET) github.com/99designs/gqlgen
	@$(MAKE) lint-install
	@echo "Tools installed"

## run: Run the indexer (dev)
run: build-dev
	@echo "Starting indexer..."
	$(BUILD_DIR)/$(BINARY_NAME) \
		--rpc http://localhost:8545 \
		--db ./dev-data \
		--log-level debug \
		--workers 10 \
		--chunk-size 100

## docker-build: Build Docker image (CHAIN as for build)
docker-build: check-chain
	@echo "Building Docker image..."
	docker build --build-arg CHAIN=$(CHAIN) -t $(BINARY_NAME):latest .
	@echo "Docker image built: $(BINARY_NAME):latest"

## docker-run: Run Docker container
docker-run:
	@echo "Running Docker container..."
	docker run -d \
		--name $(BINARY_NAME) \
		-p 8080:8080 \
		-v $$(pwd)/data:/data \
		-e INDEXER_REMOTE=http://host.docker.internal:8545 \
		$(BINARY_NAME):latest

## docker-stop: Stop Docker container
docker-stop:
	@echo "Stopping Docker container..."
	docker stop $(BINARY_NAME) || true
	docker rm $(BINARY_NAME) || true

## bench: Run benchmarks
bench:
	@echo "Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' Makefile | sed 's/## /  /'

.DEFAULT_GOAL := help

.PHONY: test-node-feed check-node-feed-arc
## test-node-feed: Check the Rust producer and Go IPC integration (requires Rust)
test-node-feed:
	cargo fmt --check --manifest-path x/arc/nodefeed/Cargo.toml
	cargo test --locked --manifest-path x/arc/nodefeed/Cargo.toml
	cargo check --locked --features reth --manifest-path x/arc/nodefeed/Cargo.toml
	cargo build --locked --manifest-path x/arc/nodefeed/Cargo.toml --bin fixture
	INDEXER_NODE_FEED_FIXTURE_BINARY="$(CURDIR)/x/arc/nodefeed/target/debug/fixture" $(GOCMD) test -race -count=1 ./internal/source/nodefeed ./pkg/app -run '^TestNodeFeed'

## check-node-feed-arc: Type-check the optional hook in the audited Arc checkout
check-node-feed-arc:
	@test -n "$(ARC_NODE_CHECKOUT)" || (echo 'Set ARC_NODE_CHECKOUT to the audited checkout'; exit 1)
	python3 x/arc/nodefeed/install_arc.py --arc-checkout "$(ARC_NODE_CHECKOUT)"
	cargo check --manifest-path "$(ARC_NODE_CHECKOUT)/Cargo.toml" -p arc-node-execution --bin arc-node-execution
