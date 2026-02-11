# Project configuration
BIN_NAME := bracket-creator
GH_REPOSITORY ?= gitrgoliveira/bracket-creator
IMAGE_NAME := ghcr.io/$(GH_REPOSITORY)
BIN_PATH := ./bin
GO_VERSION := 1.25.4

# Build flags
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X github.com/gitrgoliveira/bracket-creator/internal/cmd/version.Version=$(VERSION) \
           -X github.com/gitrgoliveira/bracket-creator/internal/cmd/version.Commit=$(COMMIT) \
           -X github.com/gitrgoliveira/bracket-creator/internal/cmd/version.BuildDate=$(BUILD_TIME)"

# OS detection
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
	OPEN_CMD := open
	SED_CMD := sed -i ''
else
	OPEN_CMD := xdg-open
	SED_CMD := sed -i
endif

# Define phony targets
.PHONY: default help clean local/deps go/test go/build go/lint examples docker/build docker/run pre-commit docs/serve docs/open run goreleaser/test release version

default: help ## Show help information (default)

clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	rm -rf $(BIN_PATH)
	rm -rf dist/
	@echo "Done!"

local/deps: ## Install project dependencies
	@echo "Installing dependencies..."
	go mod tidy
	go install github.com/spf13/cobra-cli@v1.3.0
	go install github.com/goreleaser/goreleaser@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8
	python3 -m pip install -r docs/requirements.txt

go/lint: ## Run linters
	@echo "Running linters..."
	golangci-lint run ./...

go/test: go/lint ## Run tests
	@echo "Running tests..."
	go test -race -cover ./...
	
go/build: $(BIN_PATH)/$(BIN_NAME) ## Build the application locally

$(BIN_PATH)/$(BIN_NAME): $(shell find . -name "*.go" -type f)
	@echo "Building $(BIN_NAME) version $(VERSION)..."
	@mkdir -p $(BIN_PATH)
	go generate ./...
	go build $(LDFLAGS) -o $(BIN_PATH)/$(BIN_NAME) .

examples: go/build ## Build locally and create example files
	@echo "Cleaning previous examples..."
	rm -f pools-example-*.xlsx playoffs-example-*.xlsx
	@echo "Building examples..."
	$(BIN_PATH)/$(BIN_NAME) create-pools -d -r -t 5 -f ./mock_data_small.csv -o ./pools-example-small.xlsx
	$(BIN_PATH)/$(BIN_NAME) create-playoffs -d -t 5 -f ./mock_data_small.csv -o ./playoffs-example-small.xlsx
	$(BIN_PATH)/$(BIN_NAME) create-pools -d -s -r -p 3 -w 2 -f ./mock_data_medium.csv -o ./pools-example-medium.xlsx
	$(BIN_PATH)/$(BIN_NAME) create-playoffs -d -s -f ./mock_data_medium.csv -o ./playoffs-example-medium.xlsx
	$(BIN_PATH)/$(BIN_NAME) create-pools -d -s -p 3 -w 2 -t 5 -f ./mock_data_large.csv -o ./pools-example-large.xlsx
	$(BIN_PATH)/$(BIN_NAME) create-playoffs -d -s -f ./mock_data_large.csv -o ./playoffs-example-large.xlsx
	@echo "Examples successfully created!"

docker/build: ## Build Docker image
	@echo "Building Docker image $(IMAGE_NAME)..."
	docker build \
		--build-arg GO_VERSION=$(GO_VERSION) \
		--build-arg VERSION=$(VERSION) \
		--build-arg GIT_COMMIT=$(COMMIT) \
		-t $(IMAGE_NAME):latest \
		-t $(IMAGE_NAME):$(VERSION) \
		.

docker/run: docker/build ## Run the application in Docker
	docker run -p 8080:8080 $(IMAGE_NAME):latest

pre-commit: go/test ## Run pre-commit checks
	go fmt ./...
	@echo "Code is ready to commit!"

docs/serve: ## Locally serve the documentation
	@echo "Starting documentation server..."
	mkdocs serve

docs/open: docs/serve & ## Open documentation in browser
	@echo "Opening documentation in browser..."
	$(OPEN_CMD) http://localhost:8000

run: go/build ## Run the application locally
	@echo "Running $(BIN_NAME)..."
	$(BIN_PATH)/$(BIN_NAME) serve

goreleaser/test: ## Test the goreleaser configuration locally
	@echo "Testing goreleaser configuration..."
	goreleaser --snapshot --skip=publish --clean

release: ## Build for release (using goreleaser)
	@if [ -z "$(VERSION)" ]; then \
		echo "Error: VERSION is not set. Use 'make release VERSION=x.y.z'"; \
		exit 1; \
	fi
	@echo "Creating release $(VERSION)..."
	git tag -a $(VERSION) -m "Release $(VERSION)"
	git push origin $(VERSION)
	goreleaser release --clean

version: ## Show version information
	@echo "$(BIN_NAME) version $(VERSION)"
	@echo "Commit: $(COMMIT)"
	@echo "Build time: $(BUILD_TIME)"
	@echo "Go version: $(shell go version)"

## Print this help screen
help:
	@printf "\n\033[1m$(BIN_NAME) $(VERSION) - Makefile targets\033[0m\n\n"
	@awk 'BEGIN {FS = ":.*?## "; printf ""} \
		/^[a-zA-Z0-9_\-\/]+:.*?## / {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2} \
		' $(MAKEFILE_LIST) | sort
	@printf "\n"