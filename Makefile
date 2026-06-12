BIN     := bin/ports
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: build run test lint fmt vet snapshot clean install

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/ports

run: build
	./$(BIN)

test:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

lint:
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || { echo "golangci-lint not installed — falling back to go vet"; go vet ./...; }

snapshot:
	goreleaser release --snapshot --clean

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/ports

clean:
	rm -rf bin dist
