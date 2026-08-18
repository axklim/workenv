# Homebrew Distribution Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `brew install axklim/tap/workenv` install a prebuilt `we`, and make
cutting a release one manual dispatch that ends having proven a clean install works.

**Architecture:** The pinned `golang:1.23.5` container cross-compiles three platform
binaries; `make dist` packages them and `make formula` renders a Homebrew formula from
a template in this repository. A `workflow_dispatch` release job on Linux tests, tags,
publishes and pushes the rendered formula into `axklim/homebrew-tap`; a dependent macOS
job then taps and installs what was just published. Homebrew unpacks a tarball — it
never builds.

**Tech Stack:** Go 1.23.5 (standard library only, run in Docker), GNU Make, GitHub
Actions, Homebrew formula DSL (Ruby).

**Spec:** `docs/superpowers/specs/2026-08-18-homebrew-distribution-design.md`

## Global Constraints

- **No local Go toolchain exists.** `go build` / `go test` fail on this machine.
  Every Go command runs in the container the Makefile wraps. Use `make check`,
  `make build`, `make dist`.
- **Standard library only.** `go.mod` has no requirements and must stay that way.
- **Go image is pinned:** `golang:1.23.5`, via the Makefile's `GO_IMAGE`.
- **The container sets `HOME=/cache`**, so any test reading `os.UserHomeDir()` must set
  `HOME` itself with `t.Setenv` and compare against that value.
- **Formula name is `workenv`; the installed binary is `we`.**
- **Platforms are exactly:** `darwin/arm64`, `linux/amd64`, `linux/arm64`.
- **Release versions are `X.Y.Z`** with no leading `v`; the git tag is `vX.Y.Z`.
- **Tap repository:** `axklim/homebrew-tap`, formula path `Formula/workenv.rb`.
- **Asset names:** `workenv-<version>-<os>-<arch>.tar.gz`, flat (no top-level dir).
- **Markdown in this repo wraps at ~90 columns**; tables stay ≤ 3 short columns and are
  padded so they line up in the raw source.
- **Commit messages use Conventional Commits.**

---

### Task 1: `we` reports its version

**Files:**
- Create: `cmd/we/version.go`
- Modify: `cmd/we/main.go` (usage text, and `run()` at lines 82-90)
- Test: `cmd/we/main_test.go` (append; it already has a `captureStdout` helper at
  line 98)

**Interfaces:**
- Consumes: nothing.
- Produces: package-level `var version string` in `package main` at `./cmd/we`, so
  `-ldflags "-X main.version=0.1.0"` can stamp it (Task 2). The commands `we version`
  and `we --version` print the bare version and a trailing newline, nothing else.

- [ ] **Step 1: Write the failing tests**

Append to `cmd/we/main_test.go`:

```go
// TestVersionCommandPrintsBareVersion pins the output shape the Homebrew
// formula's test block depends on: the version alone, with no "we " prefix,
// so the formula can assert equality rather than match a pattern.
func TestVersionCommandPrintsBareVersion(t *testing.T) {
	orig := version
	version = "9.9.9"
	t.Cleanup(func() { version = orig })

	for _, arg := range []string{"version", "--version"} {
		out := captureStdout(t, func() {
			if err := run([]string{arg}); err != nil {
				t.Fatalf("run(%q): %v", arg, err)
			}
		})
		if out != "9.9.9\n" {
			t.Errorf("run(%q) printed %q, want %q", arg, out, "9.9.9\n")
		}
	}
}

// TestVersionIgnoresBrokenConfig proves the version is answered before
// config.Load(). A binary that cannot report its version on a machine with a
// bad config is useless to `brew test`, which runs against a pristine
// sandbox and to a user trying to work out what they have installed.
func TestVersionIgnoresBrokenConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))

	cfgDir := filepath.Join(dir, "config", "workenv")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	broken := []byte("this line has no equals sign\n")
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), broken, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Sanity check: the config really is broken enough to stop a normal
	// command. Without this, the assertion below could pass vacuously.
	if err := run([]string{"ls"}); err == nil {
		t.Fatal("expected `we ls` to fail on an unparsable config")
	}

	out := captureStdout(t, func() {
		if err := run([]string{"--version"}); err != nil {
			t.Fatalf("run --version: %v", err)
		}
	})
	if strings.TrimSpace(out) == "" {
		t.Error("--version printed nothing")
	}
}
```

`filepath`, `os` and `strings` are already imported by this file.

- [ ] **Step 2: Run the tests to verify they fail**

