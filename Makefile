.PHONY: all setup clean build test lint lint-fix coverage vet fuzz race docs-verify proto

# Pin golangci-lint so local `make lint` matches CI (see .github/workflows/ci.yaml).
# Run via `go run` so the installed binary's version can't drift from CI.
GOLANGCI_LINT_VERSION ?= v2.11.3
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

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

# Regenerate gRPC/protobuf code. Requires protoc, protoc-gen-go, protoc-gen-go-grpc.
proto:
	@protoc -I server/proto \
		--go_out=server --go_opt=module=github.com/skyoo2003/acor/server \
		--go-grpc_out=server --go-grpc_opt=module=github.com/skyoo2003/acor/server \
		server/proto/acor/v1/acor.proto

lint:
	@$(GOLANGCI_LINT) run ./...
	@cd server && $(GOLANGCI_LINT) run ./...

coverage:
	@go test ./... -coverpkg=./... -coverprofile=coverage.out -covermode=atomic
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
