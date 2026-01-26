# Variables
BINARY_NAME=loki-ingest
BIN_DIR=bin
CMD_DIR=./cmd/server
DOCKER_IMAGE=loki-ingest
DOCKER_TAG=latest

.PHONY: help build run test clean docker

help: ## Display this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-20s %s\n", $$1, $$2}'

build: ## Build the application binary
	@echo "Building application..."
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build \
		-trimpath \
		-ldflags="-s -w" \
		-o $(BIN_DIR)/$(BINARY_NAME) \
		$(CMD_DIR)

run: ## Run the application locally
	@echo "Running application..."
	go run $(CMD_DIR)/main.go

test: ## Run tests
	@echo "Running tests..."
	go test -v ./...

clean: ## Clean build artifacts
	@echo "Cleaning..."
	rm -rf $(BIN_DIR)/
	go clean

deps: ## Download dependencies
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy

docker: ## Build Docker image
	@echo "Building Docker image..."
	docker build -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

lint: ## Run linters
	@echo "Running linters..."
	golangci-lint run

fmt: ## Format code
	@echo "Formatting code..."
	go fmt ./...

all: clean deps build ## Clean, download deps, and build
