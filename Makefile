# wordcounter Makefile
BINARY     := wordcounter
BUILD_DIR  := ./bin
CMD        := ./cmd/wordcounter
MODULE     := github.com/example/wordcounter
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GOFLAGS    := -ldflags="-s -w -X main.version=$(VERSION)"

.PHONY: all build test test-race bench lint clean run help

all: test build

## build: compile the binary to ./bin/wordcounter
build:
	@mkdir -p $(BUILD_DIR)
	go build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY) $(CMD)
	@echo "✓ built $(BUILD_DIR)/$(BINARY)"

## run: build and run with sample args (counts its own source files)
run: build
	$(BUILD_DIR)/$(BINARY) -r -include="*.go" -sort=words -desc .

## test: run all tests
test:
	go test ./... -count=1 -timeout=60s

## test-race: run tests with race detector (mandatory in CI)
test-race:
	go test -race ./... -count=1 -timeout=60s

## bench: run all benchmarks with memory stats
bench:
	go test -bench=. -benchmem -benchtime=3s -count=3 ./internal/counter/

## cover: generate HTML coverage report
cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "✓ coverage.html generated"

## lint: run go vet
lint:
	go vet ./...
	@echo "✓ vet passed"

## clean: remove build artifacts
clean:
	rm -rf $(BUILD_DIR) coverage.out coverage.html

## help: show this help
help:
	@grep -E '^## ' Makefile | sed 's/## /  /'
