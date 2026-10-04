BINARY  := outlook
PKG     := ./cmd/outlook
MODULE  := github.com/stainedhead/outlook-cli

# Version stamping (REL-4). VERSION defaults to the nearest tag, else "dev".
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

export GOPRIVATE ?= github.com/stainedhead/*

.PHONY: all build test race cover lint fmt vet tidy skill cross clean check

all: check build

# Everything CI checks, in one go.
check: fmt-check vet lint race

build:
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o bin/$(BINARY) $(PKG)

test:
	go test ./...

race:
	go test -race ./...

cover:
	go test -race -coverprofile=coverage.txt ./...
	go tool cover -func=coverage.txt | tail -n 1
	go tool cover -html=coverage.txt -o coverage.html

vet:
	go vet ./...

lint:
	golangci-lint run

fmt:
	gofmt -w .

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "$$out"; exit 1; fi

tidy:
	go mod tidy

# Generates the agent skill document from the command tree via core docgen.
# The CLI exposes a hidden `skill` command (WS3) that prints the Markdown.
skill:
	mkdir -p dist
	go run -ldflags '$(LDFLAGS)' $(PKG) skill > dist/outlook-cli.md

# Cross-compiles every release target (BLD-3): static linux, darwin/arm64.
cross:
	mkdir -p dist
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/$(BINARY)-darwin-arm64 $(PKG)
	GOOS=linux  GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/$(BINARY)-linux-amd64 $(PKG)
	GOOS=linux  GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/$(BINARY)-linux-arm64 $(PKG)

clean:
	rm -rf bin dist coverage.txt coverage.html
