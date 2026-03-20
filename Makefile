.PHONY: all setup clean build test lint coverage

all: lint test build

setup:
	@pre-commit install

clean:
	@rm -rf dist/
	@rm -f coverage.out coverage.html

build:
	@go build -o dist/acor ./cmd/acor

test:
	@go test -v ./...

lint:
	@golangci-lint run ./...

coverage:
	@go test ./... -coverprofile=coverage.out -covermode=atomic
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"