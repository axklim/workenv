# Stateful Work Environments Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record work environments in a JSON registry so branches, session
names and worktree paths are stored rather than derived; resolve GitHub
issues and their linked PRs to the same environment; place worktrees where
git users expect them, with a config override.

**Architecture:** A new `internal/state` package owns the registry
(`$XDG_STATE_HOME/workenv/envs.json`). `internal/we` is rewritten around it:
`Open` resolves a target through state → GitHub links → git worktrees and
creates the record when nothing matches (unless attach-only); `Delete` and
`List` work off the registry. `internal/gitx` learns to find the repository
root from any layout, detect a bare container, and fetch a PR head; `gh`
exposes issue↔PR links. `cmd/we` gains `open`/`attach` and `--name`/`--branch`.

**Tech Stack:** Go 1.23 standard library only; `git`, `gh`, `tmux` via the
`execx.Runner` abstraction; tests use `execx.Fake`; the toolchain runs in
Docker through the Makefile.

**Spec:** `docs/superpowers/specs/2026-08-16-stateful-workenv-design.md`

## Global Constraints

- Standard library only — no new `go.mod` requirements.
- Go 1.23.5 (`go.mod`), gofmt-clean; `make check` must pass at the end of
  every task.
- All git/gh/tmux calls go through `execx.Runner`; tests script them with
  `execx.Fake` (prefix match on `"name arg arg"`, unmatched → success, empty
  output).
- Run tests with the containerised toolchain from the repo root:
  `make test` (whole suite) or, for one package,
  `docker run --rm -v "$PWD":/src -w /src -v "$HOME/.cache/workenv-go":/cache -u $(id -u):$(id -g) -e HOME=/cache -e GOCACHE=/cache/build -e GOMODCACHE=/cache/mod -e CGO_ENABLED=0 -e GOFLAGS=-buildvcs=false golang:1.23.5 go test ./internal/state/`
  (swap the package path). `make fmt` rewrites with gofmt.
- Names fixed by the spec: state file `envs.json`; JSON keys `project`,
  `name`, `session`, `branch`, `path`, `repo_dir`, `owner`, `repo`,
  `issues`, `prs`, `created_at`; session `we-<project>-<name>`; commands
  `open` (aliases `create`, `up`), `attach`, `list` (`ls`), `delete` (`rm`,
  `down`); flags `--project`, `--name`, `--branch`, `--host`,
  `--no-terminal`, `--force`, `--delete-branch`, `--keep-worktree`; config
  keys unchanged (`projects_dir`, `worktrees_dir`, `claude_cmd`,
  `remote_we`).
- Markdown in the repo: wrap prose at ~90 columns, keep tables narrow with
  aligned columns.
- Commit after each task with a conventional prefix (`feat:`, `refactor:`,
  `docs:`), body wrapped at 72, trailer
  `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

### Task 1: `internal/state` — the registry

**Files:**
- Create: `internal/state/state.go`
- Test: `internal/state/state_test.go`

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces (used by Task 4):
  - `type Env struct { Project, Name, Session, Branch, Path, RepoDir, Owner, Repo string; Issues, PRs []int; CreatedAt time.Time }`
  - `type Store struct { Path string; Envs []*Env }`
  - `func DefaultPath() string`
  - `func Load(path string) (*Store, error)` — missing file → empty store
  - `func (s *Store) Save() error` — atomic
  - `func (s *Store) BySession(session string) *Env`
  - `func (s *Store) ByName(project, name string) *Env`
  - `func (s *Store) ByBranch(project, branch string) *Env`
  - `func (s *Store) ByPath(path string) *Env`
  - `func (s *Store) ByIssue(owner, repo string, n int) *Env`
  - `func (s *Store) ByPR(owner, repo string, n int) *Env`
  - `func (s *Store) Matching(pred func(*Env) bool) []*Env`
  - `func (s *Store) Add(env *Env) *Env`
  - `func (s *Store) Remove(project, name string) bool`
  - `func (s *Store) Link(env *Env, owner, repo string, issues, prs []int)`

- [ ] **Step 1: Write the failing tests**

`internal/state/state_test.go`:

```go
package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sample() *Env {
	return &Env{
		Project: "trade", Name: "review_claude-file",
		Session: "we-trade-review_claude-file", Branch: "review_claude-file",
		Path: "/u/projects/trade/review_claude-file", RepoDir: "/u/projects/trade",
		Owner: "axklim", Repo: "trade", Issues: []int{59}, PRs: []int{61},
		CreatedAt: time.Date(2026, 8, 16, 14, 0, 0, 0, time.UTC),
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "nope", "envs.json"))
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(s.Envs) != 0 {
		t.Errorf("Envs = %+v, want empty", s.Envs)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "envs.json")
	s := &Store{Path: path}
	s.Add(sample())
	if err := s.Save(); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	// No temp file left behind next to the registry.
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 || entries[0].Name() != "envs.json" {
		t.Errorf("state dir = %v, want only envs.json", entries)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(got.Envs) != 1 {
		t.Fatalf("Envs = %+v, want 1", got.Envs)
	}
	want := sample()
	e := got.Envs[0]
	if e.Project != want.Project || e.Name != want.Name || e.Session != want.Session ||
		e.Branch != want.Branch || e.Path != want.Path || e.RepoDir != want.RepoDir ||
		e.Owner != want.Owner || e.Repo != want.Repo || !e.CreatedAt.Equal(want.CreatedAt) ||
		len(e.Issues) != 1 || e.Issues[0] != 59 || len(e.PRs) != 1 || e.PRs[0] != 61 {
		t.Errorf("round trip = %+v, want %+v", e, want)
	}
	raw, _ := os.ReadFile(path)
	for _, key := range []string{`"envs"`, `"project"`, `"session"`, `"repo_dir"`, `"created_at"`, `"issues"`, `"prs"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("file lacks key %s:\n%s", key, raw)
		}
	}
}

func TestLoadRejectsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "envs.json")
	os.WriteFile(path, []byte("{not json"), 0o644)
	if _, err := Load(path); err == nil {
		t.Error("expected error for corrupt file")
	}
}

func TestLookups(t *testing.T) {
	s := &Store{}
	e := s.Add(sample())
	other := s.Add(&Env{Project: "trade", Name: "other", Session: "we-trade-other", Branch: "other", Path: "/u/projects/trade/other", Owner: "axklim", Repo: "trade"})

	if s.BySession("we-trade-review_claude-file") != e || s.BySession("nope") != nil {
		t.Error("BySession")
	}
	if s.ByName("trade", "review_claude-file") != e || s.ByName("other", "review_claude-file") != nil {
		t.Error("ByName")
	}
	if s.ByBranch("trade", "other") != other || s.ByBranch("trade", "main") != nil {
		t.Error("ByBranch")
	}
	if s.ByPath("/u/projects/trade/other") != other || s.ByPath("/nope") != nil {
		t.Error("ByPath")
	}
	if s.ByIssue("axklim", "trade", 59) != e || s.ByIssue("axklim", "trade", 60) != nil || s.ByIssue("someone", "trade", 59) != nil {
		t.Error("ByIssue")
	}
	// GitHub owner/repo are case-insensitive.
	if s.ByPR("AxKlim", "Trade", 61) != e {
		t.Error("ByPR should ignore case")
	}
	got := s.Matching(func(x *Env) bool { return x.Project == "trade" })
	if len(got) != 2 {
		t.Errorf("Matching = %d, want 2", len(got))
	}
}

func TestRemove(t *testing.T) {
	s := &Store{}
	s.Add(sample())
	if !s.Remove("trade", "review_claude-file") {
		t.Fatal("Remove should report success")
	}
	if len(s.Envs) != 0 || s.Remove("trade", "review_claude-file") {
		t.Error("record should be gone")
	}
}

func TestLinkAddsNumbersOnceAndSkipsOnesOwnedElsewhere(t *testing.T) {
	s := &Store{}
	a := s.Add(&Env{Project: "trade", Name: "a", Owner: "axklim", Repo: "trade", Issues: []int{59}})
	b := s.Add(&Env{Project: "trade", Name: "b"})

	s.Link(b, "axklim", "trade", []int{59, 60}, []int{61, 61})
	if b.Owner != "axklim" || b.Repo != "trade" {
		t.Errorf("Link should record owner/repo on an env without one: %+v", b)
	}
	if len(b.Issues) != 1 || b.Issues[0] != 60 {
		t.Errorf("Issues = %v, want [60] (59 belongs to a)", b.Issues)
	}
	if len(b.PRs) != 1 || b.PRs[0] != 61 {
		t.Errorf("PRs = %v, want [61] (deduplicated)", b.PRs)
	}
	s.Link(a, "axklim", "trade", []int{59}, nil)
	if len(a.Issues) != 1 {
		t.Errorf("relinking the same number must not duplicate: %v", a.Issues)
	}
}

func TestDefaultPathHonoursXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg-state")
	if got := DefaultPath(); got != "/xdg-state/workenv/envs.json" {
		t.Errorf("DefaultPath() = %q", got)
	}
	t.Setenv("XDG_STATE_HOME", "")
	home, _ := os.UserHomeDir()
	if got, want := DefaultPath(), filepath.Join(home, ".local", "state", "workenv", "envs.json"); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make test` (or the docker one-liner with `./internal/state/`)
Expected: build failure — package `state` does not exist.

- [ ] **Step 3: Implement the package**

`internal/state/state.go`:

```go
// Package state persists the registry of work environments as JSON under
// the XDG state directory ($XDG_STATE_HOME/workenv/envs.json, defaulting to
// ~/.local/state). It replaces the earlier stateless scheme: a work
// environment records its branch, tmux session and worktree path, so none of
// them has to be re-derived from — or encoded in — a name.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Env is one recorded work environment, identified by (Project, Name).
type Env struct {
	Project string `json:"project"`
	Name    string `json:"name"`
	Session string `json:"session"`
	Branch  string `json:"branch"`
	Path    string `json:"path"`
	RepoDir string `json:"repo_dir"`
	// Owner and Repo name the GitHub repository when known; issue and PR
	// numbers only mean something together with them.
	Owner     string    `json:"owner,omitempty"`
	Repo      string    `json:"repo,omitempty"`
	Issues    []int     `json:"issues,omitempty"`
	PRs       []int     `json:"prs,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// Store is the registry: the file it lives in plus its decoded contents.
type Store struct {
	Path string
	Envs []*Env
}

// file is the on-disk shape; the wrapper object leaves room for versioning.
type file struct {
	Envs []*Env `json:"envs"`
}

// DefaultPath returns the registry location following XDG notation.
func DefaultPath() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "workenv", "envs.json")
}

// Load reads the registry at path; a missing file yields an empty store.
func Load(path string) (*Store, error) {
	s := &Store{Path: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return nil, err
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	s.Envs = f.Envs
	return s, nil
}

// Save writes the registry atomically (temp file + rename), so a crash can
// never leave a truncated file behind.
func (s *Store) Save() error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	envs := s.Envs
	if envs == nil {
		envs = []*Env{}
	}
	data, err := json.MarshalIndent(file{Envs: envs}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".envs-*.json")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(append(data, '\n'))
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), s.Path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

func (s *Store) find(pred func(*Env) bool) *Env {
	for _, e := range s.Envs {
		if pred(e) {
			return e
		}
	}
	return nil
}

// Matching returns every environment satisfying pred.
func (s *Store) Matching(pred func(*Env) bool) []*Env {
	var out []*Env
	for _, e := range s.Envs {
		if pred(e) {
			out = append(out, e)
		}
	}
	return out
}

func (s *Store) BySession(session string) *Env {
	return s.find(func(e *Env) bool { return e.Session == session })
}

func (s *Store) ByName(project, name string) *Env {
	return s.find(func(e *Env) bool { return e.Project == project && e.Name == name })
}

func (s *Store) ByBranch(project, branch string) *Env {
	return s.find(func(e *Env) bool { return e.Project == project && e.Branch == branch })
}

func (s *Store) ByPath(path string) *Env {
	return s.find(func(e *Env) bool { return e.Path == path })
}

// ByIssue finds the environment holding issue n of owner/repo (GitHub names
// are case-insensitive).
func (s *Store) ByIssue(owner, repo string, n int) *Env {
	return s.find(func(e *Env) bool { return e.sameRepo(owner, repo) && slices.Contains(e.Issues, n) })
}

func (s *Store) ByPR(owner, repo string, n int) *Env {
	return s.find(func(e *Env) bool { return e.sameRepo(owner, repo) && slices.Contains(e.PRs, n) })
}

func (e *Env) sameRepo(owner, repo string) bool {
	return strings.EqualFold(e.Owner, owner) && strings.EqualFold(e.Repo, repo)
}

// Add appends env and returns it.
func (s *Store) Add(env *Env) *Env {
	s.Envs = append(s.Envs, env)
	return env
}

