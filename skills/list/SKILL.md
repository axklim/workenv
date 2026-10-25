---
name: list
description: Use when asked which work environments or worker sessions `we` has, or invoked as /workenv:list.
---

# `/workenv:list`

Run:

```
we ls
```

Report its output as is, in a code block, and nothing else. `*` before an id
marks the environment this session is in. To read the state rather than show
it, `we ls --json` has the same rows with stable keys; the `workenv:open`
skill explains the `STATE` column.
