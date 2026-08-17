package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"workenv/internal/we"
)

// plainItems returns the two items used across the list-rendering tests: one
// attached environment with an issue and a PR, one detached environment
// whose worktree directory is missing and carries no refs.
func plainItems(home string) []we.Item {
	return []we.Item{
		{
			ID:           7,
			Project:      "trade",
			Branch:       "review_claude-file",
			Session:      "trade-review_claude-file",
			SessionState: "attached",
			WorktreePath: home + "/projects/trade.review_claude-file",
			RepoPath:     home + "/projects/trade",
			Issues:       []string{"https://github.com/axklim/trade/issues/59"},
			PRs:          []string{"https://github.com/axklim/trade/pull/61"},
			Exists:       true,
		},
		{
			ID:           8,
			Project:      "trade",
			Branch:       "dev-overlay-pins-a-stale-mini-internal",
			Session:      "trade-dev-overlay-pins-a-stale-mini-internal",
			SessionState: "detached",
			WorktreePath: home + "/projects/trade.dev-overlay-pins-a-stale-mini-internal",
			RepoPath:     home + "/projects/trade",
			Exists:       false,
		},
	}
}

// projectColumn returns the column (rune index) at which "PROJECT" starts in
// the header line, so dir-line indentation can be checked without depending
// on the exact width formula.
func projectColumn(t *testing.T, header string) int {
	t.Helper()
	idx := strings.Index(header, "PROJECT")
	if idx < 0 {
		t.Fatalf("header %q has no PROJECT column", header)
	}
	return idx
}

