package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"workenv/internal/target"
	"workenv/internal/term"
	"workenv/internal/we"
)

// we ui — a full-screen picker over the registry (local, or a remote
// host's via `ssh H we ls --json`). It is a picker, not a resident
// dashboard: every action tears the screen down first and then runs the
// same flow the corresponding CLI command would, so ui adds no second code
// path for opening, creating or attaching.

const (
	ansiClearHome  = "\x1b[2J\x1b[H"
	ansiAltScreen  = "\x1b[?1049h"
	ansiMainScreen = "\x1b[?1049l"
	ansiHideCursor = "\x1b[?25l"
	ansiShowCursor = "\x1b[?25h"
	ansiInverse    = "\x1b[7m"
)

type uiActionKind int

const (
	uiQuit uiActionKind = iota
	uiOpen              // run the open flow on target (a selected id, or what `n` collected)
	uiZed               // repair item with no terminal, then launch zed on its worktree
)

// uiAction is what the key loop decided to do; it runs only after the
// terminal is back in cooked mode on the main screen.
type uiAction struct {
	kind         uiActionKind
	host         string // the host the action was chosen on ("" = local)
	target, repo string // uiOpen
	item         we.Item
}

// runUI puts the terminal in raw mode on the alternate screen, runs the key
// loop, restores the terminal, and only then performs whatever action the
// loop decided on.
func runUI(env *we.Env, in io.Reader, out io.Writer, host string) error {
	raw := &term.Raw{R: env.R}
	if err := raw.Enter(); err != nil {
		return err
	}
	fmt.Fprint(out, ansiAltScreen+ansiHideCursor)
	u := &ui{env: env, in: bufio.NewReader(in), out: out, host: host}
	act := u.loop()
	fmt.Fprint(out, ansiShowCursor+ansiMainScreen)
	raw.Exit()
	return act.run(env)
}

type ui struct {
	env        *we.Env
	in         *bufio.Reader
	out        io.Writer
	host       string // "" = the local registry
	items      []we.Item
	sel        int
	rows, cols int
	status     string // the footer's error line
}

func (u *ui) loop() uiAction {
	u.reload()
	for {
		u.draw()
		key, b := term.ReadKey(u.in)
		switch {
		case key == term.KeyCtrlC || key == term.KeyEOF || (key == term.KeyRune && b == 'q'):
			return uiAction{kind: uiQuit}
		case key == term.KeyDown || (key == term.KeyRune && b == 'j'):
			if u.sel < len(u.items)-1 {
				u.sel++
			}
		case key == term.KeyUp || (key == term.KeyRune && b == 'k'):
			if u.sel > 0 {
				u.sel--
			}
		case key == term.KeyEnter:
			if len(u.items) > 0 {
				return uiAction{kind: uiOpen, host: u.host, target: strconv.Itoa(u.items[u.sel].ID)}
			}
		case key == term.KeyRune && b == 'z':
			if len(u.items) > 0 {
				return uiAction{kind: uiZed, host: u.host, item: u.items[u.sel]}
			}
		case key == term.KeyRune && b == 'n':
			if act, ok := u.promptNew(); ok {
				return act
			}
		case key == term.KeyRune && b == 'h':
			u.promptHost()
		case key == term.KeyRune && b == 'r':
			u.reload()
		}
	}
}

// reload re-reads the terminal size and the list. A failed list (an
// unreachable host, say) keeps the previous items and lands in the footer
// instead of ending the UI.
func (u *ui) reload() {
	u.rows, u.cols = term.Size(u.env.R)
	items, err := u.listItems()
	if err != nil {
		u.status = err.Error()
		return
	}
	u.items = items
	u.status = ""
	if u.sel >= len(items) {
		u.sel = max(0, len(items)-1)
	}
}

func (u *ui) listItems() ([]we.Item, error) {
	if u.host == "" {
		return u.env.List()
	}
	out, err := u.env.R.Output("", "ssh", u.host, u.env.Cfg.RemoteWe, "ls", "--json")
	if err != nil {
		return nil, fmt.Errorf("remote ls on %s (does its we know --json?): %w", u.host, err)
	}
	var items []we.Item
	if err := json.Unmarshal([]byte(out), &items); err != nil {
		return nil, fmt.Errorf("remote ls on %s: unparsable output: %w", u.host, err)
	}
	return items, nil
}

// draw repaints the whole frame: title, the ls table with the selected row
// inverted, and a three-line footer (the selection's directory, the key
// help, the last error). Raw mode needs explicit \r\n line endings.
func (u *ui) draw() {
	where := "local"
	if u.host != "" {
		where = "host " + u.host
	}
	lines := []string{
		ansiBold + u.trunc(fmt.Sprintf("we ui — %s — %d environments", where, len(u.items))) + ansiReset,
		"",
	}
	lines = append(lines, u.tableLines()...)
	for len(lines) < u.rows-3 {
		lines = append(lines, "")
	}
	lines = append(lines, u.footerLines()...)
	fmt.Fprint(u.out, ansiClearHome+strings.Join(lines, "\r\n"))
}

