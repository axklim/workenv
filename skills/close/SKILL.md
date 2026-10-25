---
name: close
description: Use when asked to remove, tear down or close a work environment made with `we`, or this session's own one. Invoked as /workenv:close [target]; with no target it removes the environment this session runs in.
argument-hint: "[target]"
---

# `/workenv:close [target]`

Removes a work environment: its worktree, its branch, its record, and its
tmux session, which goes last.

With a target (an id, session name, branch, or issue/PR URL), run:

```
we delete <target> --delete-branch
```

With no target, it is the environment this session's working directory is
in, which is normally **this session itself**. Its tmux session is killed
when the command finishes, so your reply is the last thing this session
sends. Say so first, in your reply, before running anything, for example
"Removing this session's environment (its worktree, branch and session);
this session ends now." Then run:

```
we delete --delete-branch
```

- Add `--force` only when the human asks for it (it discards uncommitted
  changes in the worktree). Never add it on your own after a refusal.
- A refusal or error (a dirty worktree, no environment
  for the target or the cwd) is reported as is, its stderr unchanged.
- Run nothing after the delete: when it removed this session, nothing after
  it would run anyway.
