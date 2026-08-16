# workenv — Smart work environment

`we` tears up a complete, disposable work environment for a task in one
command: the project repository (bare-cloned if you don't have it), a git
worktree on the right branch, a tmux session with `claude` running in the
first window, and a Ghostty window attached to it.

```
we create https://github.com/acme/example-service/issues/123
```

…finds (or clones) `example-service`, creates the branch
`issue-123-<title-slug>` in a fresh worktree, starts the tagged tmux
session `we-example-service-issue-123-<title-slug>` with claude running,
and opens Ghostty attached to it. Run it again and it simply brings the
same environment back into focus.

## Prerequisites

`git`, `gh` (authenticated), `tmux`, [Ghostty](https://ghostty.org). Building
needs either a Go toolchain or Docker.

## Install

With Go installed:

```
go build -o ~/bin/we ./cmd/we
```

Without it — every `make` target runs the Go toolchain in a container, so
Docker is the only build dependency. The build cross-compiles for the host,
so `bin/we` is a native binary, not a Linux one:

```
make build     # -> bin/we
make install   # -> ~/bin/we
make check     # gofmt, go vet, tests
```

`make help` lists all targets. `GO_IMAGE`, `BIN`, `INSTALL_DIR`, `CACHE_DIR`
and `GOOS`/`GOARCH` are overridable — e.g.
`make build GOOS=linux GOARCH=amd64`.

## Usage

```
we create <target> [flags]    tear up
we list                       list environments
we delete <name> [flags]      tear down
```

`<target>` is one of:

| Target           | Example                          | Branch                   | Environment name |
|------------------|----------------------------------|--------------------------|------------------|
| GitHub issue URL | `.../example-service/issues/123` | `issue-123-<title-slug>` | same as branch   |
| GitHub PR URL    | `.../example-service/pull/456`   | PR head branch           | `pr-456`         |
| plain name       | `feature-123`                    | `feature-123`            | same             |

A PR from a fork has no head branch on origin to track, so its branch is
`pr-456` too, materialized from `refs/pull/456/head`.

Plain names use the repository you're standing in, or `--project <name>` to
pick one from the projects directory.

`we delete feature-123` kills the session and removes the worktree (the
project is inferred from session tags or the worktree layout). Flags:
`--force` (dirty worktree), `--delete-branch`, `--keep-worktree`.

### Remote hosts

`we create <target> --host devbox` runs `we` on the remote host over ssh
(terminal step skipped there) and opens a local Ghostty window attached to
the remote tmux session via `ssh -t`. `we list --host devbox` and
`we delete <name> --host devbox` pass through. The remote host needs `we`
installed (path configurable via `remote_we`).

## Configuration

XDG notation: `~/.config/workenv/config.toml` (or `$XDG_CONFIG_HOME/workenv/config.toml`).
All keys optional:

```toml
projects_dir  = "~/projects"      # where repositories live / get cloned
worktrees_dir = "~/projects/.we"  # worktree layout root (default: <projects_dir>/.we)
claude_cmd    = "claude"          # command run in the first tmux window
remote_we     = "we"              # we binary path on remote hosts
```

## Design

**Stateless.** Nothing is persisted anywhere. A work environment is fully
identified by `(project, name)`:

- worktrees live at the deterministic path `<worktrees_dir>/<project>/<name>`;
- tmux sessions are named `we-<project>-<name>` and tagged with tmux *user
  options* (`@workenv`, `@workenv_project`, `@workenv_name`,
  `@workenv_path`) — the sessions themselves are the registry `we list`
  reads, and what distinguishes a `we` session from a regular one;
- issue/PR numbers are encoded in branch and environment names
  (`issue-123-…`, `pr-456`), so they are recoverable from the name alone.

**Tear-up flow** (each step finds before it creates, so `create` is
idempotent):

1. *Project*: the repo containing the cwd (if its origin matches), else
   `<projects_dir>/<repo>` or `<repo>.git`, else `git clone --bare` via
   `gh` plus refs setup (fetch refspec for `refs/remotes/origin/*`,
   `remote set-head`), so origin-tracking branches work in the bare clone.
2. *Worktree*: reused wherever the branch is already checked out;
   otherwise created, starting the branch from `origin/<branch>` or the
   default branch as appropriate.
3. *tmux*: session created detached, tagged, and `claude_cmd` typed into
   the first window with `send-keys` (so the window outlives the command).
4. *Terminal*: inside tmux → `switch-client`; a client already attached →
   focus Ghostty; otherwise a new Ghostty window running
   `tmux attach-session`.

**Plain `git worktree`, not worktrunk** — no extra dependency, and the
deterministic layout is what the stateless design needs.

Note for tmux ≥ 3.5: the `=name` exact-match target syntax only works for
session-target commands (`has-session`, `kill-session`, …); pane-target
commands (`send-keys`, `set-option`, `show-options`, `capture-pane`) reject
it, so `we` passes the bare session name there (exact names still beat
prefix matches).
