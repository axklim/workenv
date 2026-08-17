package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"workenv/internal/we"
)

// renderOpts controls how renderList and renderShow decorate their output.
// main.go decides these from the environment (a TTY, NO_COLOR); the tests
// drive them directly.
type renderOpts struct {
	Color bool // bold header, dimmed dir line
	Links bool // OSC 8 hyperlinks on REFS entries
	Long  bool // renderList prints every item's stacked form instead of the table
}

const (
	colSep    = "  "
	ansiBold  = "\x1b[1m"
	ansiDim   = "\x1b[2m"
	ansiReset = "\x1b[0m"
)

// abbrevHome replaces a leading home directory in p with "~", the same
// abbreviation `we ls` and `we show` use for every path they print.
func abbrevHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	home = filepath.Clean(home)
	if p == home {
		return "~"
	}
	if rest, ok := strings.CutPrefix(p, home+string(filepath.Separator)); ok {
		return "~" + string(filepath.Separator) + rest
	}
	return p
}

// hyperlink wraps text in an OSC 8 hyperlink pointing at url.
func hyperlink(url, text string) string {
	return "\x1b]8;;" + url + "\x1b\\" + text + "\x1b]8;;\x1b\\"
}

// refNumber is the trailing number of a canonical issue/PR URL.
func refNumber(url string) string {
	if i := strings.LastIndex(url, "/"); i >= 0 {
		return url[i+1:]
	}
	return url
}

// formatRefs renders REFS: "#59 PR#61", each wrapped in an OSC 8 link to its
// full URL when links is set, "-" when there are none.
func formatRefs(issues, prs []string, links bool) string {
	var parts []string
	for _, u := range issues {
		text := "#" + refNumber(u)
		if links {
			text = hyperlink(u, text)
		}
		parts = append(parts, text)
	}
	for _, u := range prs {
		text := "PR#" + refNumber(u)
		if links {
			text = hyperlink(u, text)
		}
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

func padLeft(s string, w int) string {
	if n := w - len(s); n > 0 {
		return strings.Repeat(" ", n) + s
	}
	return s
}

func padRight(s string, w int) string {
	if n := w - len(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// dirLine is the second line of a row: "dir: <~-abbreviated path>", with
// "(missing)" appended when the worktree directory no longer exists.
func dirLine(it we.Item) string {
	line := "dir: " + abbrevHome(it.WorktreePath)
	if !it.Exists {
		line += " (missing)"
	}
	return line
}

// renderList prints `ls`'s output: with opts.Long, the stacked form for
// every item (renderShow, blank-line separated) — otherwise the table, per
// the design doc's Listing section: a header row, then two lines per
// environment — the table row and a dimmed "dir:" line indented to the
// start of the PROJECT column. Columns are padded to the widest cell
// (their own, or the header's); REFS is last and left ragged.
func renderList(w io.Writer, items []we.Item, opts renderOpts) error {
	if opts.Long {
		return renderListLong(w, items, opts)
	}
	idW, projW, sessW, stateW := len("ID"), len("PROJECT"), len("SESSION"), len("STATE")
	ids := make([]string, len(items))
	for i, it := range items {
		id := strconv.Itoa(it.ID)
		if it.Current {
			id = "*" + id
		}
		ids[i] = id
		idW = max(idW, len(id))
		projW = max(projW, len(it.Project))
		sessW = max(sessW, len(it.Session))
		stateW = max(stateW, len(it.SessionState))
	}

	header := padLeft("ID", idW) + colSep + padRight("PROJECT", projW) + colSep +
		padRight("SESSION", sessW) + colSep + padRight("STATE", stateW) + colSep + "REFS"
	if opts.Color {
		header = ansiBold + header + ansiReset
	}
	if _, err := fmt.Fprintln(w, header); err != nil {
		return err
	}

	indent := strings.Repeat(" ", idW+len(colSep))
	for i, it := range items {
		row := padLeft(ids[i], idW) + colSep + padRight(it.Project, projW) + colSep +
			padRight(it.Session, sessW) + colSep + padRight(it.SessionState, stateW) + colSep +
			formatRefs(it.Issues, it.PRs, opts.Links)
		if _, err := fmt.Fprintln(w, row); err != nil {
			return err
		}
		dir := indent + dirLine(it)
		if opts.Color {
			dir = ansiDim + dir + ansiReset
		}
		if _, err := fmt.Fprintln(w, dir); err != nil {
			return err
		}
	}
	return nil
}

// renderListLong is renderList's opts.Long branch: every item's stacked
// form (renderShow), separated by a blank line.
func renderListLong(w io.Writer, items []we.Item, opts renderOpts) error {
	for i, it := range items {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if err := renderShow(w, it, opts); err != nil {
			return err
		}
	}
	return nil
}

// renderShow prints the stacked form for one environment (`we show`, and
// each item under `ls -l`): one labelled field per line, full issue/PR
// URLs, the worktree path marked "(missing)" when its directory is gone.
func renderShow(w io.Writer, it we.Item, opts renderOpts) error {
	fields := [][2]string{
		{"id", strconv.Itoa(it.ID)},
		{"project", it.Project},
		{"branch", it.Branch},
		{"session", it.Session},
		{"state", it.SessionState},
		{"worktree", dirWorktree(it)},
		{"repo", abbrevHome(it.RepoPath)},
	}
	for _, u := range it.Issues {
		fields = append(fields, [2]string{"issue", u})
	}
	for _, u := range it.PRs {
		fields = append(fields, [2]string{"pr", u})
	}
	fields = append(fields, [2]string{"created", it.CreatedAt.Format(time.RFC3339)})

	labelW := 0
	for _, f := range fields {
		labelW = max(labelW, len(f[0])+1) // +1 for the trailing colon
	}
	for _, f := range fields {
		line := padRight(f[0]+":", labelW) + " " + f[1]
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

// dirWorktree is the worktree field's value: ~-abbreviated, "(missing)"
// appended when the directory is gone.
func dirWorktree(it we.Item) string {
	v := abbrevHome(it.WorktreePath)
	if !it.Exists {
		v += " (missing)"
	}
	return v
}
