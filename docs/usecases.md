# Use cases

Worked examples, in the order a day tends to go. Paths assume
`projects_path = ~/projects` and the default `worktree_path`; see the
[README](../README.md) for both.

- [Starting work](#starting-work) — from an issue, its PR, a fork's PR, a bare
  repository, or nothing at all
- [Coming back](#coming-back) — finding an environment again, and renaming its
  branch
- [When something breaks](#when-something-breaks) — a reboot, a deleted
  worktree
- [Placement and cleanup](#placement-and-cleanup) — one-off paths, tearing down
- [On another machine](#on-another-machine) — `--host`

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

### Start a branch with no issue behind it

```
cd ~/projects/trade && we open spike-latency
we open spike-latency --repo trade          # equivalent, from anywhere
we open spike-latency --repo ~/src/fork     # a repository outside projects_path
```

Branch `spike-latency` off the default branch, worktree
`~/projects/trade.spike-latency`, session `trade-spike-latency`, no refs.

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
started in its first window. The worktree is untouched, and the branch is
whatever git says it is.

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

Kills the session, removes the worktree, deletes the branch, drops the record.
`--force` if the worktree is dirty, `--keep-worktree` to stop after killing the
session.

## On another machine

```
we open https://github.com/axklim/trade/issues/59 --host devbox
we ls --host devbox
we delete 7 --host devbox
```

The environment is created on devbox and recorded in devbox's registry; the
local Ghostty attaches to it over ssh. Ids are per host, so `7` there is not
`7` here.