// tableLines renders the ls table, one line per environment, windowed so
// the selection stays on screen when there are more rows than fit.
func (u *ui) tableLines() []string {
	if len(u.items) == 0 {
		return []string{"no work environments — n creates one"}
	}
	idW, projW, sessW, stateW := len("ID"), len("PROJECT"), len("SESSION"), len("STATE")
	for _, it := range u.items {
		idW = max(idW, len(strconv.Itoa(it.ID)))
		projW = max(projW, len(it.Project))
		sessW = max(sessW, len(it.Session))
		stateW = max(stateW, len(it.SessionState))
	}
	lines := []string{ansiBold + u.trunc(padLeft("ID", idW)+colSep+padRight("PROJECT", projW)+colSep+
		padRight("SESSION", sessW)+colSep+padRight("STATE", stateW)+colSep+"REFS") + ansiReset}

	visible := max(1, u.rows-6) // title, blank, table header, three footer lines
	top := 0
	if u.sel >= visible {
		top = u.sel - visible + 1
	}
	for i := top; i < min(len(u.items), top+visible); i++ {
		it := u.items[i]
		row := u.trunc(padLeft(strconv.Itoa(it.ID), idW) + colSep + padRight(it.Project, projW) + colSep +
			padRight(it.Session, sessW) + colSep + padRight(it.SessionState, stateW) + colSep +
			formatRefs(it.Issues, it.PRs, false))
		if i == u.sel {
			row = ansiInverse + row + ansiReset
		}
		lines = append(lines, row)
	}
	return lines
}

func (u *ui) footerLines() []string {
	dir := ""
	if len(u.items) > 0 {
		dir = dirLine(u.items[u.sel])
	}
	return []string{
		ansiDim + u.trunc(dir) + ansiReset,
		ansiDim + u.trunc("↑↓ move · enter open · z zed · n new · h host · r reload · q quit") + ansiReset,
		u.trunc(u.status),
	}
}

// trunc clips a line to the terminal width. It must run before any escape
// codes are wrapped around the line — clipping afterwards could cut a
// sequence in half.
func (u *ui) trunc(s string) string {
	if u.cols > 0 && len(s) > u.cols {
		return s[:u.cols]
	}
	return s
}

// promptNew collects what `we open` would have taken from the command line:
// a target, then an optional --repo for a plain name.
func (u *ui) promptNew() (uiAction, bool) {
	tgt, ok := u.promptLine("new target (issue/PR/repo URL, branch): ")
	tgt = strings.TrimSpace(tgt)
	if !ok || tgt == "" {
		return uiAction{}, false
	}
	repo, ok := u.promptLine("repo (optional, for a plain name): ")
	if !ok {
		return uiAction{}, false
	}
	return uiAction{kind: uiOpen, host: u.host, target: tgt, repo: strings.TrimSpace(repo)}, true
}

// promptHost switches which registry the list shows; empty input means the
// local one. A cancelled prompt keeps the current host.
func (u *ui) promptHost() {
	host, ok := u.promptLine("host (empty for local): ")
	if !ok {
		return
	}
	u.host = strings.TrimSpace(host)
	u.sel = 0
	u.reload()
}

// promptLine runs a minimal line editor on the frame's bottom line:
// printable bytes append, backspace deletes, enter submits, and Esc or
// Ctrl-C cancels (ok = false).
func (u *ui) promptLine(label string) (string, bool) {
	var buf []byte
	defer fmt.Fprint(u.out, ansiHideCursor)
	for {
		fmt.Fprintf(u.out, "\x1b[%d;1H\x1b[2K%s%s%s", u.rows, ansiShowCursor, label, buf)
		key, b := term.ReadKey(u.in)
		switch key {
		case term.KeyEnter:
			return string(buf), true
		case term.KeyCtrlC, term.KeyEsc, term.KeyEOF:
			return "", false
		case term.KeyBackspace:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
			}
		case term.KeyRune:
			buf = append(buf, b)
		}
	}
}

// run performs the loop's decision on a restored terminal, through the
// flows the CLI commands use: openRemote for a remote host, env.Open
// locally; uiZed repairs with no terminal first, then launches Zed.
func (a uiAction) run(env *we.Env) error {
	switch a.kind {
	case uiOpen:
		if a.host != "" {
			return openRemote(env, "open", a.host, a.target, a.repo, "", "", "", false)
		}
		tgt, err := target.Parse(a.target)
		if err != nil {
			return err
		}
		res, err := env.Open(we.OpenOptions{Target: tgt, Repo: a.repo})
		if err != nil {
			return err
		}
		printOpenResult(res)
		return nil
	case uiZed:
		if a.host != "" {
			if err := openRemote(env, "open", a.host, strconv.Itoa(a.item.ID), "", "", "", "", true); err != nil {
				return err
			}
			return env.ZedRemote(a.host, a.item.WorktreePath)
		}
		tgt, err := target.Parse(strconv.Itoa(a.item.ID))
		if err != nil {
			return err
		}
		res, err := env.Open(we.OpenOptions{Target: tgt, NoTerminal: true})
		if err != nil {
			return err
		}
		printOpenResult(res)
		return env.Zed(res.WorktreePath)
	}
	return nil
}
