.PHONY: all setup clean build test lint lint-fix coverage vet fuzz race docs-verify

all: vet lint test build docs-verify

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

docs-verify:
	@go run ./tools/doccheck README.md $$(find docs/content -name '*.md')

lint:
	@golangci-lint run ./...
	@cd server && golangci-lint run ./...

coverage:
	@go test ./... -coverprofile=coverage.out -covermode=atomic
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

vet:
	@go vet ./...
	@cd server && go vet ./...

lint-fix:
	@golangci-lint run --fix ./...
	@cd server && golangci-lint run --fix ./...

fuzz:
	@go test -fuzz=FuzzFind -fuzztime=30s ./pkg/acor
	@go test -fuzz=FuzzAdd -fuzztime=30s ./pkg/acor

race:
	@go test -race ./...
	@cd server && go test -race ./...
