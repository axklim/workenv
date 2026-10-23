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
      "created_at": "2026-08-16T18:12:03Z",
      "remote_control": true,
      "model": "opus"
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
- **remote_control**, **model**, **effort** — the launch flags (`--rc`,
  `--model`, `--effort`) the session was last started with, omitted when
  unset. They are written whenever `we` starts the session, a flag passed on
  that `open` replacing the recorded value, and applied again whenever repair
  recreates it (see *Repair on open*). `remote_control` records only `--rc`:
  the config's `remote_control` still applies on its own, so turning it off
  reaches every environment. `--prompt` is not recorded.

**Running markers.** Next to the registry, `running/<id>` exists from the
moment `we` starts claude in environment `<id>`'s session until claude exits:
the command typed into the first window ends with `; rm -f <marker>`. A
reboot, a crash or a killed tmux server ends the session without running
that, so a marker with no session left means the environment was
*interrupted* (see *Listing*). `delete` and `gc` remove the marker — a
session they kill was stopped on purpose. A marker is a file rather than a
registry field so that claude's exit needs nothing but `rm`: no `we` on the
pane's `PATH`, and no second writer racing a running `we` for `envs.json`.

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
GitHub, else the `repo_path` basename with any `.git` suffix removed — unless
that repository name has an alias (see *Aliases*), which is then the project
name. Two clones of the same repository therefore share a project name; their
environments stay distinct because sessions and directories must be unique,
and `--session` / `--wt` resolve a collision.

Sanitizing replaces everything outside `[A-Za-z0-9_-]` with `-` and collapses
runs — tmux reserves `:` and `.` in target syntax, and `/` cannot be a path
segment. So branch `feat/static-grid` yields session `trade-feat-static-grid`
and directory `trade.feat-static-grid`.

There is no `we-` prefix and no issue/PR number in any name.

### Aliases

Repository names can be long, and they end up in every session name and
every `--repo`. The `[aliases]` table in the config gives a repository a
short name:

```toml
[aliases]
infra = "simple-dimple-infra"
```

The value is a repository name, as found in `projects_path`; a path is not
accepted. One repository has at most one alias. The alias:

- stands for the repository in `--repo infra`, looked up in the table before
  `projects_path` is searched;
- is a target of its own wherever a target is taken: `we open infra` and
  `we attach infra` open the repository like its URL (see *Resolution*),
  and `we show infra` / `we delete infra` find its environment on the
  default branch, as they do for the URL (see *delete*);
- is the project name of every environment created in that repository,
  whatever target created it: session `infra-review`, `PROJECT` column
  `infra`, `project` in `--json`.

The worktree path keeps the real repository name (`.repo`, `.project`); a
template that wants the alias uses `.alias`. Since names are stored, not
derived, an environment created before its alias keeps its names — adding,
changing or removing an alias never renames anything.

With `--host`, the target and `--repo` travel to the remote host verbatim,
so aliases resolve there with the remote host's own table — like
`remote_control`, the config of the `we` doing the work applies.

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
| `.project`     | repository name (see *Naming*), never the alias |
| `.alias`       | the repository's alias, else `.project`      |
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

**Trusting a clone.** claude starts in a directory it has not seen on its
"Is this a project you trust?" dialog and waits there — with `--rc`, Remote
Control is not up until it is answered, and `--prompt` waits too. A trusted
parent directory does not answer it for a new repository under it. With
`trust_clones = true`, `open` answers it for the repository it just cloned,
right after the clone and so before any session starts: it sets
`projects["<repository path>"].hasTrustDialogAccepted = true` in claude's
global config (`$CLAUDE_CONFIG_DIR/.claude.json`, else `~/.claude.json`),
the path symlink-resolved, since that is how claude sees its working
directory. claude keys a git worktree by its repository, so every worktree
of the clone is covered by the same entry and adding one writes nothing.

