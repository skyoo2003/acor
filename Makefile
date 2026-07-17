.PHONY: all setup clean build test lint lint-fix coverage vet fuzz race

# Keep in sync with .github/workflows/ci.yaml
GOLANGCI_LINT_VERSION ?= v2.11.3
GOLANGCI_LINT := go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

all: vet lint test build

setup:
	@pre-commit install

clean:
	@rm -rf dist/
	@rm -f coverage.out coverage.html

build:
	@go build -o dist/acor ./cmd/acor

test:
	@go test -v ./...
	@cd server && go test -v ./...

lint:
	@$(GOLANGCI_LINT) run ./...
	@cd server && $(GOLANGCI_LINT) run ./...

coverage:
	@go test ./... -coverprofile=coverage.out -covermode=atomic
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

vet:
	@go vet ./...
	@cd server && go vet ./...

lint-fix:
	@$(GOLANGCI_LINT) run --fix ./...
	@cd server && $(GOLANGCI_LINT) run --fix ./...

fuzz:
	@go test -fuzz=FuzzFind -fuzztime=30s ./pkg/acor
	@go test -fuzz=FuzzAdd -fuzztime=30s ./pkg/acor

race:
	@go test -race ./...
	@cd server && go test -race ./...