```
docker run --rm -v "$PWD":/src -w /src -v "$HOME/.cache/workenv-go":/cache \
  -u $(id -u):$(id -g) -e HOME=/cache -e GOCACHE=/cache/build \
  -e GOMODCACHE=/cache/mod -e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false \
  golang:1.23.5 go test -run TestVersion -v ./cmd/we/
```

Expected: FAIL to compile — `undefined: version`.

- [ ] **Step 3: Create `cmd/we/version.go`**

```go
package main

// version is stamped at link time with `-ldflags "-X main.version=0.1.0"` by
// `make build` and by the release workflow. An unstamped build — a plain
// `go build ./cmd/we` — reports "dev", which is the truthful answer for a
// binary with no release behind it.
var version = "dev"
```

- [ ] **Step 4: Answer the version before the config is loaded**

In `cmd/we/main.go`, `run()` currently opens with the help block and then calls
`config.Load()`. Insert this immediately after the help block's closing brace and
before `cfg, err := config.Load()`:

```go
	// Answered before config.Load() on purpose: a machine with an unparsable
	// config must still be able to say which version it has, and `brew test`
	// runs the binary in a sandbox with no config at all.
	if args[0] == "version" || args[0] == "--version" {
		fmt.Println(version)
		return nil
	}
```

- [ ] **Step 5: Add it to the usage text**

In the `usage` const in `cmd/we/main.go`, add a line to the `Usage:` block directly
below the `we delete …` lines, keeping the existing alignment:

```
  we version
```

- [ ] **Step 6: Run the tests to verify they pass**

```
docker run --rm -v "$PWD":/src -w /src -v "$HOME/.cache/workenv-go":/cache \
  -u $(id -u):$(id -g) -e HOME=/cache -e GOCACHE=/cache/build \
  -e GOMODCACHE=/cache/mod -e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false \
  golang:1.23.5 go test -run TestVersion -v ./cmd/we/
```

Expected: PASS, both tests.

- [ ] **Step 7: Run the whole gate**

Run: `make check`
Expected: gofmt clean, vet clean, all tests pass.

- [ ] **Step 8: Commit**

```bash
git add cmd/we/version.go cmd/we/main.go cmd/we/main_test.go
git commit -m "feat: report the binary's version with we version"
```

---

### Task 2: `make build` stamps the version in

**Files:**
- Modify: `Makefile` (variables block near the top, and the `build` target)

**Interfaces:**
- Consumes: `var version` from Task 1.
- Produces: `VERSION` and `LDFLAGS` make variables. `VERSION` defaults to
  `git describe --tags --always --dirty` with any leading `v` stripped, or `dev` when
  git says nothing. Every later task overrides it as `make <target> VERSION=X.Y.Z`.

- [ ] **Step 1: Show the gap**

```bash
make build && ./bin/we --version
```

Expected: prints `dev` — the binary has no idea what it is.

- [ ] **Step 2: Add the version variables**

In `Makefile`, directly below the `CACHE_DIR ?= …` line, add:

```make
# The version stamped into the binary. `git describe` gives a local build
# something truthful (0.1.0-3-gabc1234-dirty); releases pass VERSION
# explicitly rather than trusting the working tree. -s -w drop the symbol
# table and DWARF data, which a shipped CLI has no use for.
GIT_VERSION := $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
VERSION     ?= $(if $(GIT_VERSION),$(GIT_VERSION),dev)
LDFLAGS      = -s -w -X main.version=$(VERSION)
```

- [ ] **Step 3: Pass the flags in `build`**

Change the `build` target's compile line from:

```make
	$(DOCKER_RUN) -e GOOS=$(GOOS) -e GOARCH=$(GOARCH) $(GO_IMAGE) \
		go build -trimpath -o "$(BIN)" ./cmd/we
```

to:

```make
	$(DOCKER_RUN) -e GOOS=$(GOOS) -e GOARCH=$(GOARCH) $(GO_IMAGE) \
		go build -trimpath -ldflags '$(LDFLAGS)' -o "$(BIN)" ./cmd/we
```

and change the final echo to report it:

```make
	@echo "built $(BIN) ($(GOOS)/$(GOARCH), version $(VERSION))"
```

- [ ] **Step 4: Verify the default stamping**

```bash
make build && ./bin/we --version
```

Expected: a git-derived string, not `dev`. There are no tags yet, so
`git describe --tags --always` falls back to the short commit SHA — something like
`740932d` or `740932d-dirty`. That is correct behaviour, not a bug.

- [ ] **Step 5: Verify an explicit override**

```bash
make build VERSION=1.2.3 && ./bin/we --version
```

