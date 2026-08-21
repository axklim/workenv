# workenv UI (`we ui`) — design

Extends the [2026-08-17 design](2026-08-17-workenv-design.md) with an
interactive picker. Resolves
[issue #10](https://github.com/axklim/workenv/issues/10).

## What it is

A full-screen terminal picker over the registry:

```
we ui [--host H]
```

It lists work environments — the local registry's, or a remote host's — and
acts on the selected one: open it, open Zed in it, or create a new one. It is
a picker, not a resident dashboard: every action tears the screen down first
and then runs the exact same flow the corresponding CLI command would, so
`ui` adds no second code path for opening, creating or attaching.

A TUI rather than a GUI: `we` is a terminal tool (tmux, Ghostty, ssh), and
the standard-library-only rule leaves no room for a GUI toolkit — or a TUI
framework. The screen is drawn with ANSI escape sequences; the terminal is
put in raw mode with `stty` (see *Terminal handling*).

## Keys

| Key            | Action                                                    |
|----------------|-----------------------------------------------------------|
| `↓`/`j`, `↑`/`k` | move the selection                                      |
| `Enter`        | open the selected environment (repair + attach terminal)  |
| `z`            | open Zed in the selected environment                      |
| `n`            | new environment: prompts for a target, then an optional repo |
| `h`            | switch host: prompts for a hostname, empty means local    |
| `r`            | reload the list                                           |
| `q`, `Ctrl-C`  | quit                                                      |

The `n` prompt takes anything `we open` takes — an issue/PR/repository URL, a
branch, a plain name — and the optional repo prompt is `--repo`. The three
creation overrides (`--branch`, `--session`, `--wt`) stay CLI-only.

The list is the `ls` table (ID, PROJECT, SESSION, STATE, REFS) one line per
environment, the selected row inverted; the footer shows the selection's
worktree directory, the key help, and the last error. Errors (a failed
reload, an unreachable host) land in the footer rather than ending the UI.

## Local and remote

With no `--host`, rows come from the local registry via the same listing flow
`we ls` uses. With `--host H` (or after `h`), rows come from
`ssh H <remote_we> ls --json`, and every action goes through the existing
remote path: Enter and `n` run `ssh H we open <target> --no-terminal` and
attach a local Ghostty over ssh, exactly like `we open --host H`.

`we ls --json` is the plumbing that makes the remote list parseable: it
prints the listing as a JSON array (`[]`, not `null`, when empty) with
snake_case fields matching the registry's naming, and is pass-through for
`--host` like `-l` is. A remote `we` too old to know `--json` fails the ssh
call; the error says to update it.

## Zed

`z` materialises the environment first — the open flow with no terminal, so a
missing worktree or session is repaired the same way `open` repairs it — and
then launches Zed on the worktree:

- local: `zed <worktree_path>`
- remote: `zed ssh://<host><worktree_path>` (Zed's SSH remoting; the path is
  absolute, so host and path concatenate)

The binary is the `zed_cmd` config key, default `"zed"`, next to `claude_cmd`
and `remote_we`.

## Terminal handling

Raw mode is entered and left with `stty`, which keeps the standard-library
rule and routes through `execx.Runner` like every other external command —
so the whole UI is drivable by the scripted fake. `stty` reads the terminal
from stdin and prints to stdout (`stty -g`, `stty size`), which no existing
Runner method supports; the interface grows one method:

- `OutputWithStdin` — stdin attached to the caller's, stdout captured.

Enter saves the settings with `stty -g` and applies `stty raw -echo`; exit
restores the saved settings (`stty sane` if saving failed). The screen is the
alternate screen (`ESC [?1049h/l`) with the cursor hidden, redrawn in full on
every change; the size comes from `stty size`, falling back to 80×24, read
once per reload rather than per keypress (no SIGWINCH handling).

Key decoding is bytes off stdin: printable runes, `Enter` (CR), backspace,
`Ctrl-C`, and CSI sequences for the arrows. `we ui` refuses to start when
stdin or stdout is not a terminal.

## Placement

The low-level pieces — raw mode, size, key decoding — are `internal/term`, a
thin wrapper like `tmuxx`. The picker itself (model, drawing, key loop,
prompts) is `cmd/we/ui.go`: it is rendering plus dispatch into flows
`internal/we` already owns, which is cmd/we's job. `internal/we` gains only
the two Zed launchers.

## Testing

The UI loop takes its input as an `io.Reader` and draws to an `io.Writer`,
with all external commands on the fake Runner — a test scripts keystrokes as
bytes, then asserts on the recorded calls (`stty`, `ssh … ls --json`,
`zed …`, the open flow's git/tmux calls) and on what the frame contains.

- **execx** — `OutputWithStdin` captures trimmed stdout; the fake records it
  under its own Method.
- **term** — key decoding table; raw enter/exit argv (saved settings vs
  `sane`); size parsing and its fallback.
- **cmd** — quit restores the terminal; rows rendered; navigation changes
  what Enter opens; `z` launches Zed locally and over ssh; `n` passes target
  and repo through; `h` switches to the remote list; a cancelled prompt acts
  on nothing; `ls --json` shape, emptiness, and `--host` pass-through.