// Remove drops the environment (project, name), reporting whether it existed.
func (s *Store) Remove(project, name string) bool {
	before := len(s.Envs)
	s.Envs = slices.DeleteFunc(s.Envs, func(e *Env) bool { return e.Project == project && e.Name == name })
	return len(s.Envs) != before
}

// Link records issue and PR numbers of owner/repo on env, skipping any that
// already belong to another environment of that repository: a number maps
// to at most one work environment. An env without owner/repo adopts them.
func (s *Store) Link(env *Env, owner, repo string, issues, prs []int) {
	if owner == "" || repo == "" {
		return
	}
	if env.Owner == "" {
		env.Owner, env.Repo = owner, repo
	}
	if !env.sameRepo(owner, repo) {
		return
	}
	for _, n := range issues {
		if other := s.ByIssue(owner, repo, n); other == nil || other == env {
			env.Issues = addSorted(env.Issues, n)
		}
	}
	for _, n := range prs {
		if other := s.ByPR(owner, repo, n); other == nil || other == env {
			env.PRs = addSorted(env.PRs, n)
		}
	}
}

func addSorted(nums []int, n int) []int {
	if slices.Contains(nums, n) {
		return nums
	}
	nums = append(nums, n)
	slices.Sort(nums)
	return nums
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `make check`
Expected: all packages `ok`, gofmt and vet clean.

- [ ] **Step 5: Commit**

```bash
git add internal/state
git commit -m "feat(state): add JSON registry of work environments

Persist each we (project, name, session, branch, worktree path, repo dir,
GitHub owner/repo, issue and PR numbers) in \$XDG_STATE_HOME/workenv/envs.json,
written atomically. Lookups by session, name, branch, path, issue and PR;
Link keeps a number on at most one environment per repository.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: `internal/gitx` — repo root, bare container, head fetch, current branch

**Files:**
- Modify: `internal/gitx/gitx.go`
- Test: `internal/gitx/gitx_test.go`

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces (used by Task 4):
  - `func (g Git) RepoRoot(dir string) string` — repository root containing
    dir from any layout; `""` when not in a repo.
  - `func (g Git) IsBareContainer(repoDir string) bool`
  - `func (g Git) EnsureOriginBranch(repoDir, branch string) error`
  - `func (g Git) CurrentBranch(worktree string) string` — `""` if detached
    or not a repo.
  - `func (g Git) Prune(repoDir string) error`
  - `TopLevel` is left in place; Task 4 deletes it once `we` no longer
    uses it.

- [ ] **Step 1: Write the failing tests**

Append to `internal/gitx/gitx_test.go` (keep the existing tests; add the
import block):

```go
import (
	"errors"
	"testing"

	"workenv/internal/execx"
)

var errFake = errors.New("fake failure")

func TestRepoRootFromCommonDir(t *testing.T) {
	tests := []struct {
		name, common, want string
	}{
		{"normal clone", "/u/projects/workenv/.git", "/u/projects/workenv"},
		{"worktree of a bare container", "/u/projects/trade/.git", "/u/projects/trade"},
		{".bare container", "/u/projects/trade/.bare", "/u/projects/trade"},
		{"bare repo.git", "/u/projects/trade.git", "/u/projects/trade.git"},
	}
	for _, tt := range tests {
		f := &execx.Fake{Responses: []execx.FakeResponse{
			{Prefix: "git rev-parse --path-format=absolute --git-common-dir", Out: tt.common},
		}}
		if got := (Git{R: f}).RepoRoot("/somewhere"); got != tt.want {
			t.Errorf("%s: RepoRoot = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestRepoRootOutsideRepository(t *testing.T) {
	f := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "git rev-parse --path-format=absolute --git-common-dir", Err: errFake},
	}}
	if got := (Git{R: f}).RepoRoot("/tmp"); got != "" {
		t.Errorf("RepoRoot = %q, want empty", got)
	}
	// An unmatched fake call returns "" — must not be mistaken for a root.
	if got := (Git{R: &execx.Fake{}}).RepoRoot("/tmp"); got != "" {
		t.Errorf("RepoRoot with empty output = %q, want empty", got)
	}
}

func TestIsBareContainer(t *testing.T) {
	bare := &execx.Fake{Responses: []execx.FakeResponse{{Prefix: "git rev-parse --is-bare-repository", Out: "true"}}}
	if !(Git{R: bare}).IsBareContainer("/u/projects/trade") {
		t.Error("bare repo at <dir>/.git should be a container")
	}
	if (Git{R: bare}).IsBareContainer("/u/projects/trade.git") {
		t.Error("a bare repo.git directory has no container")
	}
	if len(bare.Calls) != 1 {
		t.Errorf("repo.git should be decided without a git call, got %v", bare.Joined())
	}
	normal := &execx.Fake{Responses: []execx.FakeResponse{{Prefix: "git rev-parse --is-bare-repository", Out: "false"}}}
	if (Git{R: normal}).IsBareContainer("/u/projects/workenv") {
		t.Error("normal clone is not a container")
	}
}

func TestEnsureOriginBranchFetchesOnlyWhenMissing(t *testing.T) {
	missing := &execx.Fake{Responses: []execx.FakeResponse{{Prefix: "git show-ref", Err: errFake}}}
	if err := (Git{R: missing}).EnsureOriginBranch("/repo", "fix/crash"); err != nil {
		t.Fatalf("EnsureOriginBranch error: %v", err)
	}
	want := "git fetch origin +refs/heads/fix/crash:refs/remotes/origin/fix/crash"
	found := false
	for _, c := range missing.Joined() {
		if c == want {
			found = true
		}
	}
	if !found {
		t.Errorf("missing %q in %v", want, missing.Joined())
	}
	present := &execx.Fake{} // show-ref succeeds by default
	if err := (Git{R: present}).EnsureOriginBranch("/repo", "fix/crash"); err != nil {
		t.Fatalf("EnsureOriginBranch error: %v", err)
	}
	for _, c := range present.Joined() {
		if len(c) >= 9 && c[:9] == "git fetch" {
			t.Errorf("unexpected fetch when the branch exists: %v", present.Joined())
		}
	}
}

func TestCurrentBranch(t *testing.T) {
	f := &execx.Fake{Responses: []execx.FakeResponse{{Prefix: "git symbolic-ref --short -q HEAD", Out: "feature-x"}}}
	if got := (Git{R: f}).CurrentBranch("/wt"); got != "feature-x" {
		t.Errorf("CurrentBranch = %q", got)
	}
	if f.Calls[0].Dir != "/wt" {
		t.Errorf("must run inside the worktree, ran in %q", f.Calls[0].Dir)
	}
	detached := &execx.Fake{Responses: []execx.FakeResponse{{Prefix: "git symbolic-ref", Err: errFake}}}
	if got := (Git{R: detached}).CurrentBranch("/wt"); got != "" {
		t.Errorf("detached CurrentBranch = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make test`
Expected: compile errors — `RepoRoot`, `IsBareContainer`,
`EnsureOriginBranch`, `CurrentBranch` undefined.

- [ ] **Step 3: Implement**

In `internal/gitx/gitx.go`, update the package comment and add the methods.
Package comment becomes:

```go
// Package gitx wraps the git operations workenv needs: locating project
// repositories from any layout (normal clone, bare container, worktree),
// bare-cloning missing ones (with fetch-refspec setup so origin-tracking
// branches work), and managing worktrees.
```

Add after `TopLevel`:

```go
// RepoRoot returns the repository containing dir, whatever the layout: the
// parent of a ".git" (or ".bare") common dir — a normal clone or a bare
// container, seen from the checkout, a worktree or the container itself —
// or the common dir for a bare "repo.git". "" when dir is not in a repo.
func (g Git) RepoRoot(dir string) string {
	out, err := g.R.Output(dir, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || out == "" {
		return ""
	}
	common := filepath.Clean(out)
	if base := filepath.Base(common); base == ".git" || base == ".bare" {
		return filepath.Dir(common)
	}
	return common
}

// IsBareContainer reports whether repoDir holds a bare repository as its
// ".git" — the layout where worktrees sit next to it (~/projects/trade/.git
// + ~/projects/trade/main). A bare "repo.git" directory is bare too but has
// no container, so it is excluded.
func (g Git) IsBareContainer(repoDir string) bool {
	if strings.HasSuffix(filepath.Base(repoDir), ".git") {
		return false
	}
	out, err := g.R.Output(repoDir, "git", "rev-parse", "--is-bare-repository")
	return err == nil && out == "true"
}

// CurrentBranch returns the branch checked out in worktree, "" when HEAD is
// detached or the directory is not a checkout.
func (g Git) CurrentBranch(worktree string) string {
	out, err := g.R.Output(worktree, "git", "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return out
}
```

Add after `FetchPRBranch`:

```go
// EnsureOriginBranch makes refs/remotes/origin/<branch> available for a
// branch that exists on origin but was pushed after the last fetch. The
// explicit refspec also works in bare repos that have no fetch refspec.
func (g Git) EnsureOriginBranch(repoDir, branch string) error {
	if g.BranchExists(repoDir, "refs/heads/"+branch) || g.BranchExists(repoDir, "refs/remotes/origin/"+branch) {
		return nil
	}
	return g.R.Run(repoDir, "git", "fetch", "origin",
		fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch))
}

// Prune forgets worktrees whose directories are gone.
func (g Git) Prune(repoDir string) error {
	return g.R.Run(repoDir, "git", "worktree", "prune")
}
```

And make `RemoveWorktree` use it:

```go
func (g Git) RemoveWorktree(repoDir, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	if err := g.R.Run(repoDir, "git", args...); err != nil {
		return err
	}
	return g.Prune(repoDir)
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `make check`
Expected: all `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/gitx
git commit -m "feat(gitx): repo root from any layout, bare-container check, head fetch

RepoRoot resolves the repository from a checkout, a worktree or a bare
container via --git-common-dir; IsBareContainer tells the <repo>/.git
layout apart from a bare repo.git; EnsureOriginBranch fetches a same-repo
PR head that was pushed after the last fetch; CurrentBranch reads a
worktree's HEAD; Prune is extracted from RemoveWorktree.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: `gh` links, `naming` without prefixes, `config` default

**Files:**
- Modify: `internal/gh/gh.go`, `internal/gh/gh_test.go`
- Modify: `internal/naming/naming.go`, `internal/naming/naming_test.go`
- Modify: `internal/config/config.go`, `internal/config/config_test.go`

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces (used by Task 4):
  - `gh.Ref{Number int; Repository{Name string; Owner{Login string}}}` with
    `func (r Ref) In(owner, repo string) bool`
  - `gh.Issue{Number int; Title string; LinkedPRs []Ref}`
  - `gh.PR{Number int; Title, HeadRefName string; IsCrossRepository bool; LinkedIssues []Ref}`
  - `naming.BranchForIssue(num int, title string) string` — the title
    slug, `issue-N` only when the slug is empty.
  - `naming.PRBranch(num int) string` — `pr-N` (renamed from `PRName`).
  - `config.Config.WorktreesDir` defaults to `""`.

- [ ] **Step 1: Write the failing tests**

Replace `internal/gh/gh_test.go` with:

```go
package gh

import (
	"testing"

	"workenv/internal/execx"
)

func TestIssue(t *testing.T) {
	f := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "gh issue view 59", Out: `{"number":59,"title":"Review CLAUDE.md file",` +
			`"closedByPullRequestsReferences":[{"number":61,"repository":{"name":"trade","owner":{"login":"axklim"}}}]}`},
	}}
	issue, err := Client{R: f}.Issue("axklim", "trade", 59)
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}
	if issue.Number != 59 || issue.Title != "Review CLAUDE.md file" {
		t.Errorf("issue = %+v", issue)
	}
	if len(issue.LinkedPRs) != 1 || issue.LinkedPRs[0].Number != 61 || !issue.LinkedPRs[0].In("axklim", "trade") {
		t.Errorf("LinkedPRs = %+v", issue.LinkedPRs)
	}
	if got := f.Joined()[0]; got != "gh issue view 59 -R axklim/trade --json number,title,closedByPullRequestsReferences" {
		t.Errorf("command = %q", got)
	}
}

func TestPR(t *testing.T) {
	f := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "gh pr view 61", Out: `{"number":61,"title":"docs: CLAUDE.md","headRefName":"review_claude-file",` +
			`"isCrossRepository":false,"closingIssuesReferences":[{"number":59,"repository":{"name":"trade","owner":{"login":"axklim"}}}]}`},
	}}
	pr, err := Client{R: f}.PR("axklim", "trade", 61)
	if err != nil {
		t.Fatalf("PR error: %v", err)
	}
	if pr.Number != 61 || pr.HeadRefName != "review_claude-file" || pr.IsCrossRepository {
		t.Errorf("pr = %+v", pr)
	}
	if len(pr.LinkedIssues) != 1 || pr.LinkedIssues[0].Number != 59 {
		t.Errorf("LinkedIssues = %+v", pr.LinkedIssues)
	}
	if got := f.Joined()[0]; got != "gh pr view 61 -R axklim/trade --json number,title,headRefName,isCrossRepository,closingIssuesReferences" {
		t.Errorf("command = %q", got)
	}
}

func TestRefInIgnoresCaseAndOtherRepos(t *testing.T) {
	var r Ref
	r.Number = 7
	r.Repository.Name = "Trade"
	r.Repository.Owner.Login = "AxKlim"
	if !r.In("axklim", "trade") {
		t.Error("In should be case-insensitive")
	}
	if r.In("axklim", "other") {
		t.Error("In must reject another repository")
	}
}
```

In `internal/naming/naming_test.go`, replace `TestBranchForIssue` and
`TestPRName` with:

```go
func TestBranchForIssueUsesTitleSlugWithoutPrefix(t *testing.T) {
	tests := []struct {
		num   int
		title string
		want  string
	}{
		{123, "Add Kafka publisher", "add-kafka-publisher"},
		{59, "Review CLAUDE.md file", "review-claude-md-file"},
		{7, "", "issue-7"},
		{42, "!!!", "issue-42"},
	}
	for _, tt := range tests {
		if got := BranchForIssue(tt.num, tt.title); got != tt.want {
			t.Errorf("BranchForIssue(%d, %q) = %q, want %q", tt.num, tt.title, got, tt.want)
		}
	}
}

func TestPRBranch(t *testing.T) {
	if got := PRBranch(456); got != "pr-456" {
		t.Errorf("PRBranch(456) = %q, want %q", got, "pr-456")
	}
}
```

In `internal/config/config_test.go`: in `TestDefaults` replace the
`WorktreesDir` assertion with

```go
	if cfg.WorktreesDir != "" {
		t.Errorf("WorktreesDir = %q, want empty (standard placement)", cfg.WorktreesDir)
	}
```

and delete `TestWorktreesDirFollowsProjectsDir` entirely. `TestParseOverrides`
keeps asserting `/data/wt`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make test`
Expected: `gh` fails (fields/`In` missing, wrong `--json` list); `naming`
fails (`PRBranch` undefined, `BranchForIssue` still prefixes); `config`
fails (`WorktreesDir` still `<projects>/.we`).

- [ ] **Step 3: Implement**

`internal/gh/gh.go` — replace the types and commands:

```go
// Package gh fetches GitHub issue/PR metadata through the gh CLI, including
// the links between them (a PR "closes" an issue via keywords or the
// Development sidebar; GitHub reports it from both sides).
package gh

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"workenv/internal/execx"
)

type Client struct {
	R execx.Runner
}

// Ref is a linked issue or PR as gh reports it. Links can point at another
// repository, so the repository travels with the number.
type Ref struct {
	Number     int `json:"number"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
}

// In reports whether the reference lives in owner/repo (case-insensitively,
// like GitHub itself).
func (r Ref) In(owner, repo string) bool {
	return strings.EqualFold(r.Repository.Owner.Login, owner) && strings.EqualFold(r.Repository.Name, repo)
}

type Issue struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	LinkedPRs []Ref  `json:"closedByPullRequestsReferences"`
}

type PR struct {
	Number            int    `json:"number"`
	Title             string `json:"title"`
	HeadRefName       string `json:"headRefName"`
	IsCrossRepository bool   `json:"isCrossRepository"`
	LinkedIssues      []Ref  `json:"closingIssuesReferences"`
}

func (c Client) Issue(owner, repo string, num int) (Issue, error) {
	out, err := c.R.Output("", "gh", "issue", "view", strconv.Itoa(num),
		"-R", owner+"/"+repo, "--json", "number,title,closedByPullRequestsReferences")
	if err != nil {
		return Issue{}, err
	}
	var issue Issue
	if err := json.Unmarshal([]byte(out), &issue); err != nil {
		return Issue{}, fmt.Errorf("parsing gh issue view output: %w", err)
	}
	return issue, nil
}

func (c Client) PR(owner, repo string, num int) (PR, error) {
	out, err := c.R.Output("", "gh", "pr", "view", strconv.Itoa(num),
		"-R", owner+"/"+repo, "--json", "number,title,headRefName,isCrossRepository,closingIssuesReferences")
	if err != nil {
		return PR{}, err
	}
	var pr PR
	if err := json.Unmarshal([]byte(out), &pr); err != nil {
		return PR{}, fmt.Errorf("parsing gh pr view output: %w", err)
	}
	return pr, nil
}
```

`internal/naming/naming.go` — package comment and the two functions:

```go
// Package naming derives default identifiers (branch names from issue
// titles, tmux session names) and sanitises user-supplied ones. Nothing is
// encoded in a name any more: the state registry records what belongs
// together.
package naming
```

```go
// BranchForIssue is the default branch for an issue: the title slug, so the
// branch reads like the work. Only a title with no usable characters falls
// back to issue-N.
func BranchForIssue(num int, title string) string {
	if slug := Slugify(title); slug != "" {
		return slug
	}
	return fmt.Sprintf("issue-%d", num)
}

// PRBranch is the local branch a fork PR is materialised on (its head branch
// does not exist on origin).
func PRBranch(num int) string {
	return fmt.Sprintf("pr-%d", num)
}
```

Also fix the `SessionName` doc comment's second sentence to read
`// tmux target syntax reserves ':' and '.', so unsafe characters become dashes.`
(no behaviour change).

`internal/config/config.go`:

- Field comment:
  ```go
  	// WorktreesDir, when set, is a root under which every worktree is
  	// created as <WorktreesDir>/<project>/<name>. Empty (the default) means
  	// standard placement: inside a bare container next to its other
  	// worktrees, otherwise the sibling directory <repo>.<name>.
  	WorktreesDir string
  ```
- In `parse`, delete the two lines
  ```go
  	if worktreesDir == "" {
  		worktreesDir = filepath.Join(cfg.ProjectsDir, ".we")
  	}
  ```
  and keep `cfg.WorktreesDir = expandHome(worktreesDir, home)` (an empty
  string stays empty).

- [ ] **Step 4: Run the tests to verify they pass**

Run: `make check`
Expected: `gh`, `naming`, `config` `ok`; `we` will now FAIL TO COMPILE
(`naming.PRName` gone) — that is expected and fixed in Task 4. To confirm
the three packages alone: run the docker one-liner with
`./internal/gh/ ./internal/naming/ ./internal/config/`.

- [ ] **Step 5: Commit**

```bash
git add internal/gh internal/naming internal/config
git commit -m "feat: gh issue/PR links, prefix-free branch names, unset worktrees_dir

gh: Issue carries closedByPullRequestsReferences, PR carries
isCrossRepository and closingIssuesReferences. naming: BranchForIssue is
the title slug (issue-N only for an empty slug); PRName becomes PRBranch.
config: worktrees_dir has no default any more — empty means standard
placement, decided by the we package.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: `internal/we` — open / attach / delete / list on the registry

**Files:**
- Rewrite: `internal/we/we.go`
- Rewrite: `internal/we/we_test.go` (keep `internal/we/helpers_test.go`)
- Modify: `internal/gitx/gitx.go` — delete `TopLevel` (no longer used)

**Interfaces:**
- Consumes: everything listed under Tasks 1–3, plus the unchanged
  `execx.Runner`, `tmuxx.Tmux` (`Has`, `New(name, dir, project, weName)`,
  `RunInFirstWindow`, `Kill`, `List`, `HasClients`, `SwitchClient`),
  `gitx.Git` (`FindProjectDir`, `CloneBare`, `WorktreeForBranch`,
  `BranchExists`, `AddWorktree`, `FetchPRBranch`, `RemoveWorktree`,
  `DeleteBranch`, `OriginGitHubRepo`), `target.Target`.
- Produces (used by Task 5):
  - `type Env struct { Cfg config.Config; R execx.Runner; GOOS, Cwd string; InsideTmux bool; StatePath string }`
  - `type OpenOptions struct { Target target.Target; Project, Name, Branch string; AttachOnly, NoTerminal bool }`
  - `type OpenResult struct { Project, Name, Branch, Path, Session, RepoDir string; Created bool }`
  - `type DeleteOptions struct { Force, DeleteBranch, KeepWorktree bool }`
  - `type Item struct { Project, Name, Branch, Path, Session, SessionState string; Issues, PRs []int }`
  - `func (e *Env) Open(opts OpenOptions) (OpenResult, error)`
  - `func (e *Env) Delete(t target.Target, project string, opts DeleteOptions) error`
  - `func (e *Env) List() ([]Item, error)`
  - `func (e *Env) AttachRemote(host, session string) error` (unchanged)

- [ ] **Step 1: Write the failing tests**

Replace `internal/we/we_test.go` with:

```go
package we

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"workenv/internal/config"
	"workenv/internal/execx"
	"workenv/internal/state"
	"workenv/internal/target"
)

// newTestEnv builds an Env over a temp projects dir holding one project
// ("proj", a directory with a .git marker), a scripted fake runner and a
// registry path under the temp root.
func newTestEnv(t *testing.T, fake *execx.Fake) (*Env, string) {
	t.Helper()
	root := t.TempDir()
	projects := filepath.Join(root, "projects")
	repo := filepath.Join(projects, "proj")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{ProjectsDir: projects, ClaudeCmd: "claude", RemoteWe: "we"}
	return &Env{Cfg: cfg, R: fake, GOOS: "darwin", Cwd: root, StatePath: filepath.Join(root, "state", "envs.json")}, repo
}

func hasCall(f *execx.Fake, want string) bool {
	return slices.ContainsFunc(f.Joined(), func(c string) bool {
		return strings.HasPrefix(c, want)
	})
}

func loadState(t *testing.T, env *Env) *state.Store {
	t.Helper()
	st, err := state.Load(env.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// seed writes envs to the registry; paths that should exist on disk are the
// caller's business.
func seed(t *testing.T, env *Env, envs ...*state.Env) {
	t.Helper()
	st := &state.Store{Path: env.StatePath}
	for _, x := range envs {
		st.Add(x)
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func name(n string) target.Target { return target.Target{Kind: target.KindName, Name: n} }
func issue(n int) target.Target {
	return target.Target{Kind: target.KindIssue, Owner: "acme", Repo: "proj", Number: n}
}
func pull(n int) target.Target {
	return target.Target{Kind: target.KindPR, Owner: "acme", Repo: "proj", Number: n}
}

// newBranchResponses script "no worktree, branch unknown, default branch
// main, no session" — the create-from-scratch path.
var newBranchResponses = []execx.FakeResponse{
	{Prefix: "git show-ref", Err: errFake},
	{Prefix: "git symbolic-ref --short refs/remotes/origin/HEAD", Err: errFake},
	{Prefix: "git symbolic-ref --short HEAD", Out: "main"},
	{Prefix: "tmux has-session", Err: errFake},
}

func TestOpenPlainNameCreatesWorktreeSessionAndRecord(t *testing.T) {
	fake := &execx.Fake{Responses: newBranchResponses}
	env, repo := newTestEnv(t, fake)

	res, err := env.Open(OpenOptions{Target: name("feature-123"), Project: "proj", NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	// Normal clone (not a bare container): sibling directory <repo>.<name>.
	wtPath := filepath.Join(env.Cfg.ProjectsDir, "proj.feature-123")
	if res.Project != "proj" || res.Branch != "feature-123" || res.Path != wtPath ||
		res.Session != "we-proj-feature-123" || res.RepoDir != repo || !res.Created {
		t.Errorf("result = %+v", res)
	}
	for _, want := range []string{
		"git worktree add -b feature-123 " + wtPath + " main",
		"tmux new-session -d -s we-proj-feature-123 -c " + wtPath,
		"tmux set-option -t we-proj-feature-123 @workenv 1",
		"tmux send-keys -t we-proj-feature-123 claude Enter",
	} {
		if !hasCall(fake, want) {
			t.Errorf("missing call %q in:\n%s", want, strings.Join(fake.Joined(), "\n"))
		}
	}
	rec := loadState(t, env).ByName("proj", "feature-123")
	if rec == nil || rec.Branch != "feature-123" || rec.Path != wtPath || rec.RepoDir != repo || rec.Session != "we-proj-feature-123" {
		t.Errorf("record = %+v", rec)
	}
}

func TestOpenPlacesWorktreeInsideBareContainer(t *testing.T) {
	fake := &execx.Fake{Responses: append([]execx.FakeResponse{
		{Prefix: "git rev-parse --is-bare-repository", Out: "true"},
	}, newBranchResponses...)}
	env, repo := newTestEnv(t, fake)

	res, err := env.Open(OpenOptions{Target: name("feature-123"), Project: "proj", NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if want := filepath.Join(repo, "feature-123"); res.Path != want {
		t.Errorf("Path = %q, want %q (inside the container)", res.Path, want)
	}
}

func TestOpenHonoursConfiguredWorktreesDir(t *testing.T) {
	fake := &execx.Fake{Responses: newBranchResponses}
	env, _ := newTestEnv(t, fake)
	env.Cfg.WorktreesDir = filepath.Join(env.Cwd, "wt")

	res, err := env.Open(OpenOptions{Target: name("feature-123"), Project: "proj", NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if want := filepath.Join(env.Cfg.WorktreesDir, "proj", "feature-123"); res.Path != want {
		t.Errorf("Path = %q, want %q", res.Path, want)
	}
}

func TestOpenAdoptsExistingWorktreeAndSession(t *testing.T) {
	existing := t.TempDir()
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "git worktree list", Out: "worktree " + existing + "\nHEAD abc\nbranch refs/heads/feature-123\n"},
		// has-session succeeds (default fake response is success).
	}}
	env, _ := newTestEnv(t, fake)

	res, err := env.Open(OpenOptions{Target: name("feature-123"), Project: "proj", NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Path != existing {
		t.Errorf("Path = %q, want the existing worktree %q", res.Path, existing)
	}
	for _, forbidden := range []string{"git worktree add", "tmux new-session", "tmux send-keys"} {
		if hasCall(fake, forbidden) {
			t.Errorf("unexpected call %q — existing worktree/session must be reused", forbidden)
		}
	}
	if rec := loadState(t, env).ByName("proj", "feature-123"); rec == nil || rec.Path != existing {
		t.Errorf("adopted worktree must be recorded: %+v", rec)
	}
}

func TestOpenIssueDerivesBranchFromTitleWithoutPrefix(t *testing.T) {
	fake := &execx.Fake{Responses: append([]execx.FakeResponse{
		{Prefix: "gh issue view 123", Out: `{"number":123,"title":"Add Kafka publisher","closedByPullRequestsReferences":[]}`},
	}, newBranchResponses...)}
	env, _ := newTestEnv(t, fake)

	res, err := env.Open(OpenOptions{Target: issue(123), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Branch != "add-kafka-publisher" || res.Name != "add-kafka-publisher" || res.Session != "we-proj-add-kafka-publisher" {
		t.Errorf("res = %+v", res)
	}
	if !hasCall(fake, "git worktree add -b add-kafka-publisher") {
		t.Errorf("missing worktree add:\n%s", strings.Join(fake.Joined(), "\n"))
	}
	rec := loadState(t, env).ByIssue("acme", "proj", 123)
	if rec == nil || rec.Owner != "acme" || rec.Repo != "proj" {
		t.Errorf("issue must be recorded on the environment: %+v", rec)
	}
}

func TestOpenIssueUsesLinkedPRBranch(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "gh issue view 59", Out: `{"number":59,"title":"Review CLAUDE.md file",` +
			`"closedByPullRequestsReferences":[{"number":61,"repository":{"name":"proj","owner":{"login":"acme"}}}]}`},
		{Prefix: "gh pr view 61", Out: `{"number":61,"headRefName":"review_claude-file","isCrossRepository":false,` +
			`"closingIssuesReferences":[{"number":59,"repository":{"name":"proj","owner":{"login":"acme"}}}]}`},
		{Prefix: "git show-ref --verify --quiet refs/heads/review_claude-file", Err: errFake},
		// The head branch is on origin already: no fetch needed.
		{Prefix: "git show-ref --verify --quiet refs/remotes/origin/review_claude-file", Out: ""},
		{Prefix: "tmux has-session", Err: errFake},
	}}
	env, _ := newTestEnv(t, fake)

	res, err := env.Open(OpenOptions{Target: issue(59), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Branch != "review_claude-file" || res.Name != "review_claude-file" || res.Session != "we-proj-review_claude-file" {
		t.Errorf("res = %+v", res)
	}
	if !hasCall(fake, "git worktree add --track -b review_claude-file") {
		t.Errorf("missing tracking worktree add:\n%s", strings.Join(fake.Joined(), "\n"))
	}
	if hasCall(fake, "git fetch") {
		t.Error("no fetch when origin/<head> exists")
	}
	rec := loadState(t, env).ByIssue("acme", "proj", 59)
	if rec == nil || !slices.Equal(rec.PRs, []int{61}) {
		t.Errorf("issue and PR must both be recorded: %+v", rec)
	}
}

func TestOpenPRThenIssueShareOneEnvironment(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "gh pr view 61", Out: `{"number":61,"headRefName":"review_claude-file","isCrossRepository":false,` +
			`"closingIssuesReferences":[{"number":59,"repository":{"name":"proj","owner":{"login":"acme"}}}]}`},
		{Prefix: "git show-ref --verify --quiet refs/heads/review_claude-file", Err: errFake},
		{Prefix: "git show-ref --verify --quiet refs/remotes/origin/review_claude-file", Out: ""},
		{Prefix: "tmux has-session", Err: errFake},
	}}
	env, _ := newTestEnv(t, fake)

	first, err := env.Open(OpenOptions{Target: pull(61), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open PR error: %v", err)
	}
	mkdir(t, first.Path) // the fake runner never creates the worktree
	fake.Calls = nil
	second, err := env.Open(OpenOptions{Target: issue(59), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open issue error: %v", err)
	}
	if second.Session != first.Session || second.Created {
		t.Errorf("issue must resolve to the PR's environment: first=%+v second=%+v", first, second)
	}
	if hasCall(fake, "gh issue view") {
		t.Error("a state hit must not call GitHub")
	}
	if hasCall(fake, "git worktree add") {
		t.Error("must not create a second worktree")
	}
}

func TestOpenIssueThenPRShareOneEnvironment(t *testing.T) {
	fake := &execx.Fake{Responses: append([]execx.FakeResponse{
		{Prefix: "gh issue view 59", Out: `{"number":59,"title":"Review CLAUDE.md file","closedByPullRequestsReferences":[]}`},
		// The PR was opened from the issue's branch.
		{Prefix: "gh pr view 61", Out: `{"number":61,"headRefName":"review-claude-md-file","isCrossRepository":false,` +
			`"closingIssuesReferences":[{"number":59,"repository":{"name":"proj","owner":{"login":"acme"}}}]}`},
	}, newBranchResponses...)}
	env, _ := newTestEnv(t, fake)

	first, err := env.Open(OpenOptions{Target: issue(59), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open issue error: %v", err)
	}
	mkdir(t, first.Path) // the fake runner never creates the worktree
	fake.Calls = nil
	second, err := env.Open(OpenOptions{Target: pull(61), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open PR error: %v", err)
	}
	if second.Session != first.Session || second.Created {
		t.Errorf("PR on the issue's branch must resolve to the same environment: %+v vs %+v", first, second)
	}
	if hasCall(fake, "git worktree add") {
		t.Error("must not create a second worktree")
	}
	rec := loadState(t, env).ByPR("acme", "proj", 61)
	if rec == nil || !slices.Equal(rec.Issues, []int{59}) {
		t.Errorf("PR must be linked onto the issue's record: %+v", rec)
	}
}

func TestOpenBySessionName(t *testing.T) {
	fake := &execx.Fake{}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "proj", Name: "feature-123", Session: "we-proj-feature-123", Branch: "feature-123", Path: path, RepoDir: repo})

	res, err := env.Open(OpenOptions{Target: name("we-proj-feature-123"), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Name != "feature-123" || res.Path != path || res.Created {
		t.Errorf("res = %+v", res)
	}
	if hasCall(fake, "git worktree add") {
		t.Error("existing environment must not be recreated")
	}
}

func TestAttachNeverCreates(t *testing.T) {
	fake := &execx.Fake{}
	env, _ := newTestEnv(t, fake)

	_, err := env.Open(OpenOptions{Target: name("nope"), Project: "proj", AttachOnly: true, NoTerminal: true})
	if err == nil || !strings.Contains(err.Error(), "we open") {
		t.Fatalf("attach of an unknown target must fail pointing at we open, got %v", err)
	}
	if hasCall(fake, "git worktree add") || hasCall(fake, "tmux new-session") {
		t.Error("attach must not create anything")
	}
	if len(loadState(t, env).Envs) != 0 {
		t.Error("attach must not record anything")
	}
	// Issue targets are resolved through GitHub links but still not created.
	fake.Responses = []execx.FakeResponse{
		{Prefix: "gh issue view 5", Out: `{"number":5,"title":"New thing","closedByPullRequestsReferences":[]}`},
	}
	_, err = env.Open(OpenOptions{Target: issue(5), AttachOnly: true, NoTerminal: true})
	if err == nil || !strings.Contains(err.Error(), "we open") {
		t.Fatalf("attach of an unknown issue must fail, got %v", err)
	}
	if hasCall(fake, "git worktree add") {
		t.Error("attach must not create a worktree for an issue")
	}
}

func TestOpenRefreshesRenamedBranchFromWorktree(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "git symbolic-ref --short -q HEAD", Out: "renamed"},
	}}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "proj", Name: "feature-123", Session: "we-proj-feature-123", Branch: "old", Path: path, RepoDir: repo})

	res, err := env.Open(OpenOptions{Target: name("feature-123"), Project: "proj", NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Branch != "renamed" {
		t.Errorf("Branch = %q, want the worktree's current branch", res.Branch)
	}
	if rec := loadState(t, env).ByName("proj", "feature-123"); rec.Branch != "renamed" {
		t.Errorf("stored branch = %q, want refreshed", rec.Branch)
	}
}

func TestOpenPRFindsRenamedBranchThroughGitWorktree(t *testing.T) {
	fake := &execx.Fake{}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	fake.Responses = []execx.FakeResponse{
		{Prefix: "gh pr view 61", Out: `{"number":61,"headRefName":"new","isCrossRepository":false,"closingIssuesReferences":[]}`},
		{Prefix: "git worktree list", Out: "worktree " + path + "\nHEAD abc\nbranch refs/heads/new\n"},
		{Prefix: "git symbolic-ref --short -q HEAD", Out: "new"},
	}
	seed(t, env, &state.Env{Project: "proj", Name: "feature-123", Session: "we-proj-feature-123", Branch: "old", Path: path, RepoDir: repo, Owner: "acme", Repo: "proj"})

	res, err := env.Open(OpenOptions{Target: pull(61), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Session != "we-proj-feature-123" || res.Created || res.Branch != "new" {
		t.Errorf("res = %+v, want the existing environment on its renamed branch", res)
	}
	if rec := loadState(t, env).ByPR("acme", "proj", 61); rec == nil || rec.Name != "feature-123" {
		t.Errorf("PR must be linked to the existing environment: %+v", rec)
	}
}

func TestOpenNameAndBranchOverrides(t *testing.T) {
	fake := &execx.Fake{Responses: newBranchResponses}
	env, _ := newTestEnv(t, fake)

	res, err := env.Open(OpenOptions{Target: name("my-env"), Project: "proj", Branch: "feat/x", NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	wtPath := filepath.Join(env.Cfg.ProjectsDir, "proj.my-env")
	if res.Branch != "feat/x" || res.Name != "my-env" || res.Session != "we-proj-my-env" || res.Path != wtPath {
		t.Errorf("res = %+v", res)
	}
	if !hasCall(fake, "git worktree add -b feat/x "+wtPath) {
		t.Errorf("missing worktree add:\n%s", strings.Join(fake.Joined(), "\n"))
	}

	// --name on an issue: the branch still comes from the title.
	fake.Responses = append([]execx.FakeResponse{
		{Prefix: "gh issue view 7", Out: `{"number":7,"title":"Fix crash","closedByPullRequestsReferences":[]}`},
	}, newBranchResponses...)
	res, err = env.Open(OpenOptions{Target: issue(7), Name: "crash", NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Branch != "fix-crash" || res.Name != "crash" || res.Session != "we-proj-crash" {
		t.Errorf("res = %+v", res)
	}
}

func TestOpenForkPRMaterialisesPRBranch(t *testing.T) {
	fake := &execx.Fake{Responses: append([]execx.FakeResponse{
		{Prefix: "gh pr view 456", Out: `{"number":456,"headRefName":"fix/crash","isCrossRepository":true,"closingIssuesReferences":[]}`},
	}, newBranchResponses...)}
	env, _ := newTestEnv(t, fake)

	res, err := env.Open(OpenOptions{Target: pull(456), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Branch != "pr-456" || res.Name != "pr-456" || res.Session != "we-proj-pr-456" {
		t.Errorf("res = %+v", res)
	}
	if !hasCall(fake, "git fetch origin pull/456/head:refs/heads/pr-456") {
		t.Errorf("missing PR head fetch:\n%s", strings.Join(fake.Joined(), "\n"))
	}
}

func TestOpenSameRepoPRFetchesHeadWhenNotYetFetched(t *testing.T) {
	fake := &execx.Fake{Responses: append([]execx.FakeResponse{
		{Prefix: "gh pr view 456", Out: `{"number":456,"headRefName":"fix/crash","isCrossRepository":false,"closingIssuesReferences":[]}`},
	}, newBranchResponses...)}
	env, _ := newTestEnv(t, fake)

	res, err := env.Open(OpenOptions{Target: pull(456), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Branch != "fix/crash" || res.Name != "fix-crash" {
		t.Errorf("res = %+v", res)
	}
	if !hasCall(fake, "git fetch origin +refs/heads/fix/crash:refs/remotes/origin/fix/crash") {
		t.Errorf("missing head fetch:\n%s", strings.Join(fake.Joined(), "\n"))
	}
}

func TestOpenRecreatesMissingWorktree(t *testing.T) {
	fake := &execx.Fake{}
	env, repo := newTestEnv(t, fake)
	gone := filepath.Join(env.Cwd, "gone")
	seed(t, env, &state.Env{Project: "proj", Name: "feature-123", Session: "we-proj-feature-123", Branch: "feature-123", Path: gone, RepoDir: repo})

	if _, err := env.Open(OpenOptions{Target: name("feature-123"), Project: "proj", NoTerminal: true}); err != nil {
		t.Fatalf("Open error: %v", err)
	}
	// Branch exists locally (show-ref succeeds by default) → plain add.
	for _, want := range []string{"git worktree prune", "git worktree add " + gone + " feature-123"} {
		if !hasCall(fake, want) {
			t.Errorf("missing %q in:\n%s", want, strings.Join(fake.Joined(), "\n"))
		}
	}
}

func TestOpenRecreatesMissingSession(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{{Prefix: "tmux has-session", Err: errFake}}}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "proj", Name: "feature-123", Session: "we-proj-feature-123", Branch: "feature-123", Path: path, RepoDir: repo})

	if _, err := env.Open(OpenOptions{Target: name("feature-123"), Project: "proj", NoTerminal: true}); err != nil {
		t.Fatalf("Open error: %v", err)
	}
	for _, want := range []string{"tmux new-session -d -s we-proj-feature-123 -c " + path, "tmux send-keys -t we-proj-feature-123 claude Enter"} {
		if !hasCall(fake, want) {
			t.Errorf("missing %q in:\n%s", want, strings.Join(fake.Joined(), "\n"))
		}
	}
	if hasCall(fake, "git worktree add") {
		t.Error("worktree exists; must not be re-added")
	}
}

func TestOpenClonesMissingProjectIntoContainer(t *testing.T) {
	fake := &execx.Fake{Responses: append([]execx.FakeResponse{
		{Prefix: "gh issue view 1", Out: `{"number":1,"title":"Start","closedByPullRequestsReferences":[]}`},
	}, newBranchResponses...)}
	env, _ := newTestEnv(t, fake)

	res, err := env.Open(OpenOptions{Target: target.Target{Kind: target.KindIssue, Owner: "acme", Repo: "newrepo", Number: 1}, NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	container := filepath.Join(env.Cfg.ProjectsDir, "newrepo")
	if !hasCall(fake, "gh repo clone acme/newrepo "+filepath.Join(container, ".git")+" -- --bare") {
		t.Errorf("missing bare clone into <projects>/newrepo/.git:\n%s", strings.Join(fake.Joined(), "\n"))
	}
	if res.RepoDir != container || res.Project != "newrepo" {
		t.Errorf("res = %+v, want repo dir %s", res, container)
	}
}

func TestOpenOutsideRepositoryFindsUniquelyNamedEnvironment(t *testing.T) {
	fake := &execx.Fake{}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "proj", Name: "feature-123", Session: "we-proj-feature-123", Branch: "feature-123", Path: path, RepoDir: repo})

	res, err := env.Open(OpenOptions{Target: name("feature-123"), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Project != "proj" || res.Created {
		t.Errorf("res = %+v", res)
	}
	// Two projects with that name: ambiguous.
	seed(t, env,
		&state.Env{Project: "proj", Name: "feature-123", Session: "we-proj-feature-123", Branch: "feature-123", Path: path, RepoDir: repo},
		&state.Env{Project: "other", Name: "feature-123", Session: "we-other-feature-123", Branch: "feature-123", Path: path, RepoDir: repo})
	if _, err := env.Open(OpenOptions{Target: name("feature-123"), NoTerminal: true}); err == nil || !strings.Contains(err.Error(), "--project") {
		t.Errorf("expected an ambiguity error mentioning --project, got %v", err)
	}
	// Attach from anywhere finds the environment even when the cwd project differs.
	seed(t, env, &state.Env{Project: "other", Name: "feature-123", Session: "we-other-feature-123", Branch: "feature-123", Path: path, RepoDir: repo})
	res, err = env.Open(OpenOptions{Target: name("feature-123"), Project: "proj", AttachOnly: true, NoTerminal: true})
	if err != nil {
		t.Fatalf("attach error: %v", err)
	}
	if res.Project != "other" {
		t.Errorf("attach should fall back to the unique match elsewhere, got %+v", res)
	}
}

func TestOpenPlainNameInProjectCreatesEvenIfNameExistsElsewhere(t *testing.T) {
	fake := &execx.Fake{Responses: newBranchResponses}
	env, repo := newTestEnv(t, fake)
	seed(t, env, &state.Env{Project: "other", Name: "feature-123", Session: "we-other-feature-123", Branch: "feature-123", Path: "/x", RepoDir: repo})

	res, err := env.Open(OpenOptions{Target: name("feature-123"), Project: "proj", NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if !res.Created || res.Project != "proj" {
		t.Errorf("open of a plain name in a project must create there: %+v", res)
	}
}

func TestOpenTerminalStepUnchanged(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{{Prefix: "tmux list-clients", Err: errFake}}}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "proj", Name: "feature-123", Session: "we-proj-feature-123", Branch: "feature-123", Path: path, RepoDir: repo})

	if _, err := env.Open(OpenOptions{Target: name("feature-123"), Project: "proj"}); err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if !hasCall(fake, "open -na Ghostty --args -e tmux attach-session -t we-proj-feature-123") {
		t.Errorf("missing ghostty open:\n%s", strings.Join(fake.Joined(), "\n"))
	}
	fake.Calls, fake.Responses = nil, []execx.FakeResponse{{Prefix: "tmux list-clients", Out: "/dev/ttys001: 0 [204x59 xterm-256color]"}}
	if _, err := env.Open(OpenOptions{Target: name("feature-123"), Project: "proj"}); err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if !hasCall(fake, "open -a Ghostty") || hasCall(fake, "open -na Ghostty") {
		t.Errorf("expected focus, not a new window:\n%s", strings.Join(fake.Joined(), "\n"))
	}
}

func TestDeleteRemovesSessionWorktreeBranchAndRecord(t *testing.T) {
	fake := &execx.Fake{}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "proj", Name: "feature-123", Session: "we-proj-feature-123", Branch: "feature-123", Path: path, RepoDir: repo})

	if err := env.Delete(name("feature-123"), "proj", DeleteOptions{DeleteBranch: true}); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	for _, want := range []string{
		"tmux kill-session -t =we-proj-feature-123",
		"git worktree remove " + path,
		"git worktree prune",
		"git branch -D feature-123",
	} {
		if !hasCall(fake, want) {
			t.Errorf("missing call %q in:\n%s", want, strings.Join(fake.Joined(), "\n"))
		}
	}
	if len(loadState(t, env).Envs) != 0 {
		t.Error("record must be removed")
	}
}

func TestDeleteByIssueURLAndKeepWorktree(t *testing.T) {
	fake := &execx.Fake{}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "proj", Name: "x", Session: "we-proj-x", Branch: "x", Path: path, RepoDir: repo, Owner: "acme", Repo: "proj", Issues: []int{59}})

	if err := env.Delete(issue(59), "", DeleteOptions{KeepWorktree: true}); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if !hasCall(fake, "tmux kill-session -t =we-proj-x") {
		t.Error("session must be killed")
	}
	if hasCall(fake, "git worktree remove") {
		t.Error("--keep-worktree must not remove the worktree")
	}
	if loadState(t, env).ByIssue("acme", "proj", 59) == nil {
		t.Error("--keep-worktree keeps the record")
	}
	// Full delete through the issue: record gone.
	if err := env.Delete(issue(59), "", DeleteOptions{}); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if len(loadState(t, env).Envs) != 0 {
		t.Error("record must be removed")
	}
}

func TestDeleteUnknownKillsStrayTaggedSessionElseErrors(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "tmux list-sessions", Out: "we-proj-x\tproj\tx\t/p\t0\n"},
	}}
	env, _ := newTestEnv(t, fake)
	if err := env.Delete(name("we-proj-x"), "", DeleteOptions{}); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if !hasCall(fake, "tmux kill-session -t =we-proj-x") {
		t.Errorf("stray tagged session must be killed:\n%s", strings.Join(fake.Joined(), "\n"))
	}
	if err := env.Delete(name("nothing"), "", DeleteOptions{}); err == nil {
		t.Error("expected error for an unknown environment")
	}
}

func TestListShowsRecordsWithSessionStateRefsAndLiveBranch(t *testing.T) {
	fake := &execx.Fake{}
	env, repo := newTestEnv(t, fake)
	live := mkdir(t, filepath.Join(env.Cwd, "live"))
	seed(t, env,
		&state.Env{Project: "proj", Name: "a", Session: "we-proj-a", Branch: "a", Path: live, RepoDir: repo, Owner: "acme", Repo: "proj", Issues: []int{59}, PRs: []int{61}},
		&state.Env{Project: "proj", Name: "b", Session: "we-proj-b", Branch: "b", Path: filepath.Join(env.Cwd, "gone"), RepoDir: repo},
	)
	fake.Responses = []execx.FakeResponse{
		{Prefix: "tmux list-sessions", Out: "we-proj-a\tproj\ta\t" + live + "\t1\n"},
		{Prefix: "git symbolic-ref --short -q HEAD", Out: "a-renamed"},
	}

	items, err := env.List()
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %+v", len(items), items)
	}
	a, b := items[0], items[1]
	if a.Name != "a" || a.SessionState != "attached" || a.Branch != "a-renamed" || !slices.Equal(a.Issues, []int{59}) || !slices.Equal(a.PRs, []int{61}) {
		t.Errorf("a = %+v", a)
	}
	if b.Name != "b" || b.SessionState != "none" || b.Branch != "b" {
		t.Errorf("b = %+v", b)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `make test`
Expected: `internal/we` fails to compile (`Open`, `OpenOptions`, `Delete`,
`StatePath`, … undefined).

- [ ] **Step 3: Rewrite `internal/we/we.go`**

```go
// Package we orchestrates the Smart work environment flows: open (find or
// create the project checkout, worktree and tmux session running claude,
// then surface it in a terminal), attach (find only), delete and list.
//
// A work environment is a record in the state registry (package state)
// keyed by (project, name): its branch, tmux session and worktree path are
// stored, not derived, so nothing has to be encoded in names. GitHub issue
// and PR numbers are attached to the record, which is how an issue URL and
// its linked PR URL resolve to the same environment. Git stays the truth
// for the branch: it is refreshed from the worktree whenever one is touched.
package we

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"workenv/internal/config"
	"workenv/internal/execx"
	"workenv/internal/gh"
	"workenv/internal/gitx"
	"workenv/internal/naming"
	"workenv/internal/state"
	"workenv/internal/target"
	"workenv/internal/tmuxx"
)

type Env struct {
	Cfg config.Config
	R   execx.Runner
	// GOOS controls terminal integration (Ghostty via `open` on darwin).
	GOOS string
	// Cwd is where we was invoked; a repository containing it is preferred
	// over the projects directory.
	Cwd string
	// InsideTmux indicates we runs inside a tmux client already.
	InsideTmux bool
	// StatePath is the JSON registry of work environments.
	StatePath string
}

type OpenOptions struct {
	Target  target.Target
	Project string // explicit project name (plain-name targets)
	Name    string // environment name override (new environments only)
	Branch  string // branch override (new environments only)
	// AttachOnly finds — through state, GitHub links and git worktrees —
	// but never creates.
	AttachOnly bool
	NoTerminal bool
}

type DeleteOptions struct {
	Force        bool // remove worktree even if dirty
	DeleteBranch bool
	KeepWorktree bool
}

type OpenResult struct {
	Project string
	Name    string
	Branch  string
	Path    string
	Session string
	RepoDir string
	Created bool
}

type Item struct {
	Project      string
	Name         string
	Branch       string
	Path         string
	Session      string
	SessionState string // attached | detached | none
	Issues       []int
	PRs          []int
}

func (e *Env) git() gitx.Git     { return gitx.Git{R: e.R} }
func (e *Env) tmux() tmuxx.Tmux  { return tmuxx.Tmux{R: e.R} }
func (e *Env) github() gh.Client { return gh.Client{R: e.R} }

// Open finds the work environment for the target — creating it unless
// opts.AttachOnly — makes sure its worktree and tmux session exist, saves
// the registry and surfaces the session in a terminal.
func (e *Env) Open(opts OpenOptions) (OpenResult, error) {
	st, err := state.Load(e.StatePath)
	if err != nil {
		return OpenResult{}, err
	}
	env, created, err := e.resolve(st, opts)
	if err != nil {
		return OpenResult{}, err
	}
	if err := e.ensureWorktree(env); err != nil {
		return OpenResult{}, err
	}
	if b := e.git().CurrentBranch(env.Path); b != "" {
		env.Branch = b
	}
	if !e.tmux().Has(env.Session) {
		if err := e.tmux().New(env.Session, env.Path, env.Project, env.Name); err != nil {
			return OpenResult{}, err
		}
		if err := e.tmux().RunInFirstWindow(env.Session, e.Cfg.ClaudeCmd); err != nil {
			return OpenResult{}, err
		}
	}
	if err := st.Save(); err != nil {
		return OpenResult{}, err
	}
	if !opts.NoTerminal {
		if err := e.showInTerminal(env.Session); err != nil {
			return OpenResult{}, err
		}
	}
	return OpenResult{
		Project: env.Project, Name: env.Name, Branch: env.Branch,
		Path: env.Path, Session: env.Session, RepoDir: env.RepoDir, Created: created,
	}, nil
}

// spec is a fully resolved target: everything needed to find the
// environment on its branch or to create it.
type spec struct {
	what    string // for messages: "acme/proj#59", "acme/proj PR #61", `"name"`
	project string
	repoDir string
	owner   string // GitHub owner/repo when known
	repo    string
	branch  string
	name    string // preferred name (plain-name targets); "" = the branch
	issues  []int  // numbers to record on the environment
	prs     []int
	forkPR  int  // >0: materialise branch from refs/pull/N/head
	fetch   bool // same-repo PR: make sure origin/<branch> is fetched
}

// resolve maps the target to its environment: state first, then GitHub
// links, then git worktrees; when nothing matches it creates the record
// unless AttachOnly. The boolean reports creation.
func (e *Env) resolve(st *state.Store, opts OpenOptions) (*state.Env, bool, error) {
	switch opts.Target.Kind {
	case target.KindIssue:
		return e.resolveIssue(st, opts)
	case target.KindPR:
		return e.resolvePR(st, opts)
	default:
		return e.resolveName(st, opts)
	}
}

func notFound(what string) error {
	return fmt.Errorf("no work environment for %s (attach never creates one — use we open)", what)
}

func (e *Env) resolveIssue(st *state.Store, opts OpenOptions) (*state.Env, bool, error) {
	t := opts.Target
	if env := st.ByIssue(t.Owner, t.Repo, t.Number); env != nil {
		return env, false, nil
	}
	issue, err := e.github().Issue(t.Owner, t.Repo, t.Number)
	if err != nil {
		return nil, false, err
	}
	var linked []int
	for _, ref := range issue.LinkedPRs {
		if !ref.In(t.Owner, t.Repo) {
			continue
		}
		if env := st.ByPR(t.Owner, t.Repo, ref.Number); env != nil {
			st.Link(env, t.Owner, t.Repo, []int{t.Number}, nil)
			return env, false, nil
		}
		linked = append(linked, ref.Number)
	}
	repoDir, err := e.projectRepo(t.Owner, t.Repo, !opts.AttachOnly)
	if err != nil {
		return nil, false, err
	}
	sp := spec{
		what: fmt.Sprintf("%s/%s#%d", t.Owner, t.Repo, t.Number), project: projectName(repoDir), repoDir: repoDir,
		owner: t.Owner, repo: t.Repo, branch: opts.Branch, issues: []int{t.Number},
	}
	if sp.branch == "" && len(linked) > 0 {
		// The most recent linked PR is the one being worked on; its branch
		// is the issue's branch.
		n := slices.Max(linked)
		pr, err := e.github().PR(t.Owner, t.Repo, n)
		if err != nil {
			return nil, false, err
		}
		sp.prs = []int{n}
		sp.branch, sp.forkPR, sp.fetch = prBranch(pr)
	}
	if sp.branch == "" {
		sp.branch = naming.BranchForIssue(t.Number, issue.Title)
	}
	return e.finish(st, opts, sp)
}

func (e *Env) resolvePR(st *state.Store, opts OpenOptions) (*state.Env, bool, error) {
	t := opts.Target
	if env := st.ByPR(t.Owner, t.Repo, t.Number); env != nil {
		return env, false, nil
	}
	pr, err := e.github().PR(t.Owner, t.Repo, t.Number)
	if err != nil {
		return nil, false, err
	}
	repoDir, err := e.projectRepo(t.Owner, t.Repo, !opts.AttachOnly)
	if err != nil {
		return nil, false, err
	}
	sp := spec{
		what: fmt.Sprintf("%s/%s PR #%d", t.Owner, t.Repo, t.Number), project: projectName(repoDir), repoDir: repoDir,
		owner: t.Owner, repo: t.Repo, branch: opts.Branch, prs: []int{t.Number},
	}
	for _, ref := range pr.LinkedIssues {
		if ref.In(t.Owner, t.Repo) {
			sp.issues = append(sp.issues, ref.Number)
		}
	}
	if sp.branch == "" {
		sp.branch, sp.forkPR, sp.fetch = prBranch(pr)
	}
	return e.finish(st, opts, sp)
}

// prBranch picks the local branch for a PR: its head branch for a same-repo
// PR (to be fetched into origin/<head> if needed), or pr-N materialised from
// refs/pull/N/head for a fork PR, whose head branch is not on origin.
func prBranch(pr gh.PR) (branch string, forkPR int, fetch bool) {
	if pr.IsCrossRepository {
		return naming.PRBranch(pr.Number), pr.Number, false
	}
	return pr.HeadRefName, 0, true
}

func (e *Env) resolveName(st *state.Store, opts OpenOptions) (*state.Env, bool, error) {
	raw := opts.Target.Name
	if env := st.BySession(raw); env != nil {
		return env, false, nil
	}
	project, repoDir, err := e.currentProject(opts.Project)
	if err != nil {
		return nil, false, err
	}
	// Inside a project the name is looked up there; attach (and no project)
	// fall back to a unique match anywhere.
	env, err := lookupName(st, raw, project, opts.AttachOnly || project == "")
	if err != nil {
		return nil, false, err
	}
	if env != nil {
		return env, false, nil
	}
	if opts.AttachOnly {
		return nil, false, notFound(fmt.Sprintf("%q", raw))
	}
	if project == "" {
		return nil, false, fmt.Errorf("not inside a repository: pass --project (looked in %s)", e.Cfg.ProjectsDir)
	}
	sp := spec{what: fmt.Sprintf("%q", raw), project: project, repoDir: repoDir, branch: opts.Branch, name: raw}
	if sp.branch == "" {
		sp.branch = raw
	}
	sp.owner, sp.repo, _ = e.git().OriginGitHubRepo(repoDir)
	return e.finish(st, opts, sp)
}

// lookupName finds an environment called (or on the branch) raw: within
// project when one is known, then — if global — a single match anywhere.
func lookupName(st *state.Store, raw, project string, global bool) (*state.Env, error) {
	if project != "" {
		if env := st.ByName(project, raw); env != nil {
			return env, nil
		}
		if env := st.ByBranch(project, raw); env != nil {
			return env, nil
		}
	}
	if !global {
		return nil, nil
	}
	matches := st.Matching(func(x *state.Env) bool { return x.Name == raw || x.Branch == raw })
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return matches[0], nil
	default:
		var projects []string
		for _, m := range matches {
			projects = append(projects, m.Project)
		}
		return nil, fmt.Errorf("%q exists in multiple projects (%s); pass --project", raw, strings.Join(projects, ", "))
	}
}

// currentProject resolves the project for plain-name targets: --project in
// the projects directory, else the repository containing the cwd. Both are
// empty when neither applies.
func (e *Env) currentProject(explicit string) (project, repoDir string, err error) {
	if explicit != "" {
		dir, ok := gitx.FindProjectDir(e.Cfg.ProjectsDir, explicit)
		if !ok {
			return "", "", fmt.Errorf("project %q not found in %s", explicit, e.Cfg.ProjectsDir)
		}
		return projectName(dir), dir, nil
	}
	if root := e.git().RepoRoot(e.Cwd); root != "" {
		return projectName(root), root, nil
	}
	return "", "", nil
}

// projectName is the project's directory name, without a bare ".git" suffix.
func projectName(repoDir string) string {
	return strings.TrimSuffix(filepath.Base(repoDir), ".git")
}

// finish resolves a fully specified target: an environment already on that
// branch is reused (state first, then a git worktree, whose environment is
// found by path — the branch may have been renamed inside it); otherwise
// the record is created, unless AttachOnly.
func (e *Env) finish(st *state.Store, opts OpenOptions, sp spec) (*state.Env, bool, error) {
	if env := st.ByBranch(sp.project, sp.branch); env != nil {
		st.Link(env, sp.owner, sp.repo, sp.issues, sp.prs)
		return env, false, nil
	}
	path, ok := e.git().WorktreeForBranch(sp.repoDir, sp.branch)
	if ok {
		if env := st.ByPath(path); env != nil {
			env.Branch = sp.branch
			st.Link(env, sp.owner, sp.repo, sp.issues, sp.prs)
			return env, false, nil
		}
	}
	if opts.AttachOnly {
		return nil, false, notFound(fmt.Sprintf("%s (branch %s)", sp.what, sp.branch))
	}
	env, err := e.create(st, opts, sp, path)
	return env, err == nil, err
}

// create records a new environment (and prepares its branch); the worktree
// itself is added by ensureWorktree. existingPath is a worktree already on
// the branch (made outside we) that becomes the environment's, or "".
func (e *Env) create(st *state.Store, opts OpenOptions, sp spec, existingPath string) (*state.Env, error) {
	n := opts.Name
	if n == "" {
		n = sp.name
	}
	if n == "" {
		n = sp.branch
	}
	n = naming.Sanitize(n)
	if n == "" {
		return nil, fmt.Errorf("cannot derive a name from %q; pass --name", sp.branch)
	}
	if other := st.ByName(sp.project, n); other != nil {
		return nil, fmt.Errorf("work environment %s/%s already exists (branch %s); pass --name", sp.project, n, other.Branch)
	}
	session := naming.SessionName(sp.project, n)
	if other := st.BySession(session); other != nil {
		return nil, fmt.Errorf("tmux session %s already belongs to %s/%s; pass --name", session, other.Project, other.Name)
	}
	switch {
	case sp.forkPR > 0:
		if err := e.git().FetchPRBranch(sp.repoDir, sp.forkPR, sp.branch); err != nil {
			return nil, err
		}
	case sp.fetch:
		if err := e.git().EnsureOriginBranch(sp.repoDir, sp.branch); err != nil {
			return nil, err
		}
	}
	path := existingPath
	if path == "" {
		path = e.worktreePath(sp.project, sp.repoDir, n)
	}
	env := &state.Env{
		Project: sp.project, Name: n, Session: session, Branch: sp.branch,
		Path: path, RepoDir: sp.repoDir, Owner: sp.owner, Repo: sp.repo,
		CreatedAt: time.Now().UTC(),
	}
	st.Link(env, sp.owner, sp.repo, sp.issues, sp.prs)
	st.Add(env)
	return env, nil
}

// worktreePath picks where a new worktree goes: under worktrees_dir when
// configured; inside a bare container, next to its other worktrees;
// otherwise the sibling <repo>.<name> of the checkout.
func (e *Env) worktreePath(project, repoDir, name string) string {
	if e.Cfg.WorktreesDir != "" {
		return filepath.Join(e.Cfg.WorktreesDir, project, name)
	}
	if e.git().IsBareContainer(repoDir) {
		return filepath.Join(repoDir, name)
	}
	return filepath.Join(filepath.Dir(repoDir), projectName(repoDir)+"."+name)
}

// ensureWorktree makes env.Path a checkout of env.Branch when the directory
// is missing (pruning first, in case git still remembers a deleted one).
func (e *Env) ensureWorktree(env *state.Env) error {
	if _, err := os.Stat(env.Path); err == nil {
		return nil
	}
	if err := e.git().Prune(env.RepoDir); err != nil {
		return err
	}
	return e.git().AddWorktree(env.RepoDir, env.Path, env.Branch)
}

// projectRepo locates the repository for owner/repo: the repository
// containing the current directory when its origin matches, then the
// projects directory, then (optionally) a fresh bare clone.
func (e *Env) projectRepo(owner, repo string, cloneIfMissing bool) (string, error) {
	if root := e.git().RepoRoot(e.Cwd); root != "" {
		if o, r, ok := e.git().OriginGitHubRepo(root); ok && strings.EqualFold(o, owner) && strings.EqualFold(r, repo) {
			return root, nil
		}
	}
	if dir, found := gitx.FindProjectDir(e.Cfg.ProjectsDir, repo); found {
		return dir, nil
	}
	if !cloneIfMissing {
		return "", fmt.Errorf("project %q not found in %s", repo, e.Cfg.ProjectsDir)
	}
	// Bare-clone as <projects_dir>/<repo>/.git — the container layout, so
	// the worktrees can sit next to it.
	container := filepath.Join(e.Cfg.ProjectsDir, repo)
	if err := os.MkdirAll(container, 0o755); err != nil {
		return "", err
	}
	fmt.Fprintf(os.Stderr, "cloning %s/%s (bare) into %s\n", owner, repo, container)
	if err := e.git().CloneBare(owner+"/"+repo, filepath.Join(container, ".git")); err != nil {
		return "", err
	}
	return container, nil
}

// showInTerminal implements the "define terminal" step: switch the current
// tmux client if we already runs inside tmux; focus Ghostty if some client
// is already attached to the session; otherwise open a new Ghostty window
// attached to it.
func (e *Env) showInTerminal(session string) error {
	if e.InsideTmux {
		return e.tmux().SwitchClient(session)
	}
	if e.tmux().HasClients(session) {
		return e.focusTerminal()
	}
	return e.openTerminal("tmux", "attach-session", "-t", session)
}

func (e *Env) focusTerminal() error {
	if e.GOOS == "darwin" {
		return e.R.Run("", "open", "-a", "Ghostty")
	}
	return nil // no portable focus mechanism; the session is attachable manually
}

// openTerminal opens a new Ghostty window running argv.
func (e *Env) openTerminal(argv ...string) error {
	if e.GOOS == "darwin" {
		args := append([]string{"-na", "Ghostty", "--args", "-e"}, argv...)
		return e.R.Run("", "open", args...)
	}
	return e.R.Run("", "ghostty", append([]string{"-e"}, argv...)...)
}

// AttachRemote opens a local terminal attached (over ssh) to a session on a
// remote host.
func (e *Env) AttachRemote(host, session string) error {
	return e.openTerminal("ssh", "-t", host, "tmux", "attach-session", "-t", session)
}

// Delete tears down the environment for the target: kills the tmux session,
// removes the worktree (unless KeepWorktree) and optionally the branch, and
// drops the record. Resolution goes through the registry only.
func (e *Env) Delete(t target.Target, project string, opts DeleteOptions) error {
	st, err := state.Load(e.StatePath)
	if err != nil {
		return err
	}
	env, err := e.lookup(st, t, project)
	if err != nil {
		return err
	}
	if env == nil {
		return e.killStray(t, project)
	}
	if e.tmux().Has(env.Session) {
		if err := e.tmux().Kill(env.Session); err != nil {
			return err
		}
	}
	if opts.KeepWorktree {
		return nil
	}
	if _, err := os.Stat(env.Path); err == nil {
		if err := e.git().RemoveWorktree(env.RepoDir, env.Path, opts.Force); err != nil {
			return err
		}
	} else {
		// Directory already gone: just let git forget it.
		_ = e.git().Prune(env.RepoDir)
	}
	if opts.DeleteBranch && env.Branch != "" {
		if err := e.git().DeleteBranch(env.RepoDir, env.Branch); err != nil {
			return err
		}
	}
	st.Remove(env.Project, env.Name)
	return st.Save()
}

// lookup finds the environment for a target in the registry alone.
func (e *Env) lookup(st *state.Store, t target.Target, project string) (*state.Env, error) {
	switch t.Kind {
	case target.KindIssue:
		return st.ByIssue(t.Owner, t.Repo, t.Number), nil
	case target.KindPR:
		return st.ByPR(t.Owner, t.Repo, t.Number), nil
	}
	if env := st.BySession(t.Name); env != nil {
		return env, nil
	}
	if project == "" {
		if root := e.git().RepoRoot(e.Cwd); root != "" {
			project = projectName(root)
		}
	}
	return lookupName(st, t.Name, project, true)
}

// killStray is the last resort for a target that is not in the registry: a
// live @workenv-tagged tmux session by that name (a lost or edited registry)
// still gets killed. Anything else is unknown.
func (e *Env) killStray(t target.Target, project string) error {
	if t.Kind == target.KindName {
		sessions, _ := e.tmux().List()
		for _, s := range sessions {
			if s.Name == t.Name || (project != "" && s.Name == naming.SessionName(project, t.Name)) {
				return e.tmux().Kill(s.Name)
			}
		}
	}
	return fmt.Errorf("no work environment for %s", describe(t))
}

func describe(t target.Target) string {
	switch t.Kind {
	case target.KindIssue:
		return fmt.Sprintf("%s/%s#%d", t.Owner, t.Repo, t.Number)
	case target.KindPR:
		return fmt.Sprintf("%s/%s PR #%d", t.Owner, t.Repo, t.Number)
	default:
		return fmt.Sprintf("%q", t.Name)
	}
}

// List renders the registry with live tmux state; the branch is read from
// the worktree when it exists (git is the truth for it).
func (e *Env) List() ([]Item, error) {
	st, err := state.Load(e.StatePath)
	if err != nil {
		return nil, err
	}
	sessions, err := e.tmux().List()
	if err != nil {
		return nil, err
	}
	attached := map[string]bool{}
	for _, s := range sessions {
		attached[s.Name] = s.Attached
	}
	var out []Item
	for _, env := range st.Envs {
		item := Item{
			Project: env.Project, Name: env.Name, Branch: env.Branch, Path: env.Path,
			Session: env.Session, SessionState: "none", Issues: env.Issues, PRs: env.PRs,
		}
		if isAttached, live := attached[env.Session]; live {
			item.SessionState = "detached"
			if isAttached {
				item.SessionState = "attached"
			}
		}
		if _, err := os.Stat(env.Path); err == nil {
			if b := e.git().CurrentBranch(env.Path); b != "" {
				item.Branch = b
			}
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Project != out[j].Project {
			return out[i].Project < out[j].Project
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
```

Then delete `TopLevel` from `internal/gitx/gitx.go` (the method and its
comment); nothing else uses it.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `make check`
Expected: every package `ok`, gofmt/vet clean. If a test fails, fix the
implementation, not the test — each test encodes a spec rule (see the spec's
Resolution / Worktree placement / delete / list sections).

- [ ] **Step 5: Commit**

```bash
git add internal/we internal/gitx
git commit -m "feat(we): resolve, open, attach, delete and list through the registry

Open finds the environment for a target — state first, then GitHub links
(an issue and its linked PR share one environment; the branch comes from
the PR), then git worktrees on that branch — and creates the record only
when nothing matches; AttachOnly never creates. Names carry no issue/PR
prefix: the branch is the PR head or the title slug, the name follows,
--name/--branch override. New worktrees go inside a bare container, else
the sibling <repo>.<name>, unless worktrees_dir is set; missing projects
are bare-cloned as <repo>/.git. Delete and List work off the registry;
the branch is refreshed from the worktree whenever it is touched.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: CLI (`open`/`attach`, flags, usage) and README

**Files:**
- Modify: `cmd/we/main.go`
- Modify: `README.md`

**Interfaces:**
- Consumes: Task 4's `we.Env{…, StatePath}`, `we.OpenOptions`,
  `we.OpenResult`, `we.DeleteOptions`, `we.Item`, `Open`, `Delete`, `List`,
  `AttachRemote`; Task 1's `state.DefaultPath()`.
- Produces: the user-facing CLI.

- [ ] **Step 1: Rewrite `cmd/we/main.go`**

```go
// we — Smart work environment.
//
// Opens a complete work environment for a task: project repository (cloned
// bare if missing), git worktree, tmux session running claude, and a
// Ghostty terminal attached to it. Environments are recorded in a JSON
// registry, so a GitHub issue and its linked PR resolve to the same one and
// branches, session names and worktree paths never have to be re-derived.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"text/tabwriter"

	"workenv/internal/config"
	"workenv/internal/execx"
	"workenv/internal/state"
	"workenv/internal/target"
	"workenv/internal/we"
)

const usage = `we — Smart work environment

Usage:
  we open   <target> [flags]   find or create a work environment and attach to it
  we attach <target> [flags]   attach to an existing work environment (never creates)
  we list                      list work environments
  we delete <target> [flags]   tear down a work environment

Aliases: create, up = open; ls = list; rm, down = delete

<target> is one of:
  a GitHub issue URL    https://github.com/owner/repo/issues/123
  a GitHub PR URL       https://github.com/owner/repo/pull/456
  a tmux session name   we-repo-fix-crash
  a name or branch      fix-crash — an existing environment; for open, a new
                        one on that branch in the current repo (or --project)

An issue and the PR linked to it resolve to the same environment. The
branch comes from the PR when there is one, from the issue title otherwise.

Flags for open:
  --project <name>   project in the projects dir (plain-name targets)
  --name <name>      environment name (default: the branch); the tmux
                     session is we-<project>-<name>
  --branch <name>    branch to check out or create (default: PR head, issue
                     title slug, or the plain name)
  --host <host>      open on a remote host (needs we installed there),
                     then attach locally over ssh
  --no-terminal      skip opening/focusing the terminal

Flags for attach: --project, --host, --no-terminal (as above)

Flags for delete:
  --project <name>    project the environment belongs to (else inferred)
  --host <host>       delete on a remote host
  --force             remove the worktree even if it has local changes
  --delete-branch     also delete the branch
  --keep-worktree     only kill the tmux session

Config (XDG): ~/.config/workenv/config.toml
  projects_dir  = "~/projects"   where repositories live / get cloned
  worktrees_dir = ""             root for all worktrees; unset = standard
                                 placement (inside a bare container, else
                                 the sibling <repo>.<name>)
  claude_cmd    = "claude"       command run in the first tmux window
  remote_we     = "we"           we binary path on remote hosts

State (XDG): ~/.local/state/workenv/envs.json
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "we:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Print(usage)
		if len(args) == 0 {
			return errors.New("missing command")
		}
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	env := &we.Env{
		Cfg:        cfg,
		R:          execx.Real{},
		GOOS:       runtime.GOOS,
		Cwd:        cwd,
		InsideTmux: os.Getenv("TMUX") != "",
		StatePath:  state.DefaultPath(),
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "open", "create", "up":
		return runOpen(env, "open", rest, false)
	case "attach":
		return runOpen(env, "attach", rest, true)
	case "list", "ls":
		return runList(env, rest)
	case "delete", "rm", "down":
		return runDelete(env, rest)
	default:
		return fmt.Errorf("unknown command %q (see we help)", cmd)
	}
}

// parseWithArg lets flags appear before or after the positional argument.
func parseWithArg(fs *flag.FlagSet, args []string) (string, error) {
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if fs.NArg() == 0 {
		return "", nil
	}
	arg := fs.Arg(0)
	if err := fs.Parse(fs.Args()[1:]); err != nil {
		return "", err
	}
	return arg, nil
}

// runOpen serves both open (find or create) and attach (find only); the two
// differ in attachOnly and in attach not taking --name/--branch.
func runOpen(env *we.Env, cmd string, args []string, attachOnly bool) error {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	project := fs.String("project", "", "project name")
	host := fs.String("host", "", "remote host")
	noTerminal := fs.Bool("no-terminal", false, "skip terminal step")
	var name, branch string
	if !attachOnly {
		fs.StringVar(&name, "name", "", "environment name")
		fs.StringVar(&branch, "branch", "", "branch to check out or create")
	}
	raw, err := parseWithArg(fs, args)
	if err != nil {
		return err
	}
	if raw == "" {
		return fmt.Errorf("%s: missing target (issue URL, PR URL, session or name)", cmd)
	}
	tgt, err := target.Parse(raw)
	if err != nil {
		return err
	}

	if *host != "" {
		var extra []string
		if *project != "" {
			extra = append(extra, "--project", *project)
		}
		if name != "" {
			extra = append(extra, "--name", name)
		}
		if branch != "" {
			extra = append(extra, "--branch", branch)
		}
		return openRemote(env, cmd, *host, raw, extra, *noTerminal)
	}

	res, err := env.Open(we.OpenOptions{
		Target: tgt, Project: *project, Name: name, Branch: branch,
		AttachOnly: attachOnly, NoTerminal: *noTerminal,
	})
	if err != nil {
		return err
	}
	verb := "found"
	if res.Created {
		verb = "created"
	}
	fmt.Printf("%s work environment %s/%s\n", verb, res.Project, res.Name)
	fmt.Printf("project:  %s (%s)\n", res.Project, res.RepoDir)
	fmt.Printf("branch:   %s\n", res.Branch)
	fmt.Printf("worktree: %s\n", res.Path)
	fmt.Printf("session:  %s\n", res.Session)
	// Machine-readable marker; the remote flow parses it over ssh.
	fmt.Printf("WE_SESSION=%s\n", res.Session)
	return nil
}

// openRemote runs the command on the remote host (terminal step skipped
// there) and attaches to the resulting session from a local terminal over
// ssh.
func openRemote(env *we.Env, cmd, host, rawTarget string, extra []string, noTerminal bool) error {
	remote := append([]string{host, env.Cfg.RemoteWe, cmd, rawTarget, "--no-terminal"}, extra...)
	out, err := env.R.Output("", "ssh", remote...)
	if out != "" {
		fmt.Println(out)
	}
	if err != nil {
		return fmt.Errorf("remote %s on %s: %w", cmd, host, err)
	}
	session := ""
	for _, line := range strings.Split(out, "\n") {
		if s, ok := strings.CutPrefix(strings.TrimSpace(line), "WE_SESSION="); ok {
			session = s
		}
	}
	if session == "" {
		return fmt.Errorf("remote we did not report a session (is %q installed on %s?)", env.Cfg.RemoteWe, host)
	}
	if noTerminal {
		fmt.Printf("attach with: ssh -t %s tmux attach-session -t %s\n", host, session)
		return nil
	}
	return env.AttachRemote(host, session)
}

func runList(env *we.Env, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	host := fs.String("host", "", "remote host")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *host != "" {
		return env.R.Run("", "ssh", *host, env.Cfg.RemoteWe, "list")
	}
	items, err := env.List()
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Println("no work environments")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PROJECT\tNAME\tBRANCH\tREFS\tSESSION\tSTATE\tPATH")
	for _, it := range items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			it.Project, it.Name, orDash(it.Branch), formatRefs(it.Issues, it.PRs), orDash(it.Session), it.SessionState, it.Path)
	}
	return w.Flush()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// formatRefs renders linked numbers as "#59 PR#61", or "-" when none.
func formatRefs(issues, prs []int) string {
	var parts []string
	for _, n := range issues {
		parts = append(parts, "#"+strconv.Itoa(n))
	}
	for _, n := range prs {
		parts = append(parts, "PR#"+strconv.Itoa(n))
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, " ")
}

func runDelete(env *we.Env, args []string) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	project := fs.String("project", "", "project name")
	host := fs.String("host", "", "remote host")
	force := fs.Bool("force", false, "remove worktree even if dirty")
	deleteBranch := fs.Bool("delete-branch", false, "also delete the branch")
	keepWorktree := fs.Bool("keep-worktree", false, "only kill the tmux session")
	raw, err := parseWithArg(fs, args)
	if err != nil {
		return err
	}
	if raw == "" {
		return errors.New("delete: missing target (issue URL, PR URL, session or name)")
	}
	if *host != "" {
		remote := []string{*host, env.Cfg.RemoteWe, "delete", raw}
		if *project != "" {
			remote = append(remote, "--project", *project)
		}
		if *force {
			remote = append(remote, "--force")
		}
		if *deleteBranch {
			remote = append(remote, "--delete-branch")
		}
		if *keepWorktree {
			remote = append(remote, "--keep-worktree")
		}
		return env.R.Run("", "ssh", remote...)
	}
	tgt, err := target.Parse(raw)
	if err != nil {
		return err
	}
	if err := env.Delete(tgt, *project, we.DeleteOptions{
		Force:        *force,
		DeleteBranch: *deleteBranch,
		KeepWorktree: *keepWorktree,
	}); err != nil {
		return err
	}
	fmt.Printf("deleted work environment %q\n", raw)
	return nil
}
```

- [ ] **Step 2: Build and run the suite**

Run: `make check && make build && ./bin/we help | head -5`
Expected: check clean; `bin/we` built; usage printed.

- [ ] **Step 3: Rewrite `README.md`**

Replace the whole file with:

````markdown
# workenv — Smart work environment

`we` opens a complete, disposable work environment for a task in one
command: the project repository (bare-cloned if you don't have it), a git
worktree on the right branch, a tmux session with `claude` running in the
first window, and a Ghostty window attached to it.

```
we open https://github.com/acme/example-service/issues/123
```

…finds (or clones) `example-service`, checks out the branch for the issue
in a worktree — the linked PR's branch if there is one, otherwise a new
`<title-slug>` branch — starts the tmux session
`we-example-service-<name>` with claude running, and opens Ghostty attached
to it. Run it again, or run it with the PR's URL, and it brings the same
environment back into focus.

## Prerequisites

`git`, `gh` (authenticated), `tmux`, [Ghostty](https://ghostty.org). Building
needs either a Go toolchain or Docker.

## Install

With Go installed:

```
go build -o ~/bin/we ./cmd/we
```

Without it — every `make` target runs the Go toolchain in a container, so
Docker is the only build dependency. The build cross-compiles for the host,
so `bin/we` is a native binary, not a Linux one:

```
make build     # -> bin/we
make install   # -> ~/bin/we
make check     # gofmt, go vet, tests
```

`make help` lists all targets. `GO_IMAGE`, `BIN`, `INSTALL_DIR`, `CACHE_DIR`
and `GOOS`/`GOARCH` are overridable — e.g.
`make build GOOS=linux GOARCH=amd64`.

## Usage

```
we open   <target> [flags]    find or create, then attach
we attach <target> [flags]    attach to an existing one (never creates)
we list                       list environments
we delete <target> [flags]    tear down
```

`create`/`up` are aliases of `open`, `ls` of `list`, `rm`/`down` of
`delete`.

`<target>` is one of:

| Target             | Example                          |
|--------------------|----------------------------------|
| GitHub issue URL   | `.../example-service/issues/123` |
| GitHub PR URL      | `.../example-service/pull/456`   |
| tmux session name  | `we-example-service-fix-crash`   |
| name or branch     | `fix-crash`                      |

A plain name that matches nothing is, for `open`, a branch to create in the
repository you're standing in (or `--project <name>` in the projects
directory). `attach` finds an environment by any of the above from anywhere
and never creates one — so a mistyped session name is an error, not a new
branch.

Flags for `open`: `--name <name>` (environment name; default the branch),
`--branch <name>` (branch to check out or create; default the PR head, the
issue title slug, or the plain name), `--project`, `--host`,
`--no-terminal`. `attach` takes `--project`, `--host`, `--no-terminal`.

`we delete <target>` kills the session, removes the worktree and forgets the
environment; flags `--force` (dirty worktree), `--delete-branch`,
`--keep-worktree` (kill the session only), `--project`.

### Issues and pull requests

An issue and the PR linked to it (closing keywords or the Development
sidebar) are one environment: `we open …/issues/59` and
`we open …/pull/61` land in the same tmux session, whichever came first.
The branch is the PR's head branch when a PR exists — the branch someone
actually pushed — and a slug of the issue title otherwise. Nothing is
encoded in names: rename the branch, retitle the issue, pick `--name`, and
the environment is still found.

A PR from a fork has no head branch on origin, so its branch is `pr-456`,
materialised from `refs/pull/456/head`.

### Remote hosts

`we open <target> --host devbox` runs `we` on the remote host over ssh
(terminal step skipped there) and opens a local Ghostty window attached to
the remote tmux session via `ssh -t`. `attach`, `list` and `delete` pass
through the same way. The remote host needs `we` installed (path
configurable via `remote_we`).

## Configuration

XDG notation: `~/.config/workenv/config.toml` (or
`$XDG_CONFIG_HOME/workenv/config.toml`). All keys optional:

```toml
projects_dir  = "~/projects"   # where repositories live / get cloned
worktrees_dir = ""             # root for all worktrees; unset = standard placement
claude_cmd    = "claude"       # command run in the first tmux window
remote_we     = "we"           # we binary path on remote hosts
```

## Design

**Recorded, not derived.** Every environment is a record in
`~/.local/state/workenv/envs.json` (`$XDG_STATE_HOME`): project, name,
tmux session, branch, worktree path, repository, and the GitHub issue and
PR numbers attached to it. `(project, name)` identifies it; the session is
`we-<project>-<name>`. Git stays the truth for the branch — it is refreshed
from the worktree whenever an environment is opened or listed. tmux
sessions still carry `@workenv_*` user options, which is how `we list`
tells attached from detached and how a we session differs from a personal
one.

**Resolution** goes state → GitHub → git:

- an issue or PR number already recorded → that environment;
- an issue's linked PRs (`closedByPullRequestsReferences`) or a PR's linked
  issues (`closingIssuesReferences`) already recorded → that environment,
  with the new number attached;
- an environment on the same branch — in the registry, or a git worktree
  already checked out on it (made by hand or by worktrunk) → adopted;
- otherwise `open` creates the record and `attach` stops.

**Worktree placement** for a new environment, in order:

1. `worktrees_dir` set → `<worktrees_dir>/<project>/<name>`;
2. the project is a bare container (`~/projects/trade/.git` is bare, its
   worktrees are `~/projects/trade/main`, `~/projects/trade/<name>`, …) →
   inside it, next to the others;
3. otherwise the sibling `<repo>.<name>` of the checkout
   (`~/projects/workenv.fix-crash`) — worktrunk's convention.

Missing projects are bare-cloned as `<projects_dir>/<repo>/.git`, so they
fit rule 2. The repository containing the cwd is found with
`git rev-parse --git-common-dir`, which works from a checkout, a worktree or
a bare container alike.

**Open flow** (each step finds before it creates, so `open` is idempotent):

1. *Resolve* the target as above.
2. *Project*: the repository containing the cwd (if its origin matches),
   else `<projects_dir>/<repo>` or `<repo>.git`, else `git clone --bare` via
   `gh` plus refs setup (fetch refspec for `refs/remotes/origin/*`,
   `remote set-head`), so origin-tracking branches work in the bare clone.
3. *Worktree*: reused when the branch is already checked out somewhere;
   otherwise created at the placement above, starting the branch from
   `origin/<branch>` (fetched first for a same-repo PR head) or the default
   branch.
4. *tmux*: session created detached, tagged, and `claude_cmd` typed into
   the first window with `send-keys` (so the window outlives the command).
5. *Terminal*: inside tmux → `switch-client`; a client already attached →
   focus Ghostty; otherwise a new Ghostty window running
   `tmux attach-session`.

A missing worktree directory or a dead tmux session (after a reboot) is
recreated on the next `open`.

**Plain `git worktree`, not worktrunk** — no extra dependency; but a
worktree worktrunk already made on the branch is adopted rather than
duplicated.

Note for tmux ≥ 3.5: the `=name` exact-match target syntax only works for
session-target commands (`has-session`, `kill-session`, …); pane-target
commands (`send-keys`, `set-option`, `show-options`, `capture-pane`) reject
it, so `we` passes the bare session name there (exact names still beat
prefix matches).
````

- [ ] **Step 4: Verify docs and build once more**

Run: `make check && make build`
Expected: clean. Also `grep -n 'issue-123\|pr-456\|\.we' README.md cmd/we/main.go`
should only show the fork-PR `pr-456` mentions and nothing about
`issue-123-…` names or a `.we` directory.

- [ ] **Step 5: Commit**

```bash
git add cmd/we/main.go README.md
git commit -m "feat(cli): open/attach commands, --name/--branch, registry-aware list

open (aliases create, up) finds or creates and attaches; attach only finds.
Both accept issue/PR URLs, tmux session names and environment names;
open also takes --name and --branch. list gains a REFS column; delete
accepts any target. README describes the recorded design, resolution
order and worktree placement.

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Self-review against the spec

- State file, schema, atomic save, invariants → Task 1.
- Naming defaults, `--name`/`--branch`, no prefixes → Tasks 3, 4, 5.
- Commands `open`/`attach`/`list`/`delete`, aliases, targets → Task 5
  (behaviour in Task 4).
- Resolution order for issue / PR / plain string, attach never creates,
  branch is the identity → Task 4 (`resolveIssue`, `resolvePR`,
  `resolveName`, `finish`).
- Worktree placement rules, container clone dest, repo root from any
  layout → Tasks 2 and 4 (`worktreePath`, `projectRepo`, `RepoRoot`).
- Same-repo PR head fetch → Task 2 (`EnsureOriginBranch`) + Task 4.
- Repair on open (worktree / session / branch drift) → Task 4 (`Open`,
  `ensureWorktree`).
- delete semantics incl. stray session → Task 4 (`Delete`, `killStray`).
- list columns → Tasks 4 and 5.
- Remote pass-through of `open`/`attach` flags → Task 5.
- Config: `worktrees_dir` default → Task 3.
- README/design doc → Task 5.
