---
name: leftovers
description: Say what would be lost or left undone if a work environment made with `we` were closed now. Uncommitted, unpushed and unmerged work, stash entries, the branch's PR, and what lives only in the conversation. It looks and tells, and never deletes anything. Use when asked "is something left here?", "is anything left?", "is everything done?", "can I close this session?", or invoked as /workenv:leftovers [target].
argument-hint: "[target]"
---

# `/workenv:leftovers [target]`

This skill looks and tells. It never deletes, stops or cleans up anything,
whatever it finds: "can I close this session?" gets this report and nothing
more. Closing is `/workenv:close`.

## 1. The environment

With a target (an id, session name, branch, or issue/PR URL), run
`we show <target>`. With none, it is the environment this session runs in:
the row `we ls` marks with `*`; run `we show <id>` on it. When no row has
`*`, say that this session is not in a `we` environment, and stop. An error
from `we` is reported as is.

From `we show` take the branch `B`, the worktree `W` and the repository `R`.

## 2. The repository

Run these. When the worktree is `(missing)`, leave out the `status` line and
run the rest with `R` in place of `W`:

```bash
git -C W status --porcelain                         # uncommitted files
git -C W fetch --quiet origin
git -C W rev-parse --verify --quiet origin/B        # fails: never pushed
git -C W log --oneline origin/B..B                  # commits not pushed
(cd W && gh pr list --head B --state all --json number,state,url)
git -C W log --oneline origin/HEAD..B               # with no PR: not on the default branch
git -C W stash list | grep -iF "on B:"              # stash entries of the branch
git -C R status -sb | head -1                       # the main checkout: "behind N"
```

Compare with `origin/B`, not the upstream: a branch cut from the default
branch tracks `origin/<default>`. When `origin/HEAD` is not set, use
`origin/<default>`, the default from `gh repo view --json defaultBranchRef`.

| Finding | Lost if you close? |
|---|---|
| uncommitted files, commits not pushed, a branch never pushed | yes: commit and push |
| no PR, and commits not on the default branch | yes: open a PR, or say they can go |
| a stash entry of the branch | yes: apply and commit it, or drop it |
| the main checkout behind its remote | yes: pull it |
| an open PR | no; it goes under "Yours" when it waits for the human's review or merge |
| a merged or closed PR, everything pushed | no; a missing `origin/B` is then a branch deleted on merge |

A check that could not run (no `gh`, offline, a missing branch) is said so,
never reported as "nothing".

## 3. The conversation

Go through this conversation for what exists only here:

- a decision, a design or a finding written nowhere: not in a file, an issue,
  a PR or a commit;
- something you said you would do and have not done;
- a question still open, yours to the human or theirs to you.

For another environment as the target you cannot see its conversation: say
that only its repository was checked.

## 4. The answer

In this form, and nothing after it:

```
**Lost if you close:** <what would be gone or left half-done, each with what
fixes it; or "nothing.">

**Yours:** <what waits for the human: a decision, an approval, a thing only
they can do>
```

Leave "Yours" out when there is none. When both are empty, the whole answer
is "Nothing is left."

Keep out what changes nothing: a detail nobody will act on, a status the
human already knows, a cosmetic remark. Do not offer to close the session.
