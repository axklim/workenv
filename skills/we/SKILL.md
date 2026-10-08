---
name: we
description: Use when asked to open, start, run, list, attach to, show or tear down a work environment or claude session for a project, GitHub issue, PR or branch with the `we` (workenv) CLI, including dispatching a task to a new session or making one reachable from another device.
---

# Driving `we`

`we open <target>` gives one task one environment: a git worktree on the right
branch, a tmux session with `claude` in its first window, and a terminal on
it. Run it again with any reference to the same work and it finds that
environment instead of making another. Every command takes the same
`<target>` (id, session name, branch, issue, PR or repository URL, plain name).
`we <command> --help` has the flags; this is the judgment the help cannot make.

## Sentence → command

| Caller says | Command |
|---|---|
| "start on issue 59 of trade" | `we open https://github.com/OWNER/trade/issues/59` |
| "pick up PR 61" | `we open https://github.com/OWNER/trade/pull/61` |
| "open the infra project" (its home, on `main`) | `we open https://github.com/OWNER/infra` |
| "run a session for infra, name it review" | `we open review --repo infra` |
| "work on branch fix-login in infra" | `we open fix-login --repo infra` |
| "back to environment 7 / session infra-review" | `we attach 7`, `we attach infra-review` (`open` works too) |
| "what is running?" | `we ls` |
| "details of 7" | `we show 7` |
| "tear down 7" | `we delete 7` (`--delete-branch` drops the branch too) |
| "clean up finished work" | `we gc --dry-run`, then `we gc` |

A name the caller gives the work is the **branch**, passed as a plain-name
target. The session is then `<project>-<branch>` (sanitized: anything outside
`[A-Za-z0-9_-]` becomes `-`), so `review` in `infra` is session `infra-review`.
Add `--session NAME` only when the caller wants a session named differently,
or `we` reports a session collision.

Issue, PR and repository URLs carry their repository: `we` finds it under
`projects_path` or clones it. A **plain name** does not, so pass `--repo`
unless the cwd is inside that repository: a bare name is looked up in
`projects_path`, a path (`~/src/fork`) reaches a repository anywhere. Ids,
session names and branches already in the registry need no `--repo`.

## Project home versus task branch

`we open https://github.com/OWNER/REPO` is the project's home: the default
branch, the main checkout adopted as the worktree, session `REPO-main`. Use it
when the task is about the project as a whole, not a change to it. Every other
target gets its own branch and worktree beside the checkout
(`~/projects/infra.review`), so tasks never share a working tree.
`we open main --repo infra` is the same home when the owner is not at hand.

## Flags that shape a new session

These apply only when `we` starts the session. On an environment that is
already running, `open` leaves it alone and says on stderr which flags it
ignored. `attach` does not accept them.

- `--branch NAME` — instead of the derived branch: issue title slug, PR head
  (`pr-<n>` for a fork's PR), default branch, or the plain name itself.
- `--session NAME`, `--wt PATH|NAME` — session name, worktree location.
- `--rc` — start claude with Remote Control named after the session, so a
  phone or laptop can pick it up. `remote_control = true` in the config does
  it for every environment.
- `--prompt "TEXT"` — claude's first prompt, typed in as soon as it starts.
  This is how one agent hands work to another; the text arrives verbatim.
- `--no-terminal` — open or switch no terminal; `attach` takes it too.
  **Required when the caller is headless**: from inside tmux, `open` and
  `attach` otherwise switch the caller's own tmux client to the session; from
  a terminal they open a Ghostty window. Bots, scripts and sessions acting for
  someone else always pass it.
- `--host HOST` — run the same command on another machine over ssh and attach
  locally. The host needs `we`; its registry and ids are its own.

A dispatch from a bot therefore looks like:

```
we open https://github.com/OWNER/trade/issues/59 --no-terminal --rc \
  --prompt "Implement issue 59 and open a PR"
```

`open` prints `created environment N` or `found environment N`, then project,
branch, worktree, session and a `WE_SESSION=<session>` line. Read the session
name from there rather than deriving it.

**Versions.** `--rc`, `--prompt`, `remote_control` and `gc` are on `main` and in no
release through 0.1.3; the next release carries them. Everything else here is
in 0.1.3. `we version` says which you have, `we open --help` which flags it
knows.

## Reading back

`we ls` prints one row per environment — id, project, session, `STATE`, refs —
and a `dir:` line. `STATE` comes from tmux, followed by the first pane's
command in parentheses: a shell (`zsh`, `bash`, …) means claude has exited;
anything else, including a version such as `2.1.295`, means it still runs.

- `attached` — a terminal is on the session.
- `detached` — the session is alive, nobody is looking at it.
- `none` — no tmux session; the next `open` or `attach` recreates it.
- `done` — every linked PR is merged or closed and the worktree is gone
  (`"done": true` in `--json`). `we gc` deletes these, killing a live session;
  environments without a PR never are. Without `gh`, `ls` shows no `done` and
  `gc` fails.

`(missing)` after `dir:` means the worktree is gone; the next `open` or
`attach` re-adds it. Both repair; only `open` creates. `*` marks the environment the cwd is in. `we ls -l` and `we show <target>`
print the stacked form with full issue and PR URLs, repository path and
creation time. To read state, prefer `we ls --json`: a JSON array with stable
keys (`id`, `session`, `state`, `worktree_path`, `worktree_missing`, `prs`, …).

The registry (`~/.local/state/workenv/envs.json`) knows: id, project, branch,
session name, worktree and repository path, linked issue and PR URLs, creation
time. The branch is refreshed from git on every `open` and `ls`. It does not
know what claude is doing or whether it still runs inside the session, whether
the branch is pushed, the PR's state, or anything on another host. Ask `gh`,
`git` or the session itself for those.

Ids are never reused, so a stale `we attach 7` fails instead of hitting another
environment. `attach` never creates: a mistyped name is an error. `delete`
kills the session, removes the worktree and drops the record; `--force` for a
dirty worktree, `--keep-worktree` to only kill the session.

## Config

`~/.config/workenv/config.toml`, all keys optional. `projects_path` (where
repositories live and get cloned, `~/projects`); `claude_cmd` (first-window
command, `claude` — `we` appends `--name <session>`, plus `--remote-control
<session>` when Remote Control is on, unless the command passes them itself);
`remote_control` (`false`); `worktree_path` (template for new worktree
locations); `remote_we` (path of `we` on `--host` machines).

## Common mistakes

- Plain name from outside the repository without `--repo`.
- `--rc` or `--prompt` against a running session: nothing happens, by design.
  To reach a running claude, use tmux on the session name from `we show`
  (`send-keys`, `capture-pane`); `we delete <id>` only if it must restart.
- Dispatching without `--no-terminal`: the bot's tmux client jumps to the new
  session, or a Ghostty window opens where nobody is watching.
- Reading `detached` as "claude is idle" or `attached` as "someone is working":
  it is only whether a terminal is attached.
