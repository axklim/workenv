---
name: close
description: Use when told to remove, tear down or close a work environment made with `we`, or this session's own one. Invoked as /workenv:close [target] [now]; with no target it removes the environment this session runs in. It checks what is left first. Not for a question such as "can I close this session?": that is workenv:leftovers.
argument-hint: "[target] [now]"
---

# `/workenv:close [target] [now]`

Removes a work environment: its worktree, its branch, its record, and its
tmux session, which goes last.

A bare question never deletes: "can I close this session?" is
`/workenv:leftovers`, not this skill.

## 1. Check first

Without `now`, do the `workenv:leftovers` check for the target (or for this
session's environment), steps 1 to 3 of that skill.

- **Something would be lost:** give the leftovers answer, in its form, and
  stop. Delete nothing; the human decides.
- **Nothing would be lost:** go on to step 2. Points that only wait for the
  human ("Yours") do not block: they go into your reply before the delete.
- **Not in an environment, or an error:** report it, and stop.

With `now` as the last word (`/workenv:close now`, `/workenv:close 7 now`),
skip the check and go to step 2.

## 2. Delete

With a target (an id, session name, branch, or issue/PR URL), run:

```
we delete <target> --delete-branch
```

Give the "Yours" points, if any, after its output: they are your last words.

With no target, it is the environment this session's working directory is
in, which is normally **this session itself**. Its tmux session is killed
when the command finishes, so your reply is the last thing this session
sends. Say so first, in your reply, before running anything, for example
"Removing this session's environment (its worktree, branch and session);
this session ends now." End that reply with the "Yours" points, if any.
Then run:

```
we delete --delete-branch
```

- `now` is not `--force`. Add `--force` only when the human asks for it (it
  discards uncommitted changes in the worktree). Never add it on your own
  after a refusal.
- A refusal or error (a dirty worktree, no environment
  for the target or the cwd) is reported as is, its stderr unchanged.
- Run nothing after the delete: when it removed this session, nothing after
  it would run anyway.
