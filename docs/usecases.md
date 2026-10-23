# Use cases

Worked examples, in the order a day tends to go. Paths assume
`projects_path = ~/projects` and the default `worktree_path`; see the
[README](../README.md) for both.

- [Starting work](#starting-work) — from an issue, its PR, a fork's PR, a bare
  repository, or nothing at all; `we new` for a fresh branch
- [Coming back](#coming-back) — finding an environment again, and renaming its
  branch
- [When something breaks](#when-something-breaks) — a reboot, interrupted
  environments, a deleted worktree
- [Placement and cleanup](#placement-and-cleanup) — one-off paths, tearing down
- [On another machine](#on-another-machine) — `--host`
- [From another device](#from-another-device) — `--rc`
- [Handing work to a new session](#handing-work-to-a-new-session) — `--prompt`
- [Picking the model](#picking-the-model) — `--model`, `--effort`

## Starting work

### Start work on an issue

```
we open https://github.com/axklim/trade/issues/59
```

`gh` reports the title "Review CLAUDE.md file" and no linked PR, so the branch
is the slug `review-claude-md-file`. The repository is cloned to
`~/projects/trade` if it is not there yet. Result: worktree
`~/projects/trade.review-claude-md-file`, session `trade-review-claude-md-file`
with `claude` running under that same session name, a Ghostty window attached,
and record id 7 holding the issue URL.

### Pick the work back up from its pull request

```
we open https://github.com/axklim/trade/pull/61
```

`gh` reports head `review-claude-md-file`. The registry already has an
environment on that branch, so this is id 7 again — the PR URL is added to it,
nothing is created, and you land in the same session.

Had the PR come from a differently named branch, it would be its own
environment: a worktree can only be on one branch.

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
# ~/.config/workenv/config.toml
[aliases]
infra = "simple-dimple-infra"
```

```
we open review --repo infra
we open infra
```

The first is branch `review` in `~/projects/simple-dimple-infra`: session
`infra-review`, worktree `~/projects/simple-dimple-infra.review` (the path
keeps the repository name; a `worktree_path` template can use `{{ .alias }}`).
The second is the project home on the default branch, session `infra-main`;
`we attach infra`, `we show infra` and `we delete infra` reach it too.
Environments made before the alias keep their `simple-dimple-infra-…` names.

### Start a branch with no issue behind it

```
cd ~/projects/trade && we open spike-latency
we open spike-latency --repo trade          # equivalent, from anywhere
we open spike-latency --repo ~/src/fork     # a repository outside projects_path
```

Branch `spike-latency` off the default branch, worktree
`~/projects/trade.spike-latency`, session `trade-spike-latency`, no refs.
`origin/main` is fetched first, so the branch starts from what origin has now,
and `open` prints the start point (`base: origin/main 1a2b3c4`). Offline, it
warns and branches from the last fetch instead.

### A fresh branch from a sentence

```
we new infra review        # branch review in simple-dimple-infra (alias infra)
we new infra               # branch feature-2 there: feature-1 is taken
cd ~/projects/trade && we new spike-latency   # branch spike-latency in trade
we new . && we new . bugfix                   # the cwd's repository, explicitly
we new https://github.com/axklim/trade/issues/59
```

`new` is `open` for a caller that has a repository and maybe a name. It
first prints how it read them, then the usual `open` output:

```
repo: simple-dimple-infra (alias infra)
branch: feature-2 (next free)
created environment 12
...
```

With no name the branch is `feature-N`, the smallest N no branch (local or on
origin) and no environment has. One argument is a repository when it is an
alias, a directory in `projects_path` or a path, and not the repository you
are standing in; otherwise it is a branch name there, so `we new trade` from
inside `trade` makes branch `trade`. `we open infra` still means the project
home on `main`. Running the same `we new infra review` again finds the
environment instead of making another.

### A repository that takes no work environments

```toml
[no_workers]
claudestrophobic = "its sessions are chats: use /chat"
```

```
$ we new claudestrophobic
we: claudestrophobic takes no work environments: its sessions are chats: use /chat
```

`new` refuses such a repository, and `open` refuses whenever it would create
an environment there. An environment that already exists in it still opens,
attaches and deletes.

## Coming back

### Come back to something

```
we ls
we attach 7
we attach trade-review-claude-md-file
we attach https://github.com/axklim/trade/issues/59
```

All four reach the same environment. `attach` never creates: a typo is an
error, not a new branch.

`STATE` shows what runs in the session's first pane: `detached (claude)` — or
a version such as `detached (2.1.295)` for a native install — is still
working, `detached (zsh)` means claude has exited.

`we ls --json` prints the same environments as a JSON array, for a script or
an agent to read instead of parsing the table; `claude_running` there answers
the same question.

### Rename the branch mid-flight

```
cd ~/projects/trade.review-claude-md-file && git branch -m claude-md
we ls
```

The listing shows `claude-md`, and the record is updated — git is the truth for
the branch. The session name and worktree path are unchanged, because they are
stored rather than derived.

## When something breaks

### After a reboot

```
we open 7
```

The tmux server is gone, so the session is recreated, tagged, and `claude`
started in its first window with `--continue`: it picks up the conversation
that was running in the worktree, or starts fresh if there is none. It comes
back with the `--rc`, `--model` and `--effort` the environment was started
with, since the registry records them; a flag passed to this `open` replaces
the recorded one. `we attach 7` does the same. The worktree is untouched, and
the branch is whatever git says it is.

A worktree that is the project home, shared with work done by hand, continues
whatever conversation was last run in that checkout.

### Bring back what the reboot interrupted

```
we ls        # 7  trade  trade-review-claude-md-file  interrupted  #59 PR#61
we ls --json # "state": "none", "interrupted": true
we open 7 --no-terminal
```

`interrupted` replaces `none` for an environment whose session ended while
claude was still running in it: a reboot, a crash, a killed tmux server.
One whose claude exited, or that `we delete --keep-worktree` stopped, stays
`none`. So after a reboot, `we open <id> --no-terminal` for each
`interrupted` environment brings back exactly what was running.

### After the worktree is deleted behind your back

```
we ls        # dir: ~/projects/trade.claude-md (missing)
we open 7
```

`we open` prunes the stale registration and re-adds the worktree at the
recorded path on the recorded branch.

## Placement and cleanup

### Put a worktree somewhere else, once

```
we open https://github.com/axklim/trade/issues/59 --wt ~/scratch/claude-md
```

Only the directory changes; the branch and session follow the usual rules. For
a permanent change, set `worktree_path` in the config instead.

### Finish up

```
we delete 7 --delete-branch
```

Removes the worktree, deletes the branch, drops the record, and kills the
session last.
A branch that is already gone — the PR was merged with "delete branch" — is
fine; only a branch that exists and cannot be deleted stops it. `--force` if
the worktree is dirty, `--keep-worktree` to stop after killing the session.

#### Remove the environment you are in

```
cd ~/projects/trade.review-claude-md-file && we delete --delete-branch
```

No target means the environment whose worktree holds the cwd, the one `ls`
marks with `*`. Since the session is killed only after everything else is
gone, an agent can run this from inside the session it removes: the session
ends with the command, and nothing is left half done.

### Clear out merged work

```
we ls            # 7  trade  trade-review-claude-md-file  done  #59 PR#61
we gc --dry-run  # would delete environment 7 (session trade-review-claude-md-file)
we gc --delete-branch
```

An environment is finished once every PR linked to it is merged or closed and
its worktree is gone; `ls` shows `done` in `STATE` for it (`"done": true` under
`--json`). `gc` deletes finished environments like `delete` does — a live
session is killed — and `--delete-branch` deletes their branches too. An
environment without a PR, such as a project home on `main`, is never
finished, and one whose worktree still exists stays.

Both ask `gh` for each PR's state. Without `gh` — not installed, offline, not
logged in — `gc` fails and deletes nothing, while `ls` still lists everything
and just does not mark anything `done`.

## On another machine

```
we open https://github.com/axklim/trade/issues/59 --host devbox
we ls --host devbox
we delete 7 --host devbox
```

The environment is created on devbox and recorded in devbox's registry; the
local Ghostty attaches to it over ssh. Ids are per host, so `7` there is not
`7` here.

## From another device

```
we open https://github.com/axklim/trade/issues/59 --rc --no-terminal
```

`claude` starts with `--remote-control trade-review-claude-md-file` as well
as `--name`, so the session appears under its tmux name in the Remote
Control list of a phone or laptop, with no terminal attached here. A session
started by another agent on this machine can be picked up that way. Set
`remote_control = true` to do it for every environment; `--rc` only matters
when the session is started, so a live one is left as it is. The environment
remembers `--rc`, so its session comes back with Remote Control after a
reboot.

With `--rc`, `open` (and `new`) wait for Remote Control to come up and end
with its link:

```
WE_SESSION=trade-review-claude-md-file
rc: https://claude.ai/code/session_01AbC…
```

That link is what to hand the person on the other device. If it does not
come within 2 minutes — the trust dialog of a fresh clone without
`trust_clones`, say — the last line is `rc: timeout`, the last screen lines go
to stderr, and the exit is non-zero. A session that is already running answers
at once if it runs Remote Control, and times out if it does not.

## Skipping claude's trust dialog in a fresh clone

```toml
trust_clones = true
```

A repository `open` has just cloned is new to `claude`, which would start on
its "Is this a project you trust?" dialog and wait — no Remote Control and
no `--prompt` until someone answers it. With `trust_clones = true`, `open`
records the answer in `~/.claude.json` for the clone before the session
starts, and every later worktree of that repository shares it. Only a clone
`open` made itself is trusted; an answer already in the file, yes or no, is
kept. It is off by default, since it answers a safety prompt for you. If
`~/.claude.json` cannot be read, `open` says so on stderr and carries on.

## Handing work to a new session

```
we open https://github.com/axklim/trade/issues/59 --no-terminal \
  --prompt "Implement issue 59 and open a PR"
```

`claude` starts with the text as its first prompt and gets to work at once,
which is how one agent dispatches a task to another. The text is shell-quoted
before it is typed into tmux, so quotes and `$` in it arrive as written. It
only reaches a session that `open` starts: if the session is already
running, nothing is typed into it and `open` says on stderr that `--prompt`
was ignored.

## Picking the model

```
we open https://github.com/axklim/trade/issues/59 --no-terminal \
  --model opus --effort high
```

`claude` starts with `--model opus --effort high`, for this environment only;
`claude_cmd` stays as it is for the rest. The values go to `claude` as
written, so whatever names `claude` accepts work here. If `claude_cmd`
already passes `--model` or `--effort`, the flag given to `open` replaces it. Like `--prompt`,
both only reach a session that `open` starts; on a running one `open` says on
stderr that they were ignored. Unlike `--prompt`, they are remembered: a
session recreated later, by `open` or `attach`, starts with them again.
