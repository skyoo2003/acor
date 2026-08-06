.PHONY: all setup clean build test lint lint-fix coverage vet fuzz bench bench-module race docs-verify license-check proto tidy-check

# Pin golangci-lint so local `make lint` matches CI (see .github/workflows/ci.yaml).
# Run via `go run` so the installed binary's version can't drift from CI.
GOLANGCI_LINT_VERSION ?= v2.11.3
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

all: vet lint test build docs-verify license-check

setup:
	@pre-commit install

clean:
	@rm -rf dist/
	@rm -f coverage.out coverage.html

build:
	@go build -o dist/acor ./cmd/acor

test:
	@go test ./...
	@cd server && go test ./...

# Verify go.mod/go.sum are tidy in all three modules. `go mod tidy` rewrites in
# place, so the diff is what fails the check. benchmarks/ is included because an
# un-tidied go.sum there is how the pinned competitor versions silently drift
# from the ones the comparison page claims to have measured.
tidy-check:
	@go mod tidy
	@cd server && go mod tidy
	@cd benchmarks && go mod tidy
	@git diff --exit-code go.mod go.sum server/go.mod server/go.sum benchmarks/go.mod benchmarks/go.sum

docs-verify:
	@go run ./tools/doccheck README.md $$(find docs/content -name '*.md')

# Gate on the third-party attribution in NOTICE. Same shape as tidy-check: the
# file is rewritten in place, and the diff against the committed one is what fails
# the check. Scoped to ./cmd/acor because that binary is the only thing this
# project redistributes, and redistribution is what triggers the obligation — so a
# dependency change that alters what the binary links has to change NOTICE too.
# The generator fails on a dependency whose license it cannot identify rather than
# guessing, which is why adding a dependency can break this target.
#
# Generated via a temp file rather than redirecting straight into NOTICE: the
# shell truncates the target before the generator runs, so an unidentified
# dependency would otherwise leave the attribution file empty in the working
# tree, one `commit -a --no-verify` away from shipping.
license-check:
	@go run ./tools/licensesnap > NOTICE.tmp && mv NOTICE.tmp NOTICE || { rm -f NOTICE.tmp; exit 1; }
	@git diff --exit-code NOTICE

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
	@cd benchmarks && go vet ./...

lint-fix:
	@$(GOLANGCI_LINT) run --fix ./...
	@cd server && $(GOLANGCI_LINT) run --fix ./...

fuzz:
	@go test -fuzz=FuzzFind -fuzztime=30s ./pkg/acor
	@go test -fuzz=FuzzAdd -fuzztime=30s ./pkg/acor

# Timing evidence for docs/content/reference/benchmarks.md. Set
# ACOR_INTEGRATION_ADDR to also run the real-server benchmarks; without it only
# the miniredis ones run, and those must never be published as timings (an
# in-process emulator has no round-trip cost, which is where V1 loses).
bench:
	@go test -bench . -benchmem -run '^$$' ./pkg/acor ./internal/engine

# Timing, memory, and propagation evidence measured through the public API. Lives
# in its own module because it needs a server and a dictionary loaded the way a
# caller would load it. Requires ACOR_INTEGRATION_ADDR — the cold-start, bulk-load,
# and propagation figures are meaningless against miniredis, which makes the Redis
# path nearly free.
bench-module:
	@cd benchmarks && go test -bench . -benchmem -benchtime=200x -run '^$$' ./...
	@cd benchmarks && go test -run 'MemoryFootprint|Propagation' -v ./...

race:
	@go test -race ./...
	@cd server && go test -race ./...
