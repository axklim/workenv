---
name: we
description: Use when asked to open, start, run, list, attach to, show or tear down a work environment or claude session for a project, GitHub issue, PR or branch with the `we` (workenv) CLI, including dispatching a task to a new session or making one reachable from another device. Invoked as /we <repo | alias | issue URL | .> [name] it starts a worker session and reports its Remote Control link.
argument-hint: <repo | alias | issue URL | .> [name]
---

# Driving `we`

`we open <target>` gives one task one environment: a git worktree on the right
branch, a tmux session with `claude` in its first window, and a terminal on
it. Run it again with any reference to the same work and it finds that
environment instead of making another. `we new` is the same for a fresh
branch, from a repository and maybe a name. Every other command takes the same
`<target>` (id, session name, branch, issue, PR or repository URL, plain name).
`we <command> --help` has the flags; this is the judgment the help cannot make.

## `/we <repo | alias | issue URL | .> [name]`

Run exactly this, with the arguments as given:

```
we new $ARGUMENTS --no-terminal --rc
```

Then reply with the session name (the `WE_SESSION=` line) and the URL from
the last line, `rc: <url>`, and nothing else. `we` decides everything: which
repository, which branch (`feature-N` when no name is given), find or create.
Do not pick a branch, add `--repo`, or rephrase the arguments.

- No arguments: ask what to open; do not guess.
- Non-zero exit: report its stderr as is. `rc: timeout` comes with the last
  screen lines on stderr; report them. A `takes no work environments` refusal
  is final, its hint says what to use instead.
- The reading lines `we new` prints first (`repo: …`, `branch: …`) are there
  if the caller asks why; do not repeat them unasked.

## Sentence → command

| Caller says | Command |
|---|---|
| "start on issue 59 of trade" | `we new https://github.com/OWNER/trade/issues/59` |
| "a session for infra, name it review" | `we new infra review` |
| "a new session in infra" (no name) | `we new infra` (branch `feature-N`) |
| "a new branch here" | `we new .` or `we new <name>` |
| "pick up PR 61" | `we open https://github.com/OWNER/trade/pull/61` |
| "open the infra project" (its home, on `main`) | `we open infra` or `we open https://github.com/OWNER/infra` |
| "back to environment 7 / session infra-review" | `we attach 7`, `we attach infra-review` (`open` works too) |
| "what is running?" | `we ls` (`/we-list`) |
| "details of 7" | `we show 7` |
| "tear down 7", "remove this session" | `we delete 7`, `we delete` (`/we-remove`) |
| "clean up finished work" | `we gc --dry-run`, then `we gc` |

Add `--no-terminal` to every `new`, `open` and `attach` run for someone else.

`we new <repo> <name>`: the repository is an alias, a directory in
`projects_path`, a path, or `.` for the cwd's; a URL of a repository already on
disk works too. One argument that is a repository other than the cwd's own
means a fresh `feature-N` there; anything else is a branch in the cwd's
repository. A name is the **branch**; the session is then `<project>-<branch>`
(sanitized: anything outside `[A-Za-z0-9_-]` becomes `-`), so `review` in
`infra` is session `infra-review`. Read the real name from `WE_SESSION=`
rather than deriving it. Add `--session NAME` only when the caller wants a
session named differently, or `we` reports a session collision.

`we open <name>` takes a plain name too, but there it is first an id, a
session or a known branch, and needs `--repo` outside the repository; prefer
`we new` to start something. An `[aliases]` table in the config
(`infra = "simple-dimple-infra"`) lets `infra` stand for the repository,
named `infra-…`. A `[no_workers]` table lists repositories where `new`, and
an `open` that would create, refuse.

## Project home versus task branch

`we open https://github.com/OWNER/REPO` (or `we open <alias>`) is the
project's home: the default branch, the main checkout adopted as the worktree,
session `REPO-main`. Use it when the task is about the project as a whole, not
a change to it. `we new` always gets its own branch and worktree beside the
checkout (`~/projects/infra.review`), so tasks never share a working tree.

## Flags that shape a new session

`new` and `open` take these. They apply only when `we` starts the session. On
an environment that is already running, `we` leaves it alone and says on
stderr which flags it ignored. `attach` does not accept them. `--rc`,
`--model` and `--effort` are recorded when they start a session, and a session
`open` or `attach` recreates later starts with them again and with `claude
--continue`, so it resumes its conversation; `--prompt` is not recorded.

- `--session NAME`, `--wt PATH|NAME` — session name, worktree location.
  `open` also takes `--branch NAME` and `--repo R`; `new`'s arguments say
  those.
- `--rc` — start claude with Remote Control named after the session, so a
  phone or laptop can pick it up, then wait for it and print `rc: <url>` as
  the last line (`rc: timeout` and a non-zero exit after 2 minutes). A
  running session answers at once if it has Remote Control, else times out.
  `remote_control = true` in the config starts every session with it, but
  only `--rc` prints the link.
- `--prompt "TEXT"` — claude's first prompt, typed in as soon as it starts.
  This is how one agent hands work to another; the text arrives verbatim.