Expected: exactly `1.2.3`.

- [ ] **Step 6: Commit**

```bash
git add Makefile
git commit -m "build: stamp the version into the binary with ldflags"
```

---

### Task 3: `make dist` cross-compiles and packages the release artifacts

**Files:**
- Modify: `Makefile` (variables, `.PHONY`, new `dist` target, `clean`)
- Modify: `.gitignore`

**Interfaces:**
- Consumes: `VERSION` and `LDFLAGS` from Task 2.
- Produces: `make dist VERSION=X.Y.Z` writes `dist/workenv-X.Y.Z-<os>-<arch>.tar.gz` for
  `darwin-arm64`, `linux-amd64` and `linux-arm64`, each a flat archive containing `we`,
  `LICENSE` and `README.md`, plus `dist/SHA256SUMS` in `shasum -a 256` format
  (`<hash>  <filename>`). Task 4 reads that file; Tasks 5 and 6 consume the tarballs.

- [ ] **Step 1: Show the gap**

```bash
make dist VERSION=0.0.0-test
```

Expected: `make: *** No rule to make target 'dist'.  Stop.`

- [ ] **Step 2: Add the platform list**

In `Makefile`, below the `LDFLAGS` line from Task 2:

```make
# Release targets. darwin/amd64 is deliberately absent: it cannot be tested
# here, and a support claim with nothing behind it is worse than none.
DIST_DIR       ?= dist
DIST_PLATFORMS  = darwin/arm64 linux/amd64 linux/arm64
```

- [ ] **Step 3: Add the `dist` target**

Add after the `install` target:

```make
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
```

Three things that are load-bearing and easy to get wrong:

- **`|| exit 1` on every command inside the loop.** A `for` loop in a make recipe is one
  shell command, so make only sees the *last* iteration's status. Without these, a
  failed cross-compile produces a tarball of stale or missing files and make reports
  success.
- **`tar -C "$$stage" we LICENSE README.md`**, not `tar "$$stage"`. Homebrew strips a
  single top-level directory when unpacking; three entries at the top means nothing is
  stripped and `bin.install "we"` finds its file.
- **`shasum -a 256`**, not `sha256sum`. `shasum` exists on both macOS and the Linux
  runner; `sha256sum` does not exist on macOS.

- [ ] **Step 4: Extend `.PHONY` and `clean`**

Change the `.PHONY` line to include the new targets:

```make
.PHONY: help build test vet fmt fmt-check check install dist formula shell clean clean-cache
```

(`formula` arrives in Task 4; declaring it now keeps this to one edit.)

Change `clean` to drop the dist directory too:

```make
clean: ## Remove build output
	rm -rf "$(dir $(BIN))" "$(DIST_DIR)"
```

- [ ] **Step 5: Ignore `dist/`**

`.gitignore` currently holds one line, `/bin/`. Add a second:

```
/dist/
```

- [ ] **Step 6: Run it**

```bash
make dist VERSION=0.0.0-test
```

Expected: three `packaged …` lines, then three sha256 lines printed from
`dist/SHA256SUMS`.

- [ ] **Step 7: Verify the archives are flat and correctly stamped**

```bash
tar -tzf dist/workenv-0.0.0-test-darwin-arm64.tar.gz
```

Expected exactly: `we`, `LICENSE`, `README.md` — no leading directory component.

```bash
mkdir -p /tmp/wetest
case "$(uname -m)" in arm64|aarch64) t=linux-arm64;; *) t=linux-amd64;; esac
tar -xzf "dist/workenv-0.0.0-test-$t.tar.gz" -C /tmp/wetest
docker run --rm -v /tmp/wetest:/t debian:stable-slim /t/we --version
```

Expected: `0.0.0-test`. The host is macOS, so the Linux binary needs a container — and
it has to be the tarball matching the host's architecture, or the container (which
pulls an image for the host's arch) answers `exec format error`.

- [ ] **Step 8: Verify a failed build actually fails the target**

```bash
make dist VERSION=0.0.0-test DIST_PLATFORMS=darwin/nosucharch; echo "exit=$?"
```

Expected: a Go error about an unsupported GOARCH and `exit=2`. If it prints `exit=0`,
the `|| exit 1` guards are missing.

- [ ] **Step 9: Commit**

```bash
git add Makefile .gitignore
git commit -m "build: add make dist for cross-compiled release tarballs"
```

---

### Task 4: The formula template and `make formula`

**Files:**
- Create: `packaging/workenv.rb.tmpl`
- Modify: `Makefile` (new `formula` target)

