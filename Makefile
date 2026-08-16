# Every target runs the Go toolchain inside a container, so the project
# builds and tests with nothing but Docker installed. Override GO_IMAGE to
# pick a different Go version.

GO_IMAGE    ?= golang:1.23.5
DOCKER      ?= docker
BIN         ?= bin/we
INSTALL_DIR ?= $(HOME)/bin

# The container is Linux, but the binary has to run on *this* machine, so
# builds always cross-compile to the host's OS/arch. Override GOOS/GOARCH to
# build for somewhere else (e.g. `make build GOOS=linux GOARCH=amd64`).
GOOS   ?= $(shell uname -s | tr '[:upper:]' '[:lower:]')
GOARCH ?= $(patsubst aarch64,arm64,$(patsubst x86_64,amd64,$(shell uname -m)))

# Kept outside the repo: `./...` skips dot-directories, but gofmt does not,
# and a module cache full of third-party sources is not project source.
CACHE_DIR ?= $(HOME)/.cache/workenv-go

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
.PHONY: help build test vet fmt fmt-check check install shell clean clean-cache

help: ## Show this help
	@grep -hE '^[a-z-]+:.*##' $(MAKEFILE_LIST) \
		| sed -e 's/:.*##[[:space:]]*/\t/' \
		| awk -F'\t' '{ printf "  \033[1m%-12s\033[0m %s\n", $$1, $$2 }'

build: | $(CACHE_DIR) ## Build ./cmd/we into bin/we for this host
	@mkdir -p "$(dir $(BIN))"
	$(DOCKER_RUN) -e GOOS=$(GOOS) -e GOARCH=$(GOARCH) $(GO_IMAGE) \
		go build -trimpath -o "$(BIN)" ./cmd/we
	@echo "built $(BIN) ($(GOOS)/$(GOARCH))"

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

install: build ## Install the binary into ~/bin (override INSTALL_DIR)
	@mkdir -p "$(INSTALL_DIR)"
	install -m 0755 "$(BIN)" "$(INSTALL_DIR)/we"
	@echo "installed $(INSTALL_DIR)/we"

shell: | $(CACHE_DIR) ## Open an interactive shell in the Go container
	$(DOCKER_RUN) -it $(GO_IMAGE) bash

clean: ## Remove build output
	rm -rf "$(dir $(BIN))"

clean-cache: ## Remove the shared Go build/module cache
	rm -rf "$(CACHE_DIR)"

$(CACHE_DIR):
	@mkdir -p "$@"
