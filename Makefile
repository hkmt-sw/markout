BINARY  := markout
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOBIN   := $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

# Platforms for `make release` (os/arch pairs).
PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: build install uninstall run test clean release

## build: compile the binary into ./$(BINARY)
build:
	go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

## install: build and install into $(GOBIN) so `markout` runs from anywhere
install:
	go install -ldflags "$(LDFLAGS)" .
	@echo "Installed $(BINARY) → $(GOBIN)/$(BINARY)"
	@case ":$$PATH:" in *":$(GOBIN):"*) ;; \
	  *) echo "NOTE: $(GOBIN) is not on your PATH. Add it to your shell profile:"; \
	     echo '      export PATH="$(GOBIN):$$PATH"' ;; esac

## uninstall: remove the installed binary
uninstall:
	rm -f "$(GOBIN)/$(BINARY)"
	@echo "Removed $(GOBIN)/$(BINARY)"

## run: launch the TUI without installing
run:
	go run .

## test: run the test suite
test:
	go test ./...

## clean: remove the locally built binary and dist/
clean:
	rm -f $(BINARY)
	rm -rf dist

## release: cross-compile static binaries for all PLATFORMS into dist/
release:
	@mkdir -p dist
	@for p in $(PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; \
	  out=dist/$(BINARY)_$(VERSION)_$${os}_$${arch}; \
	  [ "$$os" = "windows" ] && out=$$out.exe; \
	  echo "building $$out"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
	    go build -ldflags "$(LDFLAGS)" -o $$out . || exit 1; \
	done
	@cd dist && { command -v sha256sum >/dev/null 2>&1 && sha256sum $(BINARY)_* || shasum -a 256 $(BINARY)_*; } > SHA256SUMS.txt
	@echo "done → dist/ (with SHA256SUMS.txt)"