**Interfaces:**
- Consumes: `dist/SHA256SUMS` from Task 3, `VERSION` from Task 2.
- Produces: `make formula VERSION=X.Y.Z` writes a complete Homebrew formula to stdout
  and exits non-zero if any placeholder is left unresolved. Task 6 redirects it to
  `Formula/workenv.rb` in the tap clone.

- [ ] **Step 1: Show the gap**

```bash
make formula VERSION=0.0.0-test
```

Expected: `make: *** No rule to make target 'formula'.  Stop.`

- [ ] **Step 2: Create `packaging/workenv.rb.tmpl`**

```ruby
# Generated by axklim/workenv's release workflow from
# packaging/workenv.rb.tmpl. Edit it there — the next release overwrites
# this file.
class Workenv < Formula
  desc "Open a task's git worktree, tmux session and terminal in one command"
  homepage "https://github.com/axklim/workenv"
  # No explicit version: Homebrew derives it from the release asset's
  # filename below, and stating it too is redundant enough that
  # `brew audit --strict` rejects it.
  license "MIT"

  depends_on "gh"
  depends_on "tmux"
  uses_from_macos "git"

  on_macos do
    depends_on arch: :arm64

    on_arm do
      url "https://github.com/axklim/workenv/releases/download/v@VERSION@/workenv-@VERSION@-darwin-arm64.tar.gz"
      sha256 "@SHA_DARWIN_ARM64@"
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/axklim/workenv/releases/download/v@VERSION@/workenv-@VERSION@-linux-amd64.tar.gz"
      sha256 "@SHA_LINUX_AMD64@"
    end
    on_arm do
      url "https://github.com/axklim/workenv/releases/download/v@VERSION@/workenv-@VERSION@-linux-arm64.tar.gz"
      sha256 "@SHA_LINUX_ARM64@"
    end
  end

  def install
    bin.install "we"
  end

  def caveats
    <<~EOS
      we opens its terminal with Ghostty, which a formula cannot depend on:

        brew install --cask ghostty

      It also runs `claude` in the first tmux window. Claude Code is not a
      Homebrew package — install it separately, or point claude_cmd at
      something else in ~/.config/workenv/config.toml.

      Neither is needed for `we open --no-terminal`, nor on a remote host
      reached with `we --host`.
    EOS
  end

  test do
    # The check nothing else can make: a formula bumped to a version whose
    # asset was built from something else.
    assert_equal version.to_s, shell_output("#{bin}/we --version").strip

    # `we` with no arguments exits non-zero, so the usage check uses `help`.
    assert_match "we open", shell_output("#{bin}/we help")
  end
end
```

- [ ] **Step 3: Add the `formula` target**

Add to `Makefile` after `dist`:

```make
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
```

Four details, each load-bearing, none obvious:

- **The `case` guard is the point of the target.** A missing checksum must fail loudly
  rather than yield `sha256 ""` — a formula that breaks only once a user installs it.
- **awk matches the full versioned filename (`$$2==f`), not an os/arch substring.** A
  substring match would happily pull `workenv-0.0.0-test-darwin-arm64.tar.gz`'s checksum
  into a `9.9.9` render, and the guard would see nothing wrong because every placeholder
  did get substituted — with the wrong value.
- **awk re-prints the placeholder when nothing matches**, which is what gives the guard
  something to catch. `sed` substituting an *empty* capture deletes the placeholder, so
  without this the guard is blind to exactly the case it exists for.
- **Both interpolations are quoted** (`-v f="…"`, `"$(DIST_DIR)/SHA256SUMS"`). Unquoted,
  a `VERSION` containing whitespace splits the word, awk misparses the fragment as its
  program and dies before the `END` block — silently reproducing the empty-capture bug.

Note the escaping: `$$2` and `$$1` reach awk as `$2` and `$1`; `$$(…)` is a shell
command substitution, not a make variable. The comment sits at column 0, unindented, on
purpose: make flattens a backslash-continued recipe into one shell line, so a `#` inside
the continuation would comment out the rest of the recipe — including the guard.

- [ ] **Step 4: Render it**

```bash
make dist VERSION=0.0.0-test && make formula VERSION=0.0.0-test
```

Expected: the full formula on stdout, with `version "0.0.0-test"`, three distinct
64-character checksums, and no `@…@` left anywhere.

- [ ] **Step 5: Verify it is valid Ruby**

```bash
make formula VERSION=0.0.0-test | docker run --rm -i ruby:3.3-slim ruby -c
```

Expected: `Syntax OK`.

