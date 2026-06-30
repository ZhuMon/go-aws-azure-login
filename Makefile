MODULE  := github.com/ZhuMon/go-aws-azure-login
BIN     := go-aws-azure-login
RELEASE := release

# Version metadata injected into the cmd package. VERSION defaults to the
# current git tag/describe; override with `make release VERSION=v0.5.0`.
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w \
	-X $(MODULE)/cmd.Version=$(VERSION) \
	-X $(MODULE)/cmd.GitCommit=$(GIT_COMMIT) \
	-X $(MODULE)/cmd.BuildDate=$(BUILD_DATE)

# Target platforms for cross-compiled release binaries.
PLATFORMS := darwin/arm64 darwin/amd64 linux/arm64 linux/amd64 windows/amd64

.PHONY: build test build-all release clean

# Build for the current platform into ./$(BIN).
build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BIN) .

test:
	go test ./...

# Cross-compile every platform into $(RELEASE)/ and write checksums.txt.
# The project is pure Go (CGO_ENABLED=0), so this needs no C toolchain.
build-all: clean
	@mkdir -p $(RELEASE)
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		ext=; [ "$$os" = windows ] && ext=.exe; \
		out=$(RELEASE)/$(BIN)_$(VERSION)_$${os}_$${arch}$$ext; \
		echo "building $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -ldflags "$(LDFLAGS)" -o $$out . || exit 1; \
	done
	@cd $(RELEASE) && shasum -a 256 $(BIN)_$(VERSION)_* > checksums.txt
	@echo "artifacts in $(RELEASE)/"

# Cut a GitHub release as a draft: tests, cross-compiles, then uploads via gh.
# Auto-generated notes are a starting point — edit them and publish manually.
# Requires VERSION to match a pushed tag and gh authenticated to the repo owner.
release: test build-all
	gh release create $(VERSION) \
		--title "$(VERSION)" \
		--generate-notes \
		--draft \
		$(RELEASE)/$(BIN)_$(VERSION)_* \
		$(RELEASE)/checksums.txt

clean:
	rm -rf $(RELEASE)
