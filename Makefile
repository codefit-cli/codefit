# codefit — build automation.
# CGO is disabled everywhere: the single-binary, clean cross-compile guarantee
# is non-negotiable (see CLAUDE.md).

BINARY      := codefit
PKG         := ./cmd/codefit
BIN_DIR     := bin
DIST_DIR    := dist
VERSION     ?= 0.1.0-dev
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VPKG        := github.com/codefit-cli/codefit/internal/version
LDFLAGS     := -s -w \
	-X $(VPKG).Version=$(VERSION) \
	-X $(VPKG).Commit=$(COMMIT) \
	-X $(VPKG).BuildDate=$(BUILD_DATE)

export CGO_ENABLED := 0

.PHONY: build test lint cross-compile clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(PKG)

test:
	go test ./...

lint:
	golangci-lint run

cross-compile:
	GOOS=linux   GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY)-linux-amd64   $(PKG)
	GOOS=linux   GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY)-linux-arm64   $(PKG)
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY)-windows-amd64.exe $(PKG)
	GOOS=darwin  GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST_DIR)/$(BINARY)-darwin-arm64  $(PKG)

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)