- [ ] **Step 6: Verify the guard fires**

```bash
rm dist/SHA256SUMS && make formula VERSION=0.0.0-test; echo "exit=$?"
```

Expected: the "SHA256SUMS missing" message and `exit=2` (make's exit code for any failed
recipe).

```bash
make dist VERSION=0.0.0-test >/dev/null && make formula VERSION=9.9.9; echo "exit=$?"
```

Expected: the "unresolved placeholder" message and `exit=2` — the checksums file names
`0.0.0-test` tarballs, so none match a `9.9.9` render. This is the stale-artifact case
the guard exists for.

```bash
make formula VERSION="1.0 test"; echo "exit=$?"
```

Expected: the same "unresolved placeholder" message and `exit=2`. A version with
whitespace must fail loudly rather than render a formula with an empty `sha256`.

- [ ] **Step 7: Commit**

```bash
git add packaging/workenv.rb.tmpl Makefile
git commit -m "build: render the Homebrew formula from a template"
```

---

### Task 5: CI workflow

**Files:**
- Create: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: `make check`, `make dist`, `make formula` from Tasks 2-4.
- Produces: nothing later tasks depend on. This is the gate that keeps the release path
  working between releases.

- [ ] **Step 1: Create the workflow**

```yaml
name: CI

on:
  pull_request:
  push:
    branches: [main]

# ubuntu, not macos: every compiling step here runs the Go toolchain in
# Docker, and GitHub's macOS runners have no Docker daemon.
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7

      - name: gofmt, vet and the test suite
        run: make check

      # Steps below exercise the release path on every pull request. Finding
      # out at tag time that packaging is broken is the failure this prevents.
      - name: Cross-compile and package
        run: make dist VERSION=0.0.0-ci

      - name: The packaged binary reports the stamped version
        # Also proves the tarball is flat: a nested layout would not put `we`
        # where this extraction looks for it.
        run: |
          set -euo pipefail
          tar -xzf dist/workenv-0.0.0-ci-linux-amd64.tar.gz -C "$RUNNER_TEMP"
          test "$("$RUNNER_TEMP/we" --version)" = "0.0.0-ci"

      - name: The rendered formula is valid Ruby
        run: make formula VERSION=0.0.0-ci | ruby -c
```

There is deliberately no build cache. Pulling the pinned image dominates the run, and a
cold `make check` on a module with no dependencies takes under a minute.

- [ ] **Step 2: Lint the workflow**

```bash
docker run --rm -v "$PWD":/repo -w /repo rhysd/actionlint:latest -color
```

Expected: no output, exit 0. actionlint parses the YAML, checks the expression syntax
and shellchecks every `run:` block — the whole reason the `set -euo pipefail` line is
there.

- [ ] **Step 3: Run the workflow's own steps locally**

```bash
make check \
  && make dist VERSION=0.0.0-ci \
  && make formula VERSION=0.0.0-ci | docker run --rm -i ruby:3.3-slim ruby -c
```

Expected: tests pass, three tarballs, `Syntax OK`.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: check, package and render the formula on every pull request"
```

---

### Task 6: Release workflow

**Files:**
- Create: `.github/workflows/release.yml`

**Interfaces:**
- Consumes: `make check`, `make dist`, `make formula`; the repository secret
  `TAP_TOKEN`.
- Produces: a `vX.Y.Z` tag, a GitHub release carrying the three tarballs and
  `SHA256SUMS`, and `Formula/workenv.rb` in `axklim/homebrew-tap`.

- [ ] **Step 1: Create the workflow**

```yaml
name: Release

# Manual on purpose. A release is a decision, not a consequence of pushing
# to main.
on:
  workflow_dispatch:
    inputs:
      version:
        description: "Version to release, without the leading v (e.g. 0.1.0)"
        required: true

permissions:
  contents: write

