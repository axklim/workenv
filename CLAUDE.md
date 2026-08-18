# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build and test

**There is no local Go toolchain — every target runs the Go toolchain in a
Docker container**, so `go build` / `go test` fail outright. Use the Makefile
from the repository root:

```
make check     # gofmt check, go vet, whole suite — the gate before committing
make build     # -> bin/we, cross-compiled for the host
make fmt       # rewrite with gofmt
make help      # all targets
```

One package, or one test, needs the container invocation the Makefile wraps:

```
docker run --rm -v "$PWD":/src -w /src -v "$HOME/.cache/workenv-go":/cache \
  -u $(id -u):$(id -g) -e HOME=/cache -e GOCACHE=/cache/build \
  -e GOMODCACHE=/cache/mod -e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false \
  golang:1.23.5 go test -run TestName -v ./internal/we/
```

The container sets `HOME=/cache`, so a test that reads `os.UserHomeDir()` must
set `HOME` itself with `t.Setenv` and compare against that value.

## The design doc is the authority

`docs/superpowers/specs/2026-08-17-workenv-design.md` specifies the state
schema, resolution order for every target kind, naming, placement, repair,
delete and listing semantics. Read it before changing behaviour; if code and
spec disagree, the spec wins or the spec gets amended deliberately — not
silently.

It is a **snapshot of the design as implemented**, and so is
`docs/superpowers/plans/` — a record of how it was built, not a running
description of the tool. `docs/usecases.md` is the opposite: current
behaviour, user-facing, and the thing to update when behaviour changes. The
two overlap today (the use cases were copied out of the spec), which is fine —
do not "fix" it by deleting either.

## Architecture

`cmd/we` parses flags and owns **all** rendering; `internal/we` owns the flows
and is the only package that coordinates others.

The heart of it is a two-phase split in `internal/we`, worth understanding
before editing either half:

- **`resolve.go` decides *which* environment** a target means — registry,
  then GitHub links, then git worktrees, with one resolver per target kind
  (issue, PR, repository URL, plain string) converging on a shared `finish`.
  Its only side effect is recording the environment.
- **`we.go` materialises it** — `repair` creates a missing worktree, a missing
  tmux session, and refreshes the branch from git. It runs for `open` *and*
  `attach`, which is why "attach never creates an environment" and "attach
  still fixes a broken one" are both true.

`Save` happens only after materialisation succeeds, so a failure leaves the
filesystem ahead of the registry — the direction that self-heals on the next
`open` (an existing worktree on the branch is adopted).

Supporting packages: `state` (the JSON registry — pure data plus one file, no
git or exec), `gitx`, `gh`, `tmuxx` (thin command wrappers), `target` (parses
what the user typed, renders canonical reference URLs), `naming` (slugs,
sanitizing, session names), `wtpath` (renders the `worktree_path` template),
`config`, `execx`.

## Conventions

- **Standard library only.** `go.mod` has no requirements and should stay that
  way; the path template is `text/template`, not a dependency.
- **Every external command goes through `execx.Runner`** — never `os/exec`
  directly. That is what makes the flows testable.
- **References are canonical URLs.** `target.Target.URL()` produces what the
  registry stores, and `state.ByRef` compares them; if you store a URL built
  any other way, a later lookup misses it.
- **`state.ByBranch` takes a repository path, not a project name** — two
  clones of one repository share a project name.
- **The registry format is unreleased**: no migration code, no compatibility
  aliases. Delete what a redesign replaces; `Load` rejects a pre-release file.

## Testing

`execx.Fake` matches scripted responses by **prefix** of `"name arg arg …"` and
**returns success with empty output for anything unscripted**. A test that only
checks `err == nil` therefore passes vacuously — assert on the recorded calls
(`Fake.Calls`, including `Dir` and `Method`) and on the persisted registry
instead. `FakeCall.Method` exists so a test can prove the caller used
`Output` / `Run` / `OutputPassStderr` deliberately.

## tmux gotcha

The `=name` exact-match target syntax is valid only for session-target
commands (`has-session`, `kill-session`, …). Pane-target commands (`send-keys`,
`set-option`, `show-options`, `capture-pane`) reject it, so `we` passes the
bare session name there — exact names still beat prefix matches.