claude rewrites that file whole, so `we` reads it, adds the one key, and
renames a temp file over it, keeping the file's permissions (and a symlinked
file's link); every other value is carried over unchanged. A missing or
empty file starts from `{}`. A key already present is left as it is, `false`
included — that is an answer too. Only a clone is trusted: a repository
already on disk was the user's to answer for. The setting is off by default
because it answers a safety prompt on the user's behalf. A file that cannot
be read or parsed is not overwritten and does not fail `open`: it prints a
one-line note on stderr and the session starts on the dialog.

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
| alias         | `infra` (see *Aliases*)                  |

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
4. With no `--repo`, an alias opens its repository: branch `--branch`, else
   the default branch, then registry by branch, adoption or creation — as
   for a repository URL, but the repository is never cloned. With `--repo`
   the string is a branch, alias or not.
5. `attach` errors; `open` needs a repository — the cwd's or `--repo` — and
   creates on branch `--branch`, else the string itself. A string that is
   all digits is refused there: it can only be a stale id, never a branch
   worth creating, unless `--branch` says otherwise.

An environment is identified by its branch, so a registry hit never
re-queries GitHub, and a PR whose head differs from the branch in the issue's
worktree gets its own environment. A worktree can only be on one branch.

## Commands

```
we open   <target> [--repo R] [--branch B] [--session S] [--wt W]
                   [--rc] [--model M] [--effort E] [--prompt TEXT]
                   [--host H] [--no-terminal]
we new    <repo | issue URL | name> [name] [--session S] [--wt W]
                   [--rc] [--model M] [--effort E] [--prompt TEXT]
                   [--host H] [--no-terminal]
we attach <target> [--repo R] [--host H] [--no-terminal]
we ls     [-l] [--json] [--host H]
we show   <target> [--host H]
we delete [<target>] [--repo R] [--host H]
                   [--force] [--delete-branch] [--keep-worktree]
we gc     [--dry-run] [--delete-branch] [--host H]
```
`ls` is an alias of `list`; `rm` and `down` of `delete`.

`--repo <name|path>` names the repository a **plain-name** target belongs to,
for when you are not standing in it: `we open feature-123 --repo trade`. A
bare name is an alias, else looked up in `projects_path`; a value containing a separator or
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