jobs:
  release:
    runs-on: ubuntu-latest
    # Set once here and read back as "$VERSION" in every run: block below.
    # Splicing ${{ inputs.version }} directly into shell text lets GitHub
    # substitute it before bash parses anything, so a version containing a
    # `"` breaks out of quoting and runs arbitrary commands with this job's
    # contents:write and, later, TAP_TOKEN in scope.
    env:
      VERSION: ${{ inputs.version }}
    steps:
      - uses: actions/checkout@v7
        with:
          ref: main
          fetch-depth: 0

      - name: Validate the version and refuse to reuse a tag
        # If this trips: a formula-only problem needs no new version —
        # re-render `make formula VERSION=X.Y.Z` in a tap checkout and push
        # that. Anything needing a different binary means cutting the next
        # patch version instead — deleting a published tag to reuse it is
        # worse than superseding it.
        run: |
          set -euo pipefail
          V="$VERSION"
          echo "$V" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$' \
            || { echo "::error::version must be X.Y.Z, got '$V'"; exit 1; }
          if git rev-parse "v$V" >/dev/null 2>&1; then
            echo "::error::tag v$V already exists"; exit 1
          fi

      - name: Test before building anything
        run: make check

      - name: Cross-compile and package
        run: make dist VERSION="$VERSION"

      - name: The artifacts are complete and correctly stamped
        run: |
          set -euo pipefail
          V="$VERSION"
          test -f "dist/workenv-$V-darwin-arm64.tar.gz"
          test -f "dist/workenv-$V-linux-amd64.tar.gz"
          test -f "dist/workenv-$V-linux-arm64.tar.gz"
          test -f dist/SHA256SUMS

          # The checksums that end up in the formula, proven against the
          # files actually about to be uploaded.
          (cd dist && shasum -a 256 -c SHA256SUMS)

          # Extract every platform, not just the one this runner can
          # execute, and confirm each binary is actually built for the
          # architecture its filename claims. The realistic failure is a
          # cross-compile that silently produced the wrong architecture.
          mkdir -p "$RUNNER_TEMP/darwin-arm64" "$RUNNER_TEMP/linux-amd64" "$RUNNER_TEMP/linux-arm64"
          tar -xzf "dist/workenv-$V-darwin-arm64.tar.gz" -C "$RUNNER_TEMP/darwin-arm64"
          tar -xzf "dist/workenv-$V-linux-amd64.tar.gz" -C "$RUNNER_TEMP/linux-amd64"
          tar -xzf "dist/workenv-$V-linux-arm64.tar.gz" -C "$RUNNER_TEMP/linux-arm64"

          file "$RUNNER_TEMP/darwin-arm64/we" | grep -q 'Mach-O.*arm64' \
            || { echo "::error::darwin-arm64/we is not a Mach-O arm64 binary"; exit 1; }
          file "$RUNNER_TEMP/linux-amd64/we" | grep -q 'ELF.*x86-64' \
            || { echo "::error::linux-amd64/we is not an ELF x86-64 binary"; exit 1; }
          file "$RUNNER_TEMP/linux-arm64/we" | grep -q 'ELF.*aarch64' \
            || { echo "::error::linux-arm64/we is not an ELF aarch64 binary"; exit 1; }

          # Only linux/amd64 can actually run on this runner.
          test "$("$RUNNER_TEMP/linux-amd64/we" --version)" = "$V"

      # Nothing above this line published anything; everything below does.
      - name: Tag it
        # A tag and no commit: the version is stamped at link time, so a
        # release changes no file in the repository.
        run: |
          set -euo pipefail
          V="$VERSION"
          git config user.name "github-actions[bot]"
          git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
          git tag -a "v$V" -m "workenv $V"
          git push origin "v$V"

      - name: Publish the release
        # Before the formula bump, not after: the formula points at these
        # assets' URLs and checksums, so they have to exist first.
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          set -euo pipefail
          V="$VERSION"
          cat > notes.md <<EOF
          ## Install

          \`\`\`sh
          brew install axklim/tap/workenv
          \`\`\`

          Installs the \`we\` binary from the prebuilt tarballs below —
          macOS on Apple silicon, Linux on amd64 and arm64. Homebrew pulls
          \`gh\` and \`tmux\`; Ghostty and \`claude\` are not Homebrew
          packages and are installed separately.

          Checksums are in \`SHA256SUMS\`.
          EOF
          gh release create "v$V" dist/*.tar.gz dist/SHA256SUMS \
            --title "workenv $V" --notes-file notes.md

      - name: Render the formula into axklim/homebrew-tap
        # GITHUB_TOKEN cannot reach another repository, so this needs
        # TAP_TOKEN — a fine-grained PAT with contents:write on the tap.
        env:
          TAP_TOKEN: ${{ secrets.TAP_TOKEN }}
        run: |
          set -euo pipefail
          V="$VERSION"
          git clone "https://x-access-token:$TAP_TOKEN@github.com/axklim/homebrew-tap.git" tap
          make formula VERSION="$V" > tap/Formula/workenv.rb
          cd tap
          git config user.name "github-actions[bot]"
          git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
          # add, not commit -a: the first release creates this file rather
          # than editing it, and -a ignores untracked paths.
          git add Formula/workenv.rb
          if git diff --quiet HEAD; then
            echo "formula already points at v$V"
          else
            git commit -m "fix(workenv): bump to $V"
            git push origin HEAD:main
          fi

  verify:
    needs: release
    runs-on: macos-15
    env:
      VERSION: ${{ inputs.version }}
    steps:
      - name: Install through the tap exactly as a user would
        # The real gate: the published asset, through the formula just
        # pushed. `brew test` asserts `we --version` equals the formula's
        # version, so a botched bump fails here.
        run: |
          set -euo pipefail
          brew tap axklim/tap
          brew trust --formula axklim/tap/workenv
          brew install axklim/tap/workenv
          brew test workenv
          brew audit --strict --formula axklim/tap/workenv
          test "$(we --version)" = "$VERSION"
```