- `--model NAME`, `--effort LEVEL` — claude's model and effort for this
  environment, passed through verbatim; they override `claude_cmd`'s own.
- `--no-terminal` — open or switch no terminal; `attach` takes it too.
  **Required when the caller is headless**: from inside tmux, `open` and
  `attach` otherwise switch the caller's own tmux client to the session; from
  a terminal they open a Ghostty window. Bots, scripts and sessions acting for
  someone else always pass it.
- `--host HOST` — run the same command on another machine over ssh and attach
  locally. The host needs `we`; its registry and ids are its own.

A dispatch from a bot therefore looks like:

```
we new https://github.com/OWNER/trade/issues/59 --no-terminal --rc \
  --prompt "Implement issue 59 and open a PR"
```

`open` prints `created environment N` or `found environment N`, then project,
branch, worktree, session and a `WE_SESSION=<session>` line; `new` prints its
reading before that, and `--rc` adds `rc: <url>` after it. A new branch cut
from the default branch starts from a freshly fetched `origin/<default>`;
`open` then also prints `base: origin/<default> <sha>`, and warns on stderr if
it was offline.

**Versions.** `--rc`, `--prompt`, `remote_control`, `ls --json`, the claude
state in `ls`, `gc` and `[aliases]` arrived in 0.2.0; `--model` and
`--effort` arrived in 0.2.4; recorded launch flags, `--continue` on a
recreated session and the `interrupted` state arrive after 0.2.4; `we new`,
`[no_workers]`, `delete` without a target and the `rc:` line arrive after
that. `we version` says which you have, `we new --help` whether it has `new`.

## Reading back

`we ls` prints one row per environment — id, project, session, `STATE`, refs —
and a `dir:` line. `STATE` comes from tmux, followed by the first pane's
command in parentheses: a shell (`zsh`, `bash`, …) means claude has exited;
anything else, including a version such as `2.1.295`, means it still runs.

- `attached` — a terminal is on the session.
- `detached` — the session is alive, nobody is looking at it.
- `none` — no tmux session; the next `open` or `attach` recreates it.
- `interrupted` — no tmux session, but claude was still running when it
  ended: a reboot, a crash, a killed tmux server (`"interrupted": true` in
  `--json`, `state` stays `none`). After a reboot, `we open <id>
  --no-terminal` each of these and nothing else.
- `done` — every linked PR is merged or closed and the worktree is gone
  (`"done": true` in `--json`). `we gc` deletes these, killing a live session;
  environments without a PR never are. Without `gh`, `ls` shows no `done` and
  `gc` fails.

`(missing)` after `dir:` means the worktree is gone; the next `open` or
`attach` re-adds it. Both repair; only `open` and `new` create. `*` marks the
environment the cwd is in. `we ls -l` and `we show <target>` print the stacked
form with full issue and PR URLs, repository path and creation time. To read
state, prefer `we ls --json`: a JSON array with stable keys (`id`, `session`,
`state`, `worktree_path`, `worktree_missing`, `prs`, …).

The registry (`~/.local/state/workenv/envs.json`) knows: id, project, branch,
session name, worktree and repository path, linked issue and PR URLs, creation
time, and the `--rc`, `--model` and `--effort` the session was started with.
The branch is refreshed from git on every `open` and `ls`. It does not know
what claude is doing or whether it still runs inside the session, whether the
branch is pushed, the PR's state, or anything on another host. Ask `gh`, `git`
or the session itself for those.

Ids are never reused, so a stale `we attach 7` fails instead of hitting another
environment. `attach` never creates: a mistyped name is an error. `delete`
removes the worktree, drops the record and kills the session last; with no
target it is the environment the cwd is in, so a session can remove itself.
`--force` for a dirty worktree, `--keep-worktree` to only kill the session.

## Config

`~/.config/workenv/config.toml`, all keys optional. `projects_path` (where
repositories live and get cloned, `~/projects`); `claude_cmd` (first-window
command, `claude` — `we` appends `--name <session>`, plus `--remote-control
<session>` when Remote Control is on, unless the command passes them itself);
`remote_control` (`false`); `trust_clones` (`false`; answers claude's trust
dialog for a repository `open` clones); `worktree_path` (template for new
worktree locations); `remote_we` (path of `we` on `--host` machines);
`[aliases]` and `[no_workers]` tables.

## Common mistakes

- Plain name to `open` from outside the repository without `--repo`; `we new
  <repo> <name>` says it in one go.
- `--rc`, `--prompt`, `--model` or `--effort` against a running session:
  nothing happens, by design (`--rc` still waits for a link it may not have).
  To reach a running claude, use tmux on the session name from `we show`
  (`send-keys`, `capture-pane`); `we delete <id>` only if it must restart.
- Dispatching without `--no-terminal`: the bot's tmux client jumps to the new
  session, or a Ghostty window opens where nobody is watching.
- Reading `detached` as "claude is idle" or `attached` as "someone is working":
  it is only whether a terminal is attached.
