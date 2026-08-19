# Distributing `we` through Homebrew — design

Resolves [issue #6](https://github.com/axklim/workenv/issues/6).

`we` is installed today by building it: `go build`, or `make install` into
`~/.local/bin`. This adds the other way in — `brew install
axklim/tap/workenv` — and the release machinery behind it. The repository has
no `.github/` directory, no tags, no releases and no version anywhere in the
binary, so all of that is new.

## Goals

- `brew install axklim/tap/workenv` installs a prebuilt `we` on macOS
  (Apple silicon) and on Linux (amd64, arm64), the latter serving the remote
  half of `--host`.
- Cutting a release is one manual dispatch with a version number, and it
  ends having proven that a clean `brew install` of that version works.
- Nothing compiles `we` except the pinned `golang:1.23.5` container the
  project already builds with. Homebrew unpacks; it does not build.
- The whole release build is reproducible locally through `make`, so the
  workflow stays orchestration rather than logic.

## Non-goals

- Homebrew bottles. The formula ships prebuilt tarballs; there is no
  `brew test-bot` machinery and no bottle block.
- Intel macOS. The formula constrains the macOS side to `arm64`.
- Publishing to homebrew/core, or any distribution channel other than the
  personal tap at `axklim/homebrew-tap`.
- Automatic releases on merge. A release is a decision, not a consequence of
  pushing to `main`.

## Decisions

- **Prebuilt binaries, not a source build.** A source-build formula would
  pull Homebrew's Go on every install and compile with whatever version brew
  ships, contradicting the pinned toolchain the project builds with
  everywhere else.
- **The version is stamped at build time**, not stored in a source file. A
  tag and the binary it produced cannot disagree, so the class of release bug
  where a tag is cut without bumping a constant does not exist here.
- **The formula is generated from a template in this repository**, not
  hand-edited in the tap. Seven values move together on each release — the
  version and three url/sha pairs nested in `on_*` blocks — and a partial
  rewrite would ship a formula pointing at the previous release's binary for
  one platform.
- **The formula is named `workenv` and installs `bin/we`**, following the
  Homebrew convention of naming a formula after its project.

## Version surface

`cmd/we/version.go` holds one variable:

```go
var version = "dev"
```

`run()` answers `version` and `--version` alongside the existing `help` /
`-h` / `--help` handling in `cmd/we/main.go`, **before** `config.Load()`, so
the version is reportable on a machine whose config is broken:

```go
case "version", "--version":
    fmt.Println(version)
    return nil
```

The output is the bare version and nothing else — `0.1.0`, not `we 0.1.0` —
so the formula's test asserts equality rather than matching a pattern. The
usage text gains a `we version` line.

A binary built without stamping reports `dev`. That is the honest answer for
a build with no provenance, and it is what a plain `go build ./cmd/we`
produces.

## Build

The Makefile gains a version, linker flags and a platform list:

```make
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//')
LDFLAGS  = -s -w -X main.version=$(VERSION)
DIST_PLATFORMS = darwin/arm64:macos-arm linux/amd64:linux-intel linux/arm64:linux-arm
```

`build` passes `-ldflags '$(LDFLAGS)'`, so a local `make build` already
reports something truthful, such as `0.1.0-3-gabc1234-dirty`. `VERSION` is
overridable, and the release always passes it explicitly rather than relying
on `git describe`. The `-s -w` pair drops the symbol table and DWARF data,
which a CLI shipped as a release artifact has no use for.

Two new targets:

| Target         | Produces                                  |
|----------------|-------------------------------------------|
| `make dist`    | tarballs + `dist/SHA256SUMS`              |
| `make formula` | the rendered formula, on stdout           |

**`make dist VERSION=0.1.0`** cross-compiles each entry of `DIST_PLATFORMS`
in the pinned container and packages each as a flat
`dist/workenv-0.1.0-<label>.tar.gz` containing `we`, `LICENSE` and
`README.md`, then writes `dist/SHA256SUMS` over the tarballs with
`shasum -a 256` (present on both macOS and the Linux runner).

**The platform label carries no digits.** Homebrew scans a formula's version out
of its URL and `brew audit` rejects stating it as well — but a `-arm64` suffix
gave that scan a bare `64` to find, and what it picks varies by Homebrew
version: it read the real version locally and the arch suffix on the runner,
which published a v0.1.1 formula whose version was `64`. `macos-arm`,
`linux-intel` and `linux-arm` leave the version as the only number in the name,
so the scan cannot pick the wrong one.

Flat on purpose: Homebrew strips a single top-level directory when unpacking,
and a tarball with three entries at the top has none to strip, so
`bin.install "we"` finds its file.

**`make formula VERSION=0.1.0`** renders `packaging/workenv.rb.tmpl` to
stdout, substituting the version and reading each checksum out of
`dist/SHA256SUMS`. It fails with a clear message if `dist/SHA256SUMS` is
missing rather than emitting a formula with empty checksums.

`/dist/` joins `/bin/` in `.gitignore`.

## The formula

`packaging/workenv.rb.tmpl` carries four placeholders: `@VERSION@`,
`@SHA_DARWIN_ARM64@`, `@SHA_LINUX_AMD64@` and `@SHA_LINUX_ARM64@`. Rendered,
it lands in the tap as `Formula/workenv.rb`:

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
      url "https://github.com/axklim/workenv/releases/download/v0.1.0/workenv-0.1.0-macos-arm.tar.gz"
      sha256 "…"
    end
  end

  on_linux do
    on_intel do
      url "https://github.com/axklim/workenv/releases/download/v0.1.0/workenv-0.1.0-linux-intel.tar.gz"
      sha256 "…"
    end
    on_arm do
      url "https://github.com/axklim/workenv/releases/download/v0.1.0/workenv-0.1.0-linux-arm.tar.gz"
      sha256 "…"
    end
  end

  def install
    bin.install "we"
  end

  def caveats
    <<~EOS
      we opens its terminal with Ghostty, which Homebrew cannot depend on
      from a formula:

        brew install --cask ghostty

      It also runs `claude` in the first tmux window. Claude Code is not a
      Homebrew package — install it separately, or set claude_cmd to
      something else in ~/.config/workenv/config.toml.

      Neither is needed for `we --no-terminal`, nor on a remote host used
      through `we --host`.
    EOS
  end

  test do
    assert_equal version.to_s, shell_output("#{bin}/we --version").strip
    assert_match "we open", shell_output("#{bin}/we help")
  end
end
```

The URL points at a release asset rather than a tag tarball. Homebrew's
`Version.detect` derives `0.1.0` from it, and stating that value again with
an explicit `version` line is exactly what `brew audit --strict` flags as
redundant. So the formula carries no `version` line at all, and relies
entirely on the URL for version detection.

**Structure matters to `brew audit --strict`.** `on_macos`/`on_linux` may
never contain a bare `url`/`sha256` pair directly — that rejection is
unconditional, not contingent on a `depends_on` sitting alongside it.
`depends_on`, `on_intel` and `on_arm` are the permitted children. That is
why the macOS `url`/`sha256` sit inside a nested `on_arm` block rather than
directly in `on_macos`, and why the two top-level `depends_on` lines and
`uses_from_macos` are ordered before the `on_macos` block rather than after
it.

**Dependencies.** `gh` and `tmux` are hard runtime requirements. `git` goes
through `uses_from_macos`, giving macOS the system copy and Linux a real
dependency. `depends_on arch: :arm64` sits inside `on_macos` so it constrains
the Mac side without blocking Linux x86.

**Ghostty is not a dependency.** It is a cask on macOS, absent from Homebrew
on Linux, and unnecessary both for `--no-terminal` and for the remote side of
`--host`. It belongs in `caveats`, together with the fact that `claude` is
not a Homebrew package either, and the config path
`~/.config/workenv/config.toml`.

**The test block is the release's real assertion.** `we --version` must equal
the formula's version, which catches a formula bumped to a version whose
asset was built from something else. `we help` exits zero and prints the
usage text; `we` with no arguments exits non-zero, so the test uses `help`.

## Continuous integration

`.github/workflows/ci.yml`, on `pull_request` and pushes to `main`, one job
on `ubuntu-latest` — Docker is available there and absent from macOS
runners, which is why every compiling step in this design runs on Linux.

1. `make check`
2. `make dist VERSION=0.0.0-ci`
3. extract the linux/amd64 tarball and assert the binary reports `0.0.0-ci`
4. render `make formula VERSION=0.0.0-ci` to a file under a `Formula/`
   directory and run `brew style` on it

Steps 2 to 4 exercise cross-compilation, packaging, version stamping and
template rendering on every pull request, so the release path is continuously
tested rather than first attempted at tag time. Step 3 also proves the
tarball is flat, since a nested layout would not yield `we` where the
extraction expects it. Step 4 uses `brew style`, not `ruby -c`: parsing the
file proves it is syntactically valid Ruby, not that it is a valid formula —
`brew style` is the check that actually matches what `brew audit --strict`
grades in the release workflow's `verify` job, and it is path-sensitive, so
the rendered file has to sit under a directory literally named `Formula/`
for the right cop configuration to apply.

There is no build cache. Pulling the pinned image dominates the run, and a
cold `make check` on a module with no dependencies takes under a minute.

## Release

`.github/workflows/release.yml`, `workflow_dispatch` with a required
`version` input (without the leading `v`), `permissions: contents: write`.
Manual on purpose.

**Job `release`, `ubuntu-latest`.** Nothing is published until the artifacts
are proven:

1. validate the input matches `^[0-9]+\.[0-9]+\.[0-9]+$`, and refuse if the
   tag `v$V` already exists (checked precisely, against `refs/tags/v$V`, so a
   same-named branch or remote ref cannot pass for a tag that does not exist)
2. `make check`
3. `make dist VERSION=$V`, then extract the linux/amd64 tarball and assert
   `we --version` equals `$V`
4. render `make formula VERSION=$V` to a file under a `Formula/` directory
   and run `brew style` on it — the same pre-publish lint as CI's step 4,
   run here too because this is the version that is about to ship
5. `git tag -a v$V -m "workenv $V"` and push the tag
6. `gh release create v$V dist/*.tar.gz dist/SHA256SUMS`, with notes leading
   on `brew install axklim/tap/workenv`
7. clone `axklim/homebrew-tap` with `TAP_TOKEN`, write
   `make formula VERSION=$V` to `Formula/workenv.rb`, `git add`, and commit
   and push only if the file changed

Step 5 pushes a tag and commits nothing. Stamping the version at build time
means a release changes no file in the repository, so it mutates exactly one
ref.

Step 7 needs `git add` rather than `git commit -a`, because the first release
creates the formula rather than editing it — no placeholder formula has to be
seeded in the tap by hand. The "commit only if changed" guard makes a re-run
against an already-current formula a no-op instead of a failure.

Rendering the whole file also keeps `sed -i` out of the pipeline entirely, so
the BSD-versus-GNU difference in its argument handling never arises and
`make formula` behaves identically on macOS and on the Linux runner.

A `concurrency` group serializes the whole workflow, so two simultaneous
dispatches queue rather than race the tag push and the tap commit against
each other. The `verify` job declares `permissions: {}`: it needs no token,
unlike `release`, which needs the workflow-level `contents: write`.

**Job `verify`, `macos-15`, `needs: release`.** `brew tap axklim/tap`,
`brew trust --formula axklim/tap/workenv`, `brew install
axklim/tap/workenv`, `brew test workenv`, `brew audit --strict --formula
axklim/tap/workenv`.

This job installs the published asset through the formula that was just
pushed, which is the only step that proves what a user actually gets. The
`brew trust` step mirrors the sequence already working in `axklim/mynah`'s
release workflow; if it proves unnecessary it can be dropped.

## Failure and recovery

Anything failing in steps 1 to 4 publishes nothing, and the dispatch can be
re-run unchanged.

Past step 5 the tag exists and the version is spent. The tag guard in step 1
will refuse a retry at the same version, deliberately. Recovery splits by
what is broken:

- **A formula problem** needs no new version. `make formula VERSION=$V` in a
  tap checkout regenerates the file, and pushing that fixes installs.
- **Anything needing a different binary** means cutting the next patch
  version. Deleting a published tag to reuse it is worse than superseding it.

If `verify` fails, the release and the formula bump are already public. That
is the accepted cost of building on Linux and verifying on macOS, and it is
the same exposure `axklim/mynah` lives with today; the recovery rule above
applies unchanged.

## Testing

- `cmd/we/main_test.go` gains cases for `version` and `--version`, asserting
  the bare-version output and that neither path reads configuration.
- `make check` gates every pull request.
- `make dist` and `make formula` run on every pull request, as above.
- `brew test` and `brew audit --strict` gate every release.

## Documentation

- **README** — the Install section leads with `brew install
  axklim/tap/workenv` and keeps the `make` targets below it for development.
  Prerequisites notes that brew brings `gh` and `tmux`, while Ghostty and
  `claude` remain the user's to install. `we version` joins the usage block.
- **`axklim/homebrew-tap` README** — one row for `workenv` in the formula
  table.
- **CLAUDE.md** — a short note that releases are a manual dispatch and that
  the formula template lives in this repository, not in the tap.
- **`docs/usecases.md`** — untouched. No workflow changes, only a new
  `we version`.

## Prerequisites

Two things only the repository owner can do, both required before the first
release dispatch:

- **`TAP_TOKEN` secret on `axklim/workenv`** — a fine-grained PAT with
  `contents: write` on `axklim/homebrew-tap`. `GITHUB_TOKEN` cannot reach a
  second repository, so step 6 fails without it.
- **Actions enabled** on `axklim/workenv`.

The repository was made public while this design was written, which is what
lets a formula in the public tap fetch its assets with no token at all.