- [ ] **Step 2: Lint the workflow**

```bash
docker run --rm -v "$PWD":/repo -w /repo rhysd/actionlint:latest -color
```

Expected: no output, exit 0. actionlint validates YAML, expressions and shell, but knows
nothing about Homebrew — whether `brew trust` exists on the runner is settled in Task 8,
not here.

Two things in this workflow are the way they are for reasons actionlint cannot see. The
version input reaches every `run:` block through a job-level `env:` rather than being
spliced in as `${{ inputs.version }}`: GitHub substitutes those expressions into the
script text *before* bash parses it, so an inline form would let a crafted version break
out of its quotes — inside the validate step itself, before the `X.Y.Z` regex ever runs.
And the artifact-assertion step checks all three binaries with `file` while executing
only the linux/amd64 one, because running an arm64 binary on an amd64 runner would mean
pulling in qemu and a third-party action; the architecture check catches the realistic
failure (a cross-compile that silently produced the wrong target) without that.

- [ ] **Step 3: Rehearse the tap render locally**

This proves the one step that writes to another repository, without writing to it:

```bash
make dist VERSION=0.0.0-test >/dev/null
git clone --depth 1 https://github.com/axklim/homebrew-tap.git /tmp/tap-rehearsal
make formula VERSION=0.0.0-test > /tmp/tap-rehearsal/Formula/workenv.rb
git -C /tmp/tap-rehearsal status --short
docker run --rm -i ruby:3.3-slim ruby -c < /tmp/tap-rehearsal/Formula/workenv.rb
```

Expected: `?? Formula/workenv.rb` (untracked — which is why the workflow uses
`git add`), and `Syntax OK`. Then clean up: `rm -rf /tmp/tap-rehearsal`.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: add the manual release workflow and tap formula bump"
```

---

### Task 7: Documentation

**Files:**
- Modify: `README.md` (Prerequisites and Install sections, usage block)
- Modify: `CLAUDE.md` (a new "Releasing" section)

**Interfaces:**
- Consumes: everything above.
- Produces: nothing later tasks depend on.

- [ ] **Step 1: Rewrite the README's Prerequisites and Install sections**

Replace the existing `## Prerequisites` and `## Install` sections with:

```markdown
## Prerequisites

`git`, `gh` (authenticated), `tmux`, [Ghostty](https://ghostty.org), and
`claude` on your PATH. Homebrew installs `gh` and `tmux` for you; Ghostty is a
cask and Claude Code is not a Homebrew package, so both are yours to install.

## Install

```
brew install axklim/tap/workenv
```

macOS on Apple silicon, and Linux on amd64 or arm64 — the last of which is how
a host reached with `--host` gets `we`.

To build it instead: every `make` target runs the Go toolchain in a container,
so Docker is the only build dependency.

```
make build     # -> bin/we (native, cross-compiled for the host)
make install   # -> ~/.local/bin/we
make check     # gofmt, go vet, tests
```

With a Go toolchain of your own, `go build -o ~/.local/bin/we ./cmd/we` also
works — the binary then reports its version as `dev`, since nothing stamped it.

`make help` lists all targets.
```

Keep the ~90 column wrap and leave the rest of the README alone.

`docs/usecases.md` is deliberately untouched: nothing about the workflows it describes
changes, and the only new surface is `we version`.

- [ ] **Step 2: Add `we version` to the README usage block**

In the `## Usage` fenced block, below the `we delete …` lines, add:

```
we version
```

- [ ] **Step 3: Add a Releasing section to CLAUDE.md**

Insert after the "Build and test" section:

