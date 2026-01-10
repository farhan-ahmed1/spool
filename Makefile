.PHONY: help setup dev test lint clean build run-broker run-worker run-dashboard docker-up docker-down benchmark

# Default target
help:
	@echo "Spool - Distributed Task Queue"
	@echo ""
	@echo "Available commands:"
	@echo "  make setup       - Install dependencies and tools"
	@echo "  make dev         - Run all services in dev mode"
	@echo "  make test        - Run all tests"
	@echo "  make lint        - Run linters"
	@echo "  make build       - Build all binaries"
	@echo "  make run-broker  - Run broker service"
	@echo "  make run-worker  - Run worker service"
	@echo "  make dashboard   - Run dashboard"
	@echo "  make docker-up   - Start Docker services (Redis)"
	@echo "  make docker-down - Stop Docker services"
	@echo "  make benchmark   - Run performance benchmarks"
	@echo "  make clean       - Clean build artifacts"

# Install dependencies
setup:
	@echo "Installing dependencies..."
	go mod download
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/redis/go-redis/v9@latest
	
	@echo "Setup complete!"

# Build all binaries
build:
	@echo "Building binaries..."
	@mkdir -p bin
	go build -o bin/broker ./cmd/broker
	go build -o bin/worker ./cmd/worker
	@echo "Build complete! Binaries in ./bin/"

# Run tests
test:
	@echo "Running tests..."
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Tests complete! Coverage report: coverage.html"

# Run integration tests
test-integration:
	@echo "Running integration tests..."
	go test -v -tags=integration ./tests/integration/...

# Run linters
lint:
	@echo "Running linters..."
	@$$(go env GOPATH)/bin/golangci-lint run ./...
	@echo "Linting complete!"

# Run broker
run-broker:
	@echo "Starting broker..."
	go run ./cmd/broker/main.go

# Run worker
run-worker:
	@echo "Starting worker..."
	go run ./cmd/worker/main.go

# Run dashboard
run-dashboard:
	@echo "Starting dashboard..."
	go run ./web/server.go

# Run all services (in separate terminals you'll need)
dev:
	@echo "For development, run these in separate terminals:"
	@echo "  Terminal 1: make docker-up"
	@echo "  Terminal 2: make run-broker"
	@echo "  Terminal 3: make run-worker"
	@echo "  Terminal 4: make run-dashboard"

# Start Docker services
docker-up:
	@echo "Starting Docker services..."
	docker-compose up -d
	@echo "Redis running on localhost:6379"

# Stop Docker services
docker-down:
	@echo "Stopping Docker services..."
	docker-compose down
	@echo "Services stopped"

# Run benchmarks
benchmark:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./tests/load/

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf bin/
	rm -f coverage.out coverage.html
	go clean
	@echo "Clean complete!"

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...
	@echo "Format complete!"

# Generate mocks (for testing)
generate:
	@echo "Generating code..."
	go generate ./...
	@echo "Generation complete!"
