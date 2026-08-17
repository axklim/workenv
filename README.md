# workenv — Smart work environment

`we` opens a complete work environment for a task in one command: the project
repository (cloned normally if you don't have it), a git worktree on the
right branch, a tmux session with `claude` running in the first window, and a
Ghostty window attached to it.

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
Docker is the only build dependency. The build cross-compiles for the host,
so `bin/we` is a native binary, not a Linux one:

```
make build     # -> bin/we
make install   # -> ~/.local/bin/we
make check     # gofmt, go vet, tests
```

`make help` lists all targets. `GO_IMAGE`, `BIN`, `INSTALL_DIR`, `CACHE_DIR`
and `GOOS`/`GOARCH` are overridable — e.g. `make build GOOS=linux
GOARCH=amd64`.

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

A plain name and a branch are the same syntax: an existing environment on
that branch is found; otherwise `open` creates one there, in the repository
you're standing in (or `--repo`). `attach` finds an environment by any of the
above from anywhere and never creates one, so a mistyped session name is an
error, not a new branch.

`--repo <name|path>` names the repository a **plain-name** target belongs to,
for when you are not standing in it: `we open feature-123 --repo trade`. A
bare name is looked up in `projects_path`; a value containing a separator or
starting with `~` is a path to the repository, which is how repositories
outside `projects_path` are reached. Other target kinds carry their own
repository, so `--repo` is ignored there.

`open` and `attach` are one code path, differing in a single flag: `attach`
never creates an environment. `--branch`, `--session` and `--wt` only apply
when an environment is created, so `attach` doesn't define them at all —
passing one is an error, not a silent no-op. On a hit, `open` prints a note
to stderr saying they were ignored, so re-running a command from shell
history still just attaches:

```
we: environment 7 already exists; --branch, --wt ignored
```

`we delete <target>` kills the tmux session, removes the worktree and drops
the record. `--force` removes a dirty worktree, `--delete-branch` also
deletes the branch, `--keep-worktree` stops after killing the session.

### Listing

```
$ we ls
ID  PROJECT  SESSION                                       STATE     REFS
 7  trade    trade-review_claude-file                      attached  #59 PR#61
    dir: ~/projects/trade.review_claude-file
 8  trade    trade-dev-overlay-pins-a-stale-mini-internal  detached  #44
    dir: ~/projects/trade.dev-overlay-pins-a-stale-mini-internal (missing)
```

Each environment is two lines: the row, then a `dir:` line indented under
PROJECT. `$HOME` is abbreviated to `~`; a worktree whose directory no longer
exists is marked `(missing)` — the next `open` recreates it. `REFS` is
`#<n>` for a linked issue and `PR#<n>` for a linked PR, `-` when there are
none, each a clickable link on a terminal. The environment containing the
current directory is marked with `*` before its id. Colour and hyperlinks
are suppressed when stdout isn't a terminal or `NO_COLOR` is set.

`-l` and `we show <target>` print the stacked form instead — one field per
line: id, project, branch, session, state, worktree, repository, every
linked issue and PR URL in full, and the creation time.

### Issues and pull requests

An issue and the PR linked to it are **one** environment: `we open
.../issues/59` and `we open .../pull/61` land in the same tmux session,
whichever came first. The branch is the PR's head branch when a PR exists —
the branch someone actually pushed — and a slug of the issue title
otherwise. Nothing is encoded in names: rename the branch, retitle the
issue, and the environment is still found by its recorded id.

A PR from a fork has no head branch on origin, so its branch is `pr-77`,
materialised from `refs/pull/77/head`.

### Remote hosts

`we open <target> --host devbox` runs the same command on the remote host
over ssh (with `--no-terminal` and the creation overrides passed through),
parses the `WE_SESSION=` marker it prints, and opens a local Ghostty window
running `ssh -t devbox tmux attach-session -t <session>`. `ls`, `show` and
`delete` pass through unchanged. The remote host needs `we` installed; its
path is configurable via `remote_we`. Environments are recorded per host, so
`we ls --host devbox` and `we ls` show different registries, and an id on
one means nothing on the other.

## Configuration

XDG notation: `~/.config/workenv/config.toml` (or
`$XDG_CONFIG_HOME/workenv/config.toml`). All keys optional:

```toml
projects_path = "~/projects"   # where repositories live / get cloned
claude_cmd    = "claude"       # command run in the first tmux window
remote_we     = "we"           # we binary path on remote hosts

# where new worktrees go; see "Worktree placement" below
worktree_path = "{{ .repo_path }}/../{{ .repo }}.{{ .branch | sanitize }}"
```

## Design

**Recorded, not derived.** Every environment is a record in
`~/.local/state/workenv/envs.json` (`$XDG_STATE_HOME`), keyed by an integer
**id** assigned on creation and never reused: project, branch, tmux session,
worktree path, repository path, and the GitHub issue and PR URLs it's about.
Git stays the truth for the branch — it's refreshed from the worktree
whenever an environment is opened or listed. tmux sessions created by `we`
carry the `@workenv` and `@workenv_id` user options, which is how `we ls`
tells its own sessions from personal ones and how an untagged session of the
same name is recognised as someone else's and left alone — `we` refuses to
adopt or kill it, and suggests `--session` instead.

**Resolution** goes registry → GitHub → git worktrees:

- the registry already holds that issue or PR URL, that session name, that
  id, or (within the target repository) that branch → that environment;
- an issue's linked PRs, or a PR's linked issues, already recorded → that
  environment, with the new URL attached to it;
- a git worktree already checked out on the resolved branch, made by hand or
  by worktrunk → adopted as the environment's worktree;
- otherwise `open` creates the record (and clones the repository if
  needed); `attach` stops with an error instead.

Any number that arrives through a new URL — a PR found while resolving its
issue, an issue found while resolving its PR — is recorded on whichever
environment it resolved to, so the two keep converging on one environment
even as new links appear.

**Worktree placement** for a new environment is a template,
`worktree_path` in the config, rendered per environment — the same approach
worktrunk takes, so both tools can be pointed at the same layout. The
default renders siblings of the repository directory:

```toml
worktree_path = "{{ .repo_path }}/../{{ .repo }}.{{ .branch | sanitize }}"
```

`~/projects/trade` + branch `review_claude-file` →
`~/projects/trade.review_claude-file`. A centralised layout is one line of
config instead:

```toml
worktree_path = "~/worktrees/{{ .project }}/{{ .branch | sanitize }}"
```

Available variables: `.repo_path` (absolute path of the repository),
`.repo` (its basename, without a `.git` suffix), `.project` (the project
name — the repository name from `origin` when it points at GitHub, else
`.repo`), `.owner` (GitHub owner, empty when there is none), `.branch`.
`sanitize` is a filter: the filesystem-safe form of its argument (everything
outside `[A-Za-z0-9_-]` becomes `-`, runs collapsed) — branch
`feat/static-grid` sanitizes to `feat-static-grid`. `~` expands to the home
directory, a relative result resolves against `.repo_path`, and the path is
cleaned before use. Templates are Go `text/template`.

`--wt` overrides placement per invocation: a bare name replaces the
rendered leaf (`--wt spike` → `<parent>/<repo>.spike`), a value containing a
separator or starting with `~` is used verbatim (`--wt ~/scratch/x`).

**Cloning.** A repository that isn't on disk is cloned normally — not bare —
with `gh repo clone <owner>/<repo> <projects_path>/<repo>`. A normal clone
already has the standard fetch refspec and `origin/HEAD`, so no extra refs
setup is needed, and its main working tree is the default branch's worktree:
`we open <repo-url>` on a fresh clone lands there directly rather than
creating a second worktree.

`we` still works in a repository laid out as a bare container
(`<project>/.git` bare, worktrees inside it, made by hand or by worktrunk):
existing worktrees are adopted, and the repository path is found correctly
through `git rev-parse --git-common-dir`. New worktrees for such a
repository are placed as siblings of the container directory, like anywhere
else.

**Naming.** For a new environment, the tmux session is `<project>-<branch>`
and the worktree's leaf directory is `<branch>` (both sanitized, per
`sanitize` above) — there's no `we-` prefix and no issue or PR number baked
into either. Two clones of the same repository share a project name, but
stay distinct environments because sessions and directories must be unique;
`--session` / `--wt` resolve a collision.

**Repair on open** (and attach) — each step finds before it creates: a
worktree directory that's gone is pruned and re-added at the recorded path
on the recorded branch; a tmux session that's gone (a reboot, say) is
recreated, tagged, and `claude_cmd` started in its first window; a branch
renamed inside the worktree is refreshed in the record. So a missing
worktree or a dead tmux session is simply fixed by the next `open`.

## Note on tmux target syntax

tmux ≥ 3.5: the `=name` exact-match target syntax only works for
session-target commands (`has-session`, `kill-session`, …); pane-target
commands (`send-keys`, `set-option`, `show-options`, `capture-pane`) reject
it, so `we` passes the bare session name there (exact names still beat
prefix matches).
