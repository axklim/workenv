# workenv — Smart work environment

`we` opens a complete work environment for a task in one command: the project
repository (cloned if you don't have it), a git worktree on the right branch,
a tmux session with `claude` running in the first window, and a Ghostty window
attached to it.

```
we open https://github.com/axklim/trade/issues/59
```

...finds (or clones) `trade`, checks out a worktree on the branch for the
issue — the linked PR's branch if there is one, otherwise a slug of the issue
title — starts a tmux session with `claude` running, and opens Ghostty
attached to it. Run it again, or run it with the PR's URL, and it brings the
same environment back into focus: an environment is a record, not something
derived from names, so renaming the branch or the directory never breaks it.

## Prerequisites

`git`, `gh` (authenticated), `tmux`, [Ghostty](https://ghostty.org). Building
needs either a Go toolchain or Docker.

## Install

With Go installed:

```
go build -o ~/.local/bin/we ./cmd/we
```

Without it — every `make` target runs the Go toolchain in a container, so
Docker is the only build dependency:

```
make build     # -> bin/we (native, cross-compiled for the host)
make install   # -> ~/.local/bin/we
make check     # gofmt, go vet, tests
```

`make help` lists all targets.

## Usage

```
we open   <target> [--repo R] [--branch B] [--session S] [--wt W]
                   [--host H] [--no-terminal]
we attach <target> [--repo R] [--host H] [--no-terminal]
we ls     [-l] [--host H]
we show   <target> [--host H]
we delete <target> [--repo R] [--host H]
                   [--force] [--delete-branch] [--keep-worktree]
```

`ls` is an alias of `list`; `rm` and `down` of `delete`.

`<target>` is one of:

| Target         | Example                            |
|----------------|------------------------------------|
| id             | `7`                                |
| session name   | `trade-review_claude-file`         |
| branch         | `review_claude-file`               |
| issue URL      | `https://github.com/o/r/issues/59` |
| PR URL         | `https://github.com/o/r/pull/61`   |
| repository URL | `https://github.com/o/r`           |
| plain name     | `feature-123` (a branch to create) |

`open` finds an environment or creates one; `attach` only finds, so a mistyped
session name is an error rather than a new branch. `--branch`, `--session` and
`--wt` apply only when an environment is created — `attach` doesn't define
them, and `open` says so on stderr when it ignored them.

`--repo <name|path>` names the repository a **plain-name** target belongs to,
for when you're not standing in it: a bare name is looked up in
`projects_path`, a path reaches a repository outside it. Other target kinds
carry their own repository.

`we delete <target>` kills the tmux session, removes the worktree and drops
the record. `--force` removes a dirty worktree, `--delete-branch` also deletes
the branch, `--keep-worktree` stops after killing the session.

```
$ we ls
ID  PROJECT  SESSION                                       STATE     REFS
 7  trade    trade-review_claude-file                      attached  #59 PR#61
    dir: ~/projects/trade.review_claude-file
 8  trade    trade-dev-overlay-pins-a-stale-mini-internal  detached  #44
    dir: ~/projects/trade.dev-overlay-pins-a-stale-mini-internal (missing)
```

A `(missing)` worktree is recreated by the next `open`; `*` before an id marks
the environment you're standing in; refs are clickable on a terminal. `-l` and
`we show <target>` print the stacked form with full URLs.

`--host devbox` runs the command on a remote host over ssh and attaches to it
locally. The host needs `we` installed (`remote_we`), and each host keeps its
own registry — so an id on one means nothing on the other.

## Configuration

XDG notation: `~/.config/workenv/config.toml` (or
`$XDG_CONFIG_HOME/workenv/config.toml`). All keys optional:

```toml
projects_path = "~/projects"   # where repositories live / get cloned
claude_cmd    = "claude"       # command run in the first tmux window
remote_we     = "we"           # we binary path on remote hosts

# where new worktrees go — a Go text/template; variables and filters are
# documented in the design doc
worktree_path = "{{ .repo_path }}/../{{ .repo }}.{{ .branch | sanitize }}"
```

The default places worktrees as siblings of the repository:
`~/projects/trade` + branch `review_claude-file` →
`~/projects/trade.review_claude-file`. A worktree already checked out on the
branch is adopted rather than duplicated.

## How it works

Environments are recorded in `~/.local/state/workenv/envs.json`
(`$XDG_STATE_HOME`), keyed by an integer id and never derived from names: the
branch, tmux session, worktree path and the GitHub issue and PR URLs are
stored, and git stays the truth for the branch. An issue and its linked PR
resolve to one environment; resolution goes registry → GitHub links → git
worktrees. Sessions carry `@workenv` tmux user options, so `we` never adopts
or kills a session it doesn't own.

The [design doc][design] covers the rest: the state schema, resolution rules
for every target kind, placement templates, naming, repair, and worked use
cases. [docs/superpowers/plans/](docs/superpowers/plans/) holds the
implementation plans it was built from.

[design]: docs/superpowers/specs/2026-08-17-workenv-design.md
