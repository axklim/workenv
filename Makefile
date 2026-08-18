# Every target runs the Go toolchain inside a container, so the project
# builds and tests with nothing but Docker installed. Override GO_IMAGE to
# pick a different Go version.

GO_IMAGE    ?= golang:1.23.5
DOCKER      ?= docker
BIN         ?= bin/we
INSTALL_DIR ?= $(HOME)/.local/bin

# The container is Linux, but the binary has to run on *this* machine, so
# builds always cross-compile to the host's OS/arch. Override GOOS/GOARCH to
# build for somewhere else (e.g. `make build GOOS=linux GOARCH=amd64`).
GOOS   ?= $(shell uname -s | tr '[:upper:]' '[:lower:]')
GOARCH ?= $(patsubst aarch64,arm64,$(patsubst x86_64,amd64,$(shell uname -m)))

# Kept outside the repo: `./...` skips dot-directories, but gofmt does not,
# and a module cache full of third-party sources is not project source.
CACHE_DIR ?= $(HOME)/.cache/workenv-go

# The version stamped into the binary. `git describe` gives a local build
# something truthful (0.1.0-3-gabc1234-dirty); releases pass VERSION
# explicitly rather than trusting the working tree. -s -w drop the symbol
# table and DWARF data, which a shipped CLI has no use for.
GIT_VERSION := $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
VERSION     ?= $(if $(GIT_VERSION),$(GIT_VERSION),dev)
LDFLAGS      = -s -w -X main.version=$(VERSION)

# Release targets. darwin/amd64 is deliberately absent: it cannot be tested
# here, and a support claim with nothing behind it is worse than none.
DIST_DIR       ?= dist
DIST_PLATFORMS  = darwin/arm64 linux/amd64 linux/arm64

# The container runs as the invoking user so build artifacts and caches come
# out owned by that user rather than root. GOFLAGS disables VCS stamping,
# which git refuses to do on a repo it sees as foreign-owned.
DOCKER_RUN = $(DOCKER) run --rm \
	-v "$(CURDIR)":/src -w /src \
	-v "$(CACHE_DIR)":/cache \
	-u $(shell id -u):$(shell id -g) \
	-e HOME=/cache -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod \
	-e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false

GO      = $(DOCKER_RUN) $(GO_IMAGE) go
SRC_DIRS = cmd internal

.DEFAULT_GOAL := help
.PHONY: help build test vet fmt fmt-check check install dist formula shell clean clean-cache

help: ## Show this help
	@grep -hE '^[a-z-]+:.*##' $(MAKEFILE_LIST) \
		| sed -e 's/:.*##[[:space:]]*/\t/' \
		| awk -F'\t' '{ printf "  \033[1m%-12s\033[0m %s\n", $$1, $$2 }'

build: | $(CACHE_DIR) ## Build ./cmd/we into bin/we for this host
	@mkdir -p "$(dir $(BIN))"
	$(DOCKER_RUN) -e GOOS=$(GOOS) -e GOARCH=$(GOARCH) $(GO_IMAGE) \
		go build -trimpath -ldflags '$(LDFLAGS)' -o "$(BIN)" ./cmd/we
	@echo "built $(BIN) ($(GOOS)/$(GOARCH), version $(VERSION))"

test: | $(CACHE_DIR) ## Run the test suite
	$(GO) test ./...

vet: | $(CACHE_DIR) ## Run go vet
	$(GO) vet ./...

fmt: | $(CACHE_DIR) ## Rewrite sources with gofmt
	$(DOCKER_RUN) $(GO_IMAGE) gofmt -w $(SRC_DIRS)

fmt-check: | $(CACHE_DIR) ## Fail if any source needs gofmt
	@unformatted=$$($(DOCKER_RUN) $(GO_IMAGE) gofmt -l $(SRC_DIRS)); \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needed:"; echo "$$unformatted"; exit 1; \
	fi

check: fmt-check vet test ## Run gofmt check, go vet and the tests

install: build ## Install the binary into ~/.local/bin (override INSTALL_DIR)
	@mkdir -p "$(INSTALL_DIR)"
	install -m 0755 "$(BIN)" "$(INSTALL_DIR)/we"
	@echo "installed $(INSTALL_DIR)/we"

dist: | $(CACHE_DIR) ## Cross-compile and package release tarballs into dist/
	@rm -rf "$(DIST_DIR)"
	@mkdir -p "$(DIST_DIR)"
	@for p in $(DIST_PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		stage="$(DIST_DIR)/$$os-$$arch"; \
		mkdir -p "$$stage" || exit 1; \
		$(DOCKER_RUN) -e GOOS=$$os -e GOARCH=$$arch $(GO_IMAGE) \
			go build -trimpath -ldflags '$(LDFLAGS)' -o "$$stage/we" ./cmd/we || exit 1; \
		cp LICENSE README.md "$$stage/" || exit 1; \
		tar -czf "$(DIST_DIR)/workenv-$(VERSION)-$$os-$$arch.tar.gz" \
			-C "$$stage" we LICENSE README.md || exit 1; \
		rm -rf "$$stage"; \
		echo "packaged workenv-$(VERSION)-$$os-$$arch.tar.gz"; \
	done
	@cd "$(DIST_DIR)" && shasum -a 256 *.tar.gz > SHA256SUMS
	@cat "$(DIST_DIR)/SHA256SUMS"

formula: ## Render the Homebrew formula from dist/SHA256SUMS to stdout
	@test -f "$(DIST_DIR)/SHA256SUMS" || { \
		echo "make formula: $(DIST_DIR)/SHA256SUMS missing — run 'make dist VERSION=$(VERSION)' first" >&2; \
		exit 1; }
# awk re-prints the placeholder itself when no line matches, so sed's
# substitution is a no-op instead of a deletion: the case guard below needs
# literal @SHA_...@ text left in the output to catch, not silent emptiness.
	@out=$$(sed \
		-e "s|@VERSION@|$(VERSION)|g" \
		-e "s|@SHA_DARWIN_ARM64@|$$(awk -v f="workenv-$(VERSION)-darwin-arm64.tar.gz" '$$2==f{print $$1;found=1}END{if(!found)print "@SHA_DARWIN_ARM64@"}' "$(DIST_DIR)/SHA256SUMS")|g" \
		-e "s|@SHA_LINUX_AMD64@|$$(awk -v f="workenv-$(VERSION)-linux-amd64.tar.gz" '$$2==f{print $$1;found=1}END{if(!found)print "@SHA_LINUX_AMD64@"}' "$(DIST_DIR)/SHA256SUMS")|g" \
		-e "s|@SHA_LINUX_ARM64@|$$(awk -v f="workenv-$(VERSION)-linux-arm64.tar.gz" '$$2==f{print $$1;found=1}END{if(!found)print "@SHA_LINUX_ARM64@"}' "$(DIST_DIR)/SHA256SUMS")|g" \
		packaging/workenv.rb.tmpl); \
	case "$$out" in \
		*@VERSION@*|*@SHA_*) \
			echo "make formula: unresolved placeholder — is dist/SHA256SUMS complete?" >&2; \
			exit 1;; \
	esac; \
	printf '%s\n' "$$out"

shell: | $(CACHE_DIR) ## Open an interactive shell in the Go container
	$(DOCKER_RUN) -it $(GO_IMAGE) bash

clean: ## Remove build output
	rm -rf "$(dir $(BIN))" "$(DIST_DIR)"

clean-cache: ## Remove the shared Go build/module cache
	rm -rf "$(CACHE_DIR)"

$(CACHE_DIR):
	@mkdir -p "$@"