**new** is `open` from a sentence, for a caller that has a repository and
maybe a name rather than a target ([issue
#42](https://github.com/axklim/workenv/issues/42)):

```
we new <issue URL> [name]   as `we open <issue URL>`; a name is its --branch
we new <repo> <name>        branch <name> in repo
we new <repo>               branch feature-N in repo
we new <name>               branch <name> in the cwd's repository
we new . [name]             the cwd's repository, explicitly
```

A repository is an alias, a directory under `projects_path`, a path (as for
`--repo`), `.` for the cwd's, or a repository URL found where `open` would
find it — never cloned here: `new` says to `we open` it instead. With two
arguments the first is always the repository. One argument that is not a URL
is a repository when it is one *and* not the cwd's own repository; otherwise
it is a branch name in the cwd's repository, and outside any repository that
is an error. So `we new trade` from inside `trade` is branch `trade`, and a
branch such as `feat/login` stays a branch unless it is a path to a
repository. A PR URL works like an issue URL. With no name the branch is
`feature-N`, the smallest N ≥ 1 for which that repository has neither a
branch `feature-N` (local or `origin/`, read with `git for-each-ref`) nor an
environment on it.

Before the usual `open` output, `new` prints the reading it took, one line
each: `target: <owner>/<repo>#<n>` for a URL, else `repo: <name> (<how>)`
with *how* one of `cwd`, `alias <a>`, `projects_path`, `path`; then
`branch: <name> (given)` or `branch: feature-N (next free)`. Then it is
`open`: same session flags and creation overrides (`--branch` and `--repo`
are its arguments), same find-or-create, same output including
`WE_SESSION=`. The branch is looked up, adopted or created in that
repository exactly as `open <branch> --repo <repo>` would, except that it is
never read as an id or a session name. `we open <alias>` keeps meaning the
project home; `new` is where a bare repository means a fresh branch.

**No workers.** The config's `[no_workers]` table (see *Configuration*)
lists repositories, by name or alias, that take no work environments. `new`
refuses any of them; `open` refuses only when it would *create* an
environment there — adoption included — so existing environments there can
still be opened, attached and deleted. The refusal is `we: <key> takes no
work environments: <hint>`, non-zero exit, before anything is created. The
repository's name is the project name before aliasing (see *Naming*), so
every clone of a listed repository is covered.

**Repair on open** (and attach) — each step finds before it creates:

- worktree directory missing → `git worktree prune`, re-add at the recorded
  path on the recorded branch;
- tmux session missing → create, tag, start `claude_cmd` in the first
  window with `--name <session>` appended, so Claude Code's own session
  name matches the tmux session (a `claude_cmd` that already passes `-n`
  or `--name` keeps its own); with Remote Control on, `--remote-control
  <session>` is appended too, so the name the session is reachable under
  from another device is the same one (same rule: a `claude_cmd` that
  passes `--remote-control` itself keeps its own); with `--model` or
  `--effort`, `--model <name>` / `--effort <level>` are appended,
  shell-quoted, replacing the same flag in `claude_cmd`; with `--prompt`, the text goes last,
  shell-quoted, as claude's positional first prompt. Remote Control, model
  and effort default to the recorded ones (see *State*), so a recreated
  session comes back as it was started. For an environment that already
  existed — a session recreated after a reboot — claude also gets
  `--continue`, so it picks up the conversation last run in the worktree
  (Claude Code keys conversations by directory), and the same command without
  it follows after `||`, since `claude --continue` exits with an error where
  there is no conversation to continue. A `claude_cmd` that passes
  `--continue`/`-c` or `--resume`/`-r` itself keeps its own. Finally `; rm -f
  <running marker>` is appended (see *State*). An environment adopted onto
  a shared checkout, such as the project home, continues whatever
  conversation was last run in that checkout;
- branch renamed inside the worktree → the stored branch is refreshed.

**New branches start from a fresh default branch.** A branch that exists
neither locally nor on origin is cut from the default branch, and only that
path fetches: `git fetch origin +refs/heads/<default>:refs/remotes/origin/<default>`
runs right before `git worktree add`, so the start point is
`origin/<default>` as origin has it now, not as the last fetch left it.
Existing local branches and PR heads are not fetched here, and a repository
without an `origin` remote starts from its local default branch. A failed
fetch never fails the open: `open` warns in one line on stderr and branches
from the local ref. Either way it prints a `base:` line (start ref and short
commit) after `branch:`, so a stale base is visible.

**Remote Control** is off unless `remote_control = true` in the config or
`we open --rc` turns it on for one open. Either only matters when repair
starts the session: a session that is already live keeps the claude it
runs. A session started with `--rc` records it, so recreating that session
keeps Remote Control on. `attach` does not define `--rc`, like the creation
overrides.

With `--rc`, `open` and `new` then wait for Remote Control to come up and
print `rc: <url>` as the last line of stdout — what a caller handing the
session to a human reports. They poll the first pane every 2 seconds with
`tmux capture-pane -p -J -S -` (history included, so a live session that is
already up answers at once) for Claude Code's `/remote-control is active`
line and the `https://claude.ai/code/session_…` link on it or the line after.
After 2 minutes without one they print `rc: timeout`, the last 8 non-blank
screen lines on stderr, and exit non-zero. This also applies to a live
session: one that was not started with Remote Control times out. Nothing
answers dialogs on the way; with `trust_clones = true` none stands in it.

**Initial prompt.** `we open --prompt "<text>"` hands claude its first
prompt, so a session started for another agent starts working at once. Like
`--rc` it applies whenever repair starts the session, on creation and on
repair alike. A live session keeps the claude it runs and is not typed into;
since a dropped task should not go unnoticed, `open` then prints a one-line
note to stderr that `--prompt` was ignored. `attach` does not define it.

**Model and effort.** `we open --model <name>` and `--effort <level>` pick
claude's model and reasoning effort for one environment, where `claude_cmd`
is global. Like `--prompt` they apply only when repair starts the session;
on a live one they are named in the same stderr note as an ignored
`--prompt` (one line, e.g. `--model, --prompt ignored`). Values pass through
verbatim — `we` does not validate them, claude does. Unlike `--name` and
`--remote-control`, they override the config: a `claude_cmd` that passes
`--model` or `--effort` has that flag and its value dropped, since the flag
typed for one environment is the more specific request. Both are recorded
with the session they start, and a later `open` that starts the session
with a different value replaces it; one ignored by a live session is not
recorded. `attach` does not define either.

**Adoption.** A live tmux session with the target name is reused only if it
carries `@workenv`. An untagged session of the same name is someone else's,
and `we` refuses rather than taking it over. The refusal says what to do,
which differs by case: creating a new environment, `--session` picks another
name; for an environment that already exists, `--session` cannot help — the
message names the conflict and points at renaming or killing the other
session, or `we delete <id>`.

**delete** resolves through the registry and local git only — never GitHub,
never a clone. A repository URL or an alias means the environment on that
repository's default branch (`origin/HEAD` read locally), found by branch in
the registry; a repository that is not on disk has no environment. It kills
the session, removes the worktree (`--force` when dirty; a directory that
is already gone is just pruned), optionally deletes the branch (one that
is already gone counts as deleted — the usual end of a PR merged with branch
deletion; only a branch that exists and cannot be deleted fails), and drops
the record.
`--keep-worktree` kills the session and keeps everything else. A target that
is not in the registry but names a live `@workenv`-tagged session gets that
session killed.

With no target, `delete` means the environment whose worktree contains the
cwd — the one `ls` marks — the innermost when worktrees nest; outside every
environment it is an error. The session is killed **last**, after the
worktree, the branch and the record are gone, and only then: `delete` may run
inside that very session (an agent removing itself) and dies with it. A
failed teardown therefore leaves the session running, and a kill that fails
after the record is gone leaves a tagged stray that `delete <session>` still
kills.

**gc** retires finished work. An environment is **finished** when it has at
least one PR, every one of them is merged or closed, and its worktree is
missing. A missing worktree alone stays recoverable — the next `open`
re-adds it — and an environment without a PR, such as a project home on
`main`, is never finished. gc asks `gh pr view <n> -R <owner>/<repo> --json
state` once per PR of each environment whose worktree is missing, then
deletes every finished one exactly like `delete` (the stale worktree pruned,
the branch deleted with `--delete-branch`, the records saved, and only then
each tagged live session killed). `--dry-run` prints what would go and changes nothing.
GitHub is asked about every candidate before anything is torn down, so when
`gh` is missing, offline or unauthenticated, gc fails naming the
environment and deletes nothing.

## Listing

```
ID  PROJECT  SESSION                                       STATE              REFS
 7  trade    trade-review_claude-file                      attached (claude)  #59 PR#61
    dir: ~/projects/trade.review_claude-file
 8  trade    trade-dev-overlay-pins-a-stale-mini-internal  detached (zsh)     #44
    dir: ~/projects/trade.dev-overlay-pins-a-stale-mini-internal (missing)
```

- Rows are two lines: the table row, then a dimmed `dir:` line. `$HOME` is
  abbreviated to `~`; a directory that no longer exists is marked
  `(missing)` — the next `open` recreates it.
- `REFS` renders `#59` / `PR#61`, each an OSC 8 hyperlink to its full URL
  when stdout is a terminal, plain text otherwise. `-` when there are none.
- `STATE` is `attached` / `detached` / `none`, from tmux. A live session
  adds its first pane's `#{pane_current_command}` in parentheses — the pane
  claude was started in — so `detached (claude)` is still working and
  `detached (zsh)` has finished or crashed. The name is shown as tmux
  reports it: claude from the native installer is named after its version,
  `detached (2.1.295)`. For the same reason "claude is running" is never
  decided by comparing with the word `claude`: a shell (`zsh`, `bash`, `sh`,
  `fish`, `login`) means claude has exited, anything else means it runs.
- An **interrupted** environment reads `interrupted` in `STATE` instead of
  `none`: it has no session, but its running marker (see *State*) is still
  there, so the session ended without claude exiting — a reboot, a crash, a
  killed tmux server. An environment whose claude exited reads `none`, so
  after a reboot `interrupted` names exactly the ones to bring back:
  `we ls --json`, then `we open <id> --no-terminal` for each.
- A finished environment (see *gc*) reads `done` in `STATE` instead of
  `none`, and `done, detached (zsh)` while its session still lives. Deciding
  that takes the same `gh` calls as gc, made only for rows whose worktree is
  missing and which have a PR. `ls` must work without GitHub, so when `gh`
  fails the row is simply not `done`, and the remaining lookups are skipped
  rather than failing one by one. `show` and `-l` follow the same rule.
- The environment containing the current directory is marked.
- Colour and hyperlinks are suppressed when stdout is not a terminal or
  `NO_COLOR` is set.
- `-l` and `we show <target>` (resolved like *delete*) print the stacked
  form instead: branch, full issue and PR URLs, repository directory,
  creation time.
- `--json` prints the stacked form's facts for an agent to read: a JSON array,
  one object per environment, `[]` when there are none. Keys are stable:
  `id`, `project`, `branch`, `session`, `state` (without the command),
  `pane_command` (`""` with no session), `claude_running` (the rule above;
  `false` with no session), `worktree_path`, `worktree_missing`, `done`
  (finished, the rule above; `state` keeps the tmux value), `interrupted`
  (the rule above; `state` stays `none`), `repo_path`, `issues`, `prs` (both always arrays),
  `created_at` (RFC 3339). Paths are absolute, not `~`-abbreviated. `-l` has
  no effect with it.

## Remote hosts

`--host devbox` runs the same command over ssh with `--no-terminal` and the
creation overrides, `--rc`, `--model`, `--effort` and `--prompt` passed
through — model, effort and prompt shell-quoted, since ssh joins its
arguments into one remote command line —
parses the `WE_SESSION=` marker, and opens a local Ghostty running `ssh -t
devbox tmux attach-session -t <session>`. `remote_control` and
`trust_clones` are read from the remote host's config, since that is the
`we` starting claude and cloning.
`new` passes through like `open`, its arguments verbatim, so the reading is
the remote host's. `ls` (with `-l` and `--json`), `show`, `delete` (with or
without a target — without one, the remote cwd is rarely in an environment)
and `gc` pass through unchanged. The remote host needs `we`
installed; its path is `remote_we`.

## Configuration

`$XDG_CONFIG_HOME/workenv/config.toml` (default
`~/.config/workenv/config.toml`), all keys optional:

```toml
projects_path = "~/projects"   # where repositories live / get cloned
claude_cmd    = "claude"       # command run in the first tmux window,
                               # with --name <session> appended
remote_control = false         # also append --remote-control <session>;
                               # `we open --rc` does it for one open
trust_clones  = false          # pre-accept claude's trust dialog for a
                               # repository open clones; see Placement
remote_we     = "we"           # we binary path on remote hosts

# where new worktrees go; see Placement for variables and filters
worktree_path = "{{ .repo_path }}/../{{ .repo }}.{{ .branch | sanitize }}"

[aliases]                      # short names for repositories; see Aliases
infra = "simple-dimple-infra"

[no_workers]                   # repositories that take no environments;
claudestrophobic = "its sessions are chats: use /chat"   # see new
```

The parser is line-based, not a TOML library: `key = "value"` lines, `#`
comment lines, and the `[aliases]` and `[no_workers]` table headers. As in
TOML, a table runs to the next header, so top-level keys come before it.
Alias and `no_workers` keys keep to TOML's bare-key characters
`[A-Za-z0-9_-]`; quoted keys, inline tables and other tables are rejected.
A `no_workers` key is a repository name or an alias, its value the hint the
refusal ends with (may be empty).

## Testing

Unit tests drive every flow through the existing scripted `execx.Fake`
runner, asserting exact argv and the persisted registry:

- **state** — round trip, atomic save, id assignment and non-reuse, URL
  canonicalisation and lookup, uniqueness invariants.
- **naming** — session and directory derivation, sanitizing, project from
  origin.
- **we** — each resolution path above; claude running versus a shell; issue and PR converging on one
  environment; adoption of an existing worktree; refusal to adopt an untagged
  session; repair of a missing worktree and session, with Remote Control
  from the config or `--rc`, with an initial prompt and with a model and
  effort; launch flags recorded on start, applied again on a recreated
  session and replaced by a later flag, but not by one a live session
  ignored; `--continue` with its fresh fallback; the running marker left on
  start and removed by delete, and `ls` marking `interrupted`; a prompt,
  model and effort ignored by a live session; branch drift; placement
  (default template, a custom `worktree_path`, `--wt` name and path);
  delete semantics; aliases in `--repo` and as a target of open, attach,
  show and delete, naming a new environment but leaving an existing one's
  names and the worktree path alone; show and delete by repository URL
  without GitHub or a clone; gc collecting only finished environments,
  `--dry-run`, failing without `gh` before touching anything, and `ls` marking `done`
  and staying usable without `gh`; a clone trusted in claude's config before
  its session starts, only with `trust_clones`, only for a clone, and a
  config that cannot be parsed reported rather than failing `open`; `new`'s
  reading of every argument form, `feature-N` skipping local, origin and
  registry branches, a name never read as an id, and its errors; the
  `no_workers` refusal by name and alias, for `new` and a creating `open`,
  and an existing environment there still opening; `delete` of the current
  (innermost) environment and its error outside one; the session killed
  after the record is saved, by `delete` and `gc`; the Remote Control link
  read from the pane, and the timeout returning the last screen.
- **claudecfg** — the config path, adding the trust key to a missing, empty
  or populated file with every other value kept, leaving an existing answer
  alone, refusing a malformed file without rewriting it, and a symlinked file.
- **config** — template rendering: variables, the `sanitize` filter, `~`
  expansion, relative results, and a clear error for a template that fails
  to parse or render; the `[aliases]` and `[no_workers]` tables and their
  rejected forms.
- **gitx** — local and origin branch names in one set.
- **tmuxx** — the first pane's command per session; capturing a pane's
  history by bare name.
- **cmd** — listing layout, TTY vs piped rendering, `show`, the `--json`
  keys and the empty array, the `done` and `interrupted` states, `gc` output and `--host`;
  `new`'s flags and reading printed first, `rc:` as the last line or
  `rc: timeout` with an error, `new` and target-less `delete` over `--host`.

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

### Use a short name for a long repository

```toml
[aliases]
infra = "simple-dimple-infra"
```

```
we open review --repo infra
we open infra
```

The first is branch `review` in `~/projects/simple-dimple-infra`: session
`infra-review`, worktree `~/projects/simple-dimple-infra.review`. The second
is the project home, session `infra-main`. Environments made before the alias
keep their `simple-dimple-infra-…` names.

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
started in its first window with `--continue`, picking up the conversation
it was in, and with the `--rc`, `--model` and `--effort` the environment was
started with. The worktree is untouched, and the branch is whatever git says
it is. Before that, `we ls` shows the environments whose claude was still
running at the reboot as `interrupted`, and the ones that had finished as
`none`.

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

### Clear out merged work

```
we ls            # 7  trade  trade-review-claude-md-file  done  #59 PR#61
we gc --dry-run  # would delete environment 7 (session trade-review-claude-md-file)
we gc --delete-branch
```

PR#61 is merged and the worktree was removed after it, so environment 7 is
finished: gc drops its record and its branch. An environment whose PR is
still open, or whose worktree is still there, stays.

### Do any of it on another machine

```
we open https://github.com/axklim/trade/issues/59 --host devbox
we ls --host devbox
we delete 7 --host devbox
```

The environment is created on devbox and recorded in devbox's registry; the
local Ghostty attaches to it over ssh. Ids are per host, so `7` there is not
`7` here.
