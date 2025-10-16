.PHONY: all build clean test lint coverage generate tools help

# Variables
BINARY_NAME=indexer-go
MAIN_PATH=./cmd
BUILD_DIR=./build
COVERAGE_FILE=coverage.out

# Go commands
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod
GOFMT=$(GOCMD) fmt
GOVET=$(GOCMD) vet

# Build flags
LDFLAGS=-ldflags "-s -w"
TAGS=-tags release

all: clean build test

## build: Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) $(TAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_PATH)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

## build-dev: Build for development (with debug info)
build-dev:
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

## test: Run tests
test:
	@echo "Running tests..."
	$(GOTEST) -v -short ./...

## test-all: Run all tests including integration tests
test-all:
	@echo "Running all tests..."
	$(GOTEST) -v ./...

## test-integration: Run integration tests only
test-integration:
	@echo "Running integration tests..."
	$(GOTEST) -v -tags=integration ./...

## coverage: Generate test coverage report
coverage:
	@echo "Generating coverage report..."
	$(GOTEST) -coverprofile=$(COVERAGE_FILE) -covermode=atomic ./...
	$(GOCMD) tool cover -html=$(COVERAGE_FILE) -o coverage.html
	@echo "Coverage report generated: coverage.html"

## lint: Run linter
lint:
	@echo "Running linter..."
	@which golangci-lint > /dev/null || (echo "golangci-lint not found. Install with: make tools" && exit 1)
	golangci-lint run ./...

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
	@which golangci-lint > /dev/null || curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin
	@echo "Tools installed"

## run: Run the indexer (dev)
run: build-dev
	@echo "Starting indexer..."
	$(BUILD_DIR)/$(BINARY_NAME) start \
		--remote http://localhost:8545 \
		--db-path ./dev-data \
		--log-level debug

## docker-build: Build Docker image
docker-build:
	@echo "Building Docker image..."
	docker build -t $(BINARY_NAME):latest .
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