func TestRenderListPlain(t *testing.T) {
	t.Setenv("HOME", "/Users/u")
	items := plainItems("/Users/u")
	var buf bytes.Buffer
	if err := renderList(&buf, items, renderOpts{}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "\x1b") {
		t.Fatalf("plain output must contain no ANSI escapes, got %q", out)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 5 { // header + 2 rows * 2 lines each
		t.Fatalf("expected 5 lines, got %d: %q", len(lines), lines)
	}
	header := lines[0]
	for _, col := range []string{"ID", "PROJECT", "SESSION", "STATE", "REFS"} {
		if !strings.Contains(header, col) {
			t.Errorf("header %q missing column %q", header, col)
		}
	}
	projectCol := projectColumn(t, header)

	row1, dir1 := lines[1], lines[2]
	row2, dir2 := lines[3], lines[4]

	if !strings.Contains(row1, "trade-review_claude-file") || !strings.Contains(row1, "attached") {
		t.Errorf("row1 %q missing expected fields", row1)
	}
	if !strings.Contains(row1, "#59 PR#61") {
		t.Errorf("row1 %q missing refs #59 PR#61", row1)
	}

	trimmed1 := strings.TrimLeft(dir1, " ")
	if !strings.HasPrefix(trimmed1, "dir:") {
		t.Errorf("dir1 %q must start with dir: after its indent", dir1)
	}
	if idx := strings.Index(dir1, "dir:"); idx != projectCol {
		t.Errorf("dir1 %q: dir: at col %d, want %d (under PROJECT)", dir1, idx, projectCol)
	}
	if !strings.Contains(dir1, "~/projects/trade.review_claude-file") {
		t.Errorf("dir1 %q missing ~-abbreviated path", dir1)
	}
	if strings.Contains(dir1, "(missing)") {
		t.Errorf("dir1 %q must not be marked (missing), the directory exists", dir1)
	}

	if !strings.Contains(row2, "detached") {
		t.Errorf("row2 %q missing state detached", row2)
	}
	if !strings.Contains(row2, "-") {
		t.Errorf("row2 %q missing '-' for no refs", row2)
	}
	if idx := strings.Index(dir2, "dir:"); idx != projectCol {
		t.Errorf("dir2 %q: dir: at col %d, want %d (under PROJECT)", dir2, idx, projectCol)
	}
	if !strings.Contains(dir2, "(missing)") {
		t.Errorf("dir2 %q missing (missing) marker", dir2)
	}
}

func TestRenderListMarksCurrent(t *testing.T) {
	t.Setenv("HOME", "/Users/u")
	items := plainItems("/Users/u")
	items[1].Current = true
	var buf bytes.Buffer
	if err := renderList(&buf, items, renderOpts{}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	row1, row2 := lines[1], lines[3]
	if strings.Contains(row1, "*") {
		t.Errorf("row1 %q must not be marked current", row1)
	}
	if !strings.Contains(row2, "*8") {
		t.Errorf("row2 %q must be marked current with *8", row2)
	}
}

func TestRenderListColorAndLinks(t *testing.T) {
	t.Setenv("HOME", "/Users/u")
	items := plainItems("/Users/u")
	var buf bytes.Buffer
	if err := renderList(&buf, items, renderOpts{Color: true, Links: true}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	header, row1, dir1 := lines[0], lines[1], lines[2]

	if !strings.Contains(header, "\x1b[1m") {
		t.Errorf("header %q must be bold", header)
	}
	if !strings.Contains(dir1, "\x1b[2m") {
		t.Errorf("dir1 %q must be dimmed", dir1)
	}
	wantIssueLink := "\x1b]8;;https://github.com/axklim/trade/issues/59\x1b\\#59\x1b]8;;\x1b\\"
	if !strings.Contains(row1, wantIssueLink) {
		t.Errorf("row1 %q missing issue OSC 8 link %q", row1, wantIssueLink)
	}
	wantPRLink := "\x1b]8;;https://github.com/axklim/trade/pull/61\x1b\\PR#61\x1b]8;;\x1b\\"
	if !strings.Contains(row1, wantPRLink) {
		t.Errorf("row1 %q missing PR OSC 8 link %q", row1, wantPRLink)
	}
}

func TestRenderShow(t *testing.T) {
	t.Setenv("HOME", "/Users/u")
	created := time.Date(2026, 8, 16, 18, 12, 3, 0, time.UTC)
	item := we.Item{
		ID:           7,
		Project:      "trade",
		Branch:       "review_claude-file",
		Session:      "trade-review_claude-file",
		SessionState: "attached",
		WorktreePath: "/Users/u/projects/trade.review_claude-file",
		RepoPath:     "/Users/u/projects/trade",
		Issues:       []string{"https://github.com/axklim/trade/issues/59"},
		PRs:          []string{"https://github.com/axklim/trade/pull/61"},
		Exists:       false,
		CreatedAt:    created,
	}
	var buf bytes.Buffer
	if err := renderShow(&buf, item, renderOpts{}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "\x1b") {
		t.Fatalf("plain show output must contain no ANSI escapes, got %q", out)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 {
		t.Fatal("renderShow produced no output")
	}
	for _, want := range []string{"7", "trade", "review_claude-file", "trade-review_claude-file", "attached"} {
		found := false
		for _, l := range lines {
			if strings.Contains(l, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("output missing field containing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "~/projects/trade.review_claude-file (missing)") {
		t.Errorf("worktree line must be ~-abbreviated and marked (missing):\n%s", out)
	}
	if !strings.Contains(out, "https://github.com/axklim/trade/issues/59") {
		t.Errorf("output missing full issue URL:\n%s", out)
	}
	if !strings.Contains(out, "https://github.com/axklim/trade/pull/61") {
		t.Errorf("output missing full PR URL:\n%s", out)
	}

	// Every field is on its own line: no line holds more than one of the
	// distinguishing values checked above.
	lineCount := map[string]int{}
	for _, l := range lines {
		lineCount[l]++
	}
	for l, n := range lineCount {
		if n > 1 {
			t.Errorf("line %q repeated %d times, expected one field per line", l, n)
		}
	}
}

// TestRenderListLong asserts renderList itself dispatches to the stacked
// form when opts.Long is set — the table columns (SESSION, STATE) must not
// appear, and each item's renderShow output (starting "id:") must, so the
// field really drives behaviour rather than being read by nobody.
func TestRenderListLong(t *testing.T) {
	t.Setenv("HOME", "/Users/u")
	items := plainItems("/Users/u")

	var wantBuf bytes.Buffer
	for i, it := range items {
		if i > 0 {
			wantBuf.WriteByte('\n')
		}
		if err := renderShow(&wantBuf, it, renderOpts{Long: true}); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	if err := renderList(&buf, items, renderOpts{Long: true}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != wantBuf.String() {
		t.Errorf("renderList(Long:true) = %q, want %q", buf.String(), wantBuf.String())
	}
	if strings.Contains(buf.String(), "dir:") {
		t.Errorf("stacked form must not print the table's dir: line:\n%s", buf.String())
	}
}

func TestAbbrevHome(t *testing.T) {
	t.Setenv("HOME", "/Users/u")
	cases := map[string]string{
		"/Users/u":          "~",
		"/Users/u/projects": "~/projects",
		"/Users/uother/x":   "/Users/uother/x",
		"/other/path":       "/other/path",
	}
	for in, want := range cases {
		if got := abbrevHome(in); got != want {
			t.Errorf("abbrevHome(%q) = %q, want %q", in, got, want)
		}
	}
}