```markdown
## Releasing

A release is a manual `workflow_dispatch` of `.github/workflows/release.yml`
with a bare `X.Y.Z` version. It runs `make check`, cross-compiles the three
release platforms, tags, publishes, then renders the Homebrew formula into
`axklim/homebrew-tap` and installs it on a macOS runner to prove it works.

**The formula's source of truth is `packaging/workenv.rb.tmpl` here, not
`Formula/workenv.rb` in the tap** — the tap's copy is generated and the next
release overwrites it. `make dist VERSION=X.Y.Z` then `make formula
VERSION=X.Y.Z` reproduces locally exactly what the workflow pushes.

The version is stamped at link time (`-ldflags -X main.version=…`), so
releasing changes no file in the repository and cuts no commit — only a tag.
```

- [ ] **Step 4: Check the rendering of the changed docs**

```bash
gh api --method POST /markdown -f mode=markdown -f text="$(cat README.md)" >/dev/null && echo "README renders"
awk 'length > 90 && $0 !~ /^ *(brew|go build|https?:)/ {print FILENAME":"NR": "length}' README.md CLAUDE.md
```

Expected: `README renders`, and no over-long prose lines (URLs and command lines are
exempt).

- [ ] **Step 5: Commit**

```bash
git add README.md CLAUDE.md
git commit -m "docs: lead the install with Homebrew and document releasing"
```

---

### Task 8: Cut the first release

**Files:**
- No repository files. This task is a runbook; its deliverable is a working
  `brew install axklim/tap/workenv` and a row in the tap's README.

**Interfaces:**
- Consumes: everything above, merged to `main`.
- Produces: `v0.1.0`, the published release, `Formula/workenv.rb` in the tap.

- [ ] **Step 1: Create the `TAP_TOKEN` secret**

At <https://github.com/settings/personal-access-tokens>, create a fine-grained PAT
scoped to `axklim/homebrew-tap` only, with **Contents: Read and write**. Then:

```bash
gh secret set TAP_TOKEN --repo axklim/workenv
```

Paste the token when prompted. Verify:

```bash
gh secret list --repo axklim/workenv
```

Expected: `TAP_TOKEN` listed. Without it the release publishes but the formula step
fails, leaving a tag with no formula — recoverable, but avoidable.

Then confirm Actions can run at all, which they never have in this repository:

```bash
gh api repos/axklim/workenv/actions/permissions
```

Expected: `"enabled": true`. If not, enable Actions under Settings → Actions → General
before going further.

- [ ] **Step 2: Confirm the branch is merged and CI is green**

```bash
gh pr checks --repo axklim/workenv
```

Expected: the `check` job passing. Merge the branch before releasing — the workflow
checks out `main`.

- [ ] **Step 3: Dispatch the release**

```bash
gh workflow run release.yml --repo axklim/workenv -f version=0.1.0
gh run watch --repo axklim/workenv
```

Expected: both jobs green.

If only the `verify` job fails — `brew trust` turning out not to be a command, most
likely — the release and the formula are already correct and published. Do not cut a new
version for it: delete the offending line from `release.yml`, commit, and run Step 4
below by hand, which is the same check `verify` performs. The next release exercises the
fixed job.

- [ ] **Step 4: Verify the install from a clean state**

```bash
brew uninstall workenv 2>/dev/null; brew untap axklim/tap 2>/dev/null
brew install axklim/tap/workenv
we --version
```

Expected: `0.1.0`.

- [ ] **Step 5: Add the row to the tap's README**

```bash
git clone https://github.com/axklim/homebrew-tap.git /tmp/tap && cd /tmp/tap
```

Add a row to the formula table in `README.md`, padded so the columns line up in the raw
source alongside the existing `aerotab` and `mynah` rows:

```markdown
| [`workenv`](https://github.com/axklim/workenv) | Open a task's worktree, tmux session and terminal in one command |
```

Re-pad the whole table — the separator row included — so every pipe lines up, then:

```bash
git commit -am "docs: list workenv in the formula table"
git push
cd - && rm -rf /tmp/tap
```

- [ ] **Step 6: Close the issue**

```bash
gh issue close 6 --repo axklim/workenv \
  --comment "Released as v0.1.0. \`brew install axklim/tap/workenv\`."
```

---

## Notes for the executor

- **The commit-per-task rhythm matters more than usual here.** Tasks 5, 6 and 8 touch
  things that are awkward to undo — a pushed tag, a published release, a formula in
  another repository. A clean history is what makes fixing forward cheap.
- **Never delete a published tag to reuse its version.** If a release goes out wrong,
  a formula-only problem is fixed by re-rendering and pushing to the tap; anything
  needing a different binary means cutting the next patch version.
- **`make check` is the gate before every commit**, per CLAUDE.md.
