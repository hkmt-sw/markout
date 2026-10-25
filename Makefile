BINARY  := markout
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
GOBIN   := $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

# Versions of the tools that `make lint` and `make vuln` run with, the same
# as in .github/workflows/ci.yml (and release.yml): change them together.
GOLANGCI_LINT_VERSION := v2.14.0
GOVULNCHECK_VERSION   := v1.9.0

# How long `make fuzz` runs each fuzz target.
FUZZTIME := 30s

# Platforms for `make release` (os/arch pairs).
PLATFORMS := darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: build install uninstall run test lint vuln fuzz clean release

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

## test: run the test suite as CI does (race detector, shuffled order)
test:
	go test -race -shuffle=on ./...

## lint: run golangci-lint with the settings in .golangci.yml
lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION) run ./...

## vuln: look for known vulnerabilities in the code and in the Go that builds it
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

## fuzz: run every fuzz target for $(FUZZTIME) (a target on its own: go test -fuzz)
fuzz:
	go test ./internal/mathml  -run='^$$' -fuzz='^FuzzMath$$'    -fuzztime=$(FUZZTIME)
	go test ./internal/mermaid -run='^$$' -fuzz='^FuzzMermaid$$' -fuzztime=$(FUZZTIME)
	go test ./internal/parse   -run='^$$' -fuzz='^FuzzParse$$'   -fuzztime=$(FUZZTIME)
	go test ./internal/render  -run='^$$' -fuzz='^FuzzSVGSize$$' -fuzztime=$(FUZZTIME)
	go test ./internal/theme   -run='^$$' -fuzz='^FuzzTheme$$'   -fuzztime=$(FUZZTIME)

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
