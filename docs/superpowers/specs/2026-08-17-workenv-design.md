# workenv (`we`) — design

Supersedes the 2026-08-16 revision of this document (see git history). It
covers the whole tool, not just the delta: identity, state, naming,
placement, resolution and commands.

Resolves [issue #3](https://github.com/axklim/workenv/issues/3).

## What `we` is

One command that puts you in front of a task: the project repository (cloned
if you don't have it), a git worktree on the right branch, a tmux session
running `claude`, and a Ghostty window attached to it.

```
we open https://github.com/axklim/trade/issues/59
```

## Goals

- A work environment is **recorded**, not derived from names. Branches,
  sessions and directories can be renamed without breaking anything.
- A GitHub issue and the pull request linked to it are **one** environment;
  the branch comes from the PR when there is one.
- Every environment has a short **id** you can type: `we attach 7`.
- Worktrees live where git users expect them, with a config override.
- One Go dependency (`go-flags`, for the CLI surface); `git`, `gh`, `tmux`
  and Ghostty are the runtime ones.

## Non-goals

- Migrating environments made by earlier versions (the format is unreleased).
- Managing branches beyond checkout, creation and optional deletion.
- Supporting forges other than GitHub for issue/PR resolution.

## Model

An **environment** is one worktree, one branch, and one tmux session, plus
the GitHub issues and pull requests it is about. It is identified by an
integer `id`, assigned on creation and never reused.

Everything else about it — branch, session name, directory — is *data*, free
to change. Git remains the truth for the branch: the stored value is
refreshed from the worktree whenever an environment is opened or listed.

## State

One JSON file, `$XDG_STATE_HOME/workenv/envs.json` (default
`~/.local/state/workenv/envs.json`), written atomically (temp file + rename).
XDG places "state that should persist between restarts" here.

```json
{
  "next_id": 8,
  "envs": [
    {
      "id": 7,
      "project": "trade",
      "branch": "review_claude-file",
      "tmux_session": "trade-review_claude-file",
      "worktree_path": "/Users/u/projects/trade.review_claude-file",
      "repo_path": "/Users/u/projects/trade",
      "issues": ["https://github.com/axklim/trade/issues/59"],
      "prs": ["https://github.com/axklim/trade/pull/61"],
      "created_at": "2026-08-16T18:12:03Z"
    }
  ]
}
```

Fields:

- **id** — the key. `next_id` only ever increases, so a deleted id is never
  handed out again and a stale `we attach 7` fails instead of hitting a
  different environment.
- **project** — display name and session prefix; see *Naming*.
- **branch**, **tmux_session**, **worktree_path**, **repo_path** — stored, not
  computed.
- **issues**, **prs** — canonical GitHub URLs,
  `https://github.com/<owner>/<repo>/(issues|pull)/<n>`, no trailing slash.
  The repository travels with the number, so links to *another* repository
  are kept rather than dropped.

Invariants:

- `tmux_session` is unique across the registry.
- `worktree_path` is unique across the registry.
- An issue or PR URL belongs to at most one environment.

The registry is per host: a `--host devbox` environment is recorded in
devbox's file, and ids are per host.

## tmux tags

Sessions created by `we` carry two tmux user options: `@workenv = 1` and
`@workenv_id = <id>`. They are not the registry — the JSON file is — but
they let `we` tell its own sessions from personal ones, which matters in two
places: `ls` reads liveness only for tagged sessions, and an untagged
session is never adopted or killed (see *Adoption*).

## Naming

For a **new** environment:

| Value        | Default                                     |
|--------------|---------------------------------------------|
| branch       | see *Resolution*                            |
| tmux session | `<project>-<branch>`, sanitized             |
| worktree dir | placement rule, leaf `<branch>`, sanitized  |

`project` is the repository name from the `origin` remote when it points at
GitHub, else the `repo_path` basename with any `.git` suffix removed. Two
clones of the same repository therefore share a project name; their
environments stay distinct because sessions and directories must be unique,
and `--session` / `--wt` resolve a collision.

Sanitizing replaces everything outside `[A-Za-z0-9_-]` with `-` and collapses
runs — tmux reserves `:` and `.` in target syntax, and `/` cannot be a path
segment. So branch `feat/static-grid` yields session `trade-feat-static-grid`
and directory `trade.feat-static-grid`.

There is no `we-` prefix and no issue/PR number in any name.

## Placement

Where a new worktree goes is a **template**, `worktree_path` in the config,
rendered per environment — the same approach worktrunk takes, so both tools
can be pointed at the same layout. The default renders siblings of the
repository directory:

```toml
worktree_path = "{{ .repo_path }}/../{{ .repo }}.{{ .branch | sanitize }}"
```

`~/projects/trade` + branch `review_claude-file` →
`~/projects/trade.review_claude-file`.

Available variables and filters:

| Name           | Value                                        |
|----------------|----------------------------------------------|
| `.repo_path`   | absolute path of the repository directory    |
| `.repo`        | its basename, without any `.git` suffix      |
| `.project`     | project name (see *Naming*)                  |
| `.owner`       | GitHub owner, empty when there is none       |
| `.branch`      | the branch being checked out                 |
| `sanitize`     | filter: filesystem-safe form of its argument |

`~` expands to the home directory, a relative result resolves against
`.repo_path`, and the path is cleaned (`/../` collapsed) before use. Other
layouts are one line of config — a centralised root, for instance:

```toml
worktree_path = "~/worktrees/{{ .project }}/{{ .branch | sanitize }}"
```

Templates are Go `text/template` — no template library to depend on.
A worktrunk template is portable in shape but not verbatim: `{{ repo }}`
becomes `{{ .repo }}`, and `{% if %}` becomes `{{ if }}`.

Two rules modify the result:

1. If a worktree is already checked out on the branch — anywhere, made by
   hand or by worktrunk — it is adopted as the environment's worktree and
   nothing new is created. A repository's main working tree counts, which is
   what makes `we open <repo-url>` land in the existing checkout.
2. `--wt` overrides per invocation: a bare name replaces the rendered leaf
   (`--wt spike` → `<parent>/<repo>.spike`), a value containing a separator
   or starting with `~` is used verbatim (`--wt ~/scratch/x`).

**Cloning.** A repository that is not on disk is cloned normally — not bare —
with `gh repo clone <owner>/<repo> <projects_path>/<repo>`. A normal clone
already has the standard fetch refspec and `origin/HEAD`, so no refs setup is
needed, and its main working tree is the default branch's worktree.

`we` still works in a repository laid out as a bare container
(`<project>/.git` bare, worktrees inside it): existing worktrees are adopted,
and `repo_path` resolves correctly through `git rev-parse --git-common-dir`.
New worktrees for such a repository are siblings of the container directory,
like anywhere else.

## Targets

Every command takes the same `<target>`:

| Target        | Example                                  |
|---------------|------------------------------------------|
| id            | `7`                                      |
| session name  | `trade-review_claude-file`               |
| branch        | `review_claude-file`                     |
| issue URL     | `https://github.com/o/r/issues/59`       |
| PR URL        | `https://github.com/o/r/pull/61`         |
| repository URL| `https://github.com/o/r`                 |
| plain name    | `feature-123` (a branch to create)       |

A plain name and a branch are the same syntax: an existing environment on
that branch is found; otherwise `open` creates one there.

## Resolution

Order everywhere: **registry → GitHub → git worktrees**. Any number that
arrives through a new URL is recorded on the environment it resolved to.

**Issue URL**

1. Registry holds that issue URL → hit.
2. `gh issue view` for the title and `closedByPullRequestsReferences`.
3. A linked PR URL already in the registry → hit; the issue URL is linked.
4. Branch = `--branch`, else the head of the highest-numbered linked PR (via
   `gh pr view`; a fork PR gives `pr-<n>`), else the title slug.
5. Registry holds that branch in the project → hit, links added.
   Otherwise a git worktree already on the branch → adopt.
6. `attach` errors; `open` creates.

**PR URL**

1. Registry holds that PR URL → hit.
2. `gh pr view` for `headRefName`, `isCrossRepository` and
   `closingIssuesReferences`.
3. Branch = `--branch`, else `headRefName` for a same-repo PR, else `pr-<n>`
   materialised from `refs/pull/<n>/head`.
4. Registry by branch → hit, links added (including the closing issues, each
   skipped if it belongs to another environment). Otherwise adopt a worktree
   on the branch.
5. `attach` errors; `open` creates.

**Repository URL**

1. Locate the repository: the one containing the cwd if its origin matches,
   else `<projects_path>/<repo>`, else clone it.
2. Branch = `--branch`, else the default branch (`origin/HEAD`).
3. Registry by branch → hit. Otherwise adopt the worktree that has it —
   normally the main working tree — else create.

**Plain string**

1. An integer that matches an id → hit.
2. A session name in the registry → hit.
3. A branch in the registry: within the repository of the cwd (or `--repo`).
   With no explicit `--repo`, a unique match anywhere is also a hit; an
   explicit `--repo` scopes the search to that repository, so the same flag
   means the same thing here as it does for `delete`. Several matches
   elsewhere are an error naming them and suggesting `--repo` — never a
   silent third environment on the same branch name.
4. `attach` errors; `open` needs a repository — the cwd's or `--repo` — and
   creates on branch `--branch`, else the string itself. A string that is
   all digits is refused there: it can only be a stale id, never a branch
   worth creating, unless `--branch` says otherwise.

An environment is identified by its branch, so a registry hit never
re-queries GitHub, and a PR whose head differs from the branch in the issue's
worktree gets its own environment. A worktree can only be on one branch.

## Commands

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

`--repo <name|path>` names the repository a **plain-name** target belongs to,
for when you are not standing in it: `we open feature-123 --repo trade`. A
bare name is looked up in `projects_path`; a value containing a separator or
starting with `~` is a path to the repository, which is how repositories
outside `projects_path` are reached. Other target kinds carry their own
repository, so it is ignored there.

**`open` and `attach` are one code path**, differing in a single flag: attach
never creates an environment. Same targets, same resolution, same repair,
same output. Because attach cannot create, the three creation overrides are
rejected there rather than silently ignored.

`--branch`, `--session` and `--wt` only apply when an environment is
created. On a hit, `open` prints a one-line note to stderr saying they were
ignored, so re-running a command from shell history still attaches.

**Repair on open** (and attach) — each step finds before it creates:

- worktree directory missing → `git worktree prune`, re-add at the recorded
  path on the recorded branch;
- tmux session missing → create, tag, start `claude_cmd` in the first window;
- branch renamed inside the worktree → the stored branch is refreshed.

**Adoption.** A live tmux session with the target name is reused only if it
carries `@workenv`. An untagged session of the same name is someone else's,
and `we` refuses rather than taking it over. The refusal says what to do,
which differs by case: creating a new environment, `--session` picks another
name; for an environment that already exists, `--session` cannot help — the
message names the conflict and points at renaming or killing the other
session, or `we delete <id>`.

**delete** resolves through the registry only. It kills the session, removes
the worktree (`--force` when dirty; a directory that is already gone is just
pruned), optionally deletes the branch, and drops the record.
`--keep-worktree` kills the session and keeps everything else. A target that
is not in the registry but names a live `@workenv`-tagged session gets that
session killed.

## Listing

```
ID  PROJECT  SESSION                                       STATE     REFS
 7  trade    trade-review_claude-file                      attached  #59 PR#61
    dir: ~/projects/trade.review_claude-file
 8  trade    trade-dev-overlay-pins-a-stale-mini-internal  detached  #44
    dir: ~/projects/trade.dev-overlay-pins-a-stale-mini-internal (missing)
```

- Rows are two lines: the table row, then a dimmed `dir:` line. `$HOME` is
  abbreviated to `~`; a directory that no longer exists is marked
  `(missing)` — the next `open` recreates it.
- `REFS` renders `#59` / `PR#61`, each an OSC 8 hyperlink to its full URL
  when stdout is a terminal, plain text otherwise. `-` when there are none.
- `STATE` is `attached` / `detached` / `none`, from tmux.
- The environment containing the current directory is marked.
- Colour and hyperlinks are suppressed when stdout is not a terminal or
  `NO_COLOR` is set.
- `-l` and `we show <target>` print the stacked form instead: branch, full
  issue and PR URLs, repository directory, creation time.

## Remote hosts

`--host devbox` runs the same command over ssh with `--no-terminal` and the
creation overrides passed through, parses the `WE_SESSION=` marker, and opens
a local Ghostty running `ssh -t devbox tmux attach-session -t <session>`.
`ls`, `show` and `delete` pass through unchanged. The remote host needs `we`
installed; its path is `remote_we`.

## Configuration

`$XDG_CONFIG_HOME/workenv/config.toml` (default
`~/.config/workenv/config.toml`), all keys optional:

```toml
projects_path = "~/projects"   # where repositories live / get cloned
claude_cmd    = "claude"       # command run in the first tmux window
remote_we     = "we"           # we binary path on remote hosts

# where new worktrees go; see Placement for variables and filters
worktree_path = "{{ .repo_path }}/../{{ .repo }}.{{ .branch | sanitize }}"
```

## Testing

Unit tests drive every flow through the existing scripted `execx.Fake`
runner, asserting exact argv and the persisted registry:

- **state** — round trip, atomic save, id assignment and non-reuse, URL
  canonicalisation and lookup, uniqueness invariants.
- **naming** — session and directory derivation, sanitizing, project from
  origin.
- **we** — each resolution path above; issue and PR converging on one
  environment; adoption of an existing worktree; refusal to adopt an untagged
  session; repair of a missing worktree and session; branch drift; placement
  (default template, a custom `worktree_path`, `--wt` name and path);
  delete semantics.
- **config** — template rendering: variables, the `sanitize` filter, `~`
  expansion, relative results, and a clear error for a template that fails
  to parse or render.
- **cmd** — listing layout, TTY vs piped rendering, `show`.

## Use cases

Worked examples, in the order a day tends to go. Paths assume
`projects_path = ~/projects` and the default `worktree_path`.

### Start work on an issue

```
we open https://github.com/axklim/trade/issues/59
```

`gh` reports the title "Review CLAUDE.md file" and no linked PR, so the
branch is the slug `review-claude-md-file`. The repository is cloned to
`~/projects/trade` if it is not there yet. Result: worktree
`~/projects/trade.review-claude-md-file`, session
`trade-review-claude-md-file` with `claude` running, a Ghostty window
attached, and record id 7 holding the issue URL.

### Pick the work back up from its pull request

```
we open https://github.com/axklim/trade/pull/61
```

`gh` reports head `review-claude-md-file`. The registry already has an
environment on that branch, so this is id 7 again — the PR URL is added to
it, nothing is created, and you land in the same session. (Had the PR come
from a differently named branch, it would be its own environment: a worktree
can only be on one branch.)

### Work on two things at once

```
we open https://github.com/axklim/trade/issues/44
```

A second worktree of the same repository, `~/projects/trade.dev-overlay-…`,
with its own branch and its own session. Both show in `we ls`; the first is
untouched.

### Review a pull request from a fork

```
we open https://github.com/axklim/trade/pull/77
```

`isCrossRepository` is true, so there is no head branch on origin: the branch
is `pr-77`, materialised from `refs/pull/77/head`. Worktree
`~/projects/trade.pr-77`, session `trade-pr-77`.

### Just open a project

```
we open https://github.com/axklim/trade
```

No issue, no PR. The branch is the default branch, and a normal clone already
has it checked out at `~/projects/trade`, so that worktree is adopted rather
than created. Session `trade-main`.

### Start a branch with no issue behind it

```
cd ~/projects/trade && we open spike-latency
we open spike-latency --repo trade          # equivalent, from anywhere
we open spike-latency --repo ~/src/fork     # a repository outside projects_path
```

Branch `spike-latency` off the default branch, worktree
`~/projects/trade.spike-latency`, session `trade-spike-latency`, no refs.

### Come back to something

```
we ls
we attach 7
we attach trade-review-claude-md-file
we attach https://github.com/axklim/trade/issues/59
```

All four reach the same environment. `attach` never creates: a typo is an
error, not a new branch.

### Rename the branch mid-flight

```
cd ~/projects/trade.review-claude-md-file && git branch -m claude-md
we ls
```

The listing shows `claude-md`, and the record is updated — git is the truth
for the branch. The session name and worktree path are unchanged, because
they are stored rather than derived.

### After a reboot

```
we open 7
```

The tmux server is gone, so the session is recreated, tagged, and `claude`
started in its first window. The worktree is untouched, and the branch is
whatever git says it is.

### After the worktree is deleted behind your back

```
we ls        # dir: ~/projects/trade.claude-md (missing)
we open 7
```

`we open` prunes the stale registration and re-adds the worktree at the
recorded path on the recorded branch.

### Put a worktree somewhere else, once

```
we open https://github.com/axklim/trade/issues/59 --wt ~/scratch/claude-md
```

Only the directory changes; the branch and session follow the usual rules.
For a permanent change, set `worktree_path` in the config instead.

### Finish up

```
we delete 7 --delete-branch
```

Kills the session, removes the worktree, deletes the branch, drops the
record. `--force` if the worktree is dirty, `--keep-worktree` to stop after
killing the session.

### Do any of it on another machine

```
we open https://github.com/axklim/trade/issues/59 --host devbox
we ls --host devbox
we delete 7 --host devbox
```

The environment is created on devbox and recorded in devbox's registry; the
local Ghostty attaches to it over ssh. Ids are per host, so `7` there is not
`7` here.
