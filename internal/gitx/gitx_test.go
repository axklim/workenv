package gitx

import (
	"errors"
	"strings"
	"testing"

	"workenv/internal/execx"
)

const porcelain = `worktree /Users/u/projects/example-service
HEAD abcdefabcdefabcdefabcdefabcdefabcdefabcd
branch refs/heads/main

worktree /Users/u/projects/.we/example-service/feature-x
HEAD 1234561234561234561234561234561234561234
branch refs/heads/feature-x

worktree /Users/u/projects/.we/example-service/detached
HEAD 9999999999999999999999999999999999999999
detached
`

func TestParseWorktrees(t *testing.T) {
	wts := parseWorktrees(porcelain)
	if len(wts) != 3 {
		t.Fatalf("got %d worktrees, want 3: %+v", len(wts), wts)
	}
	if wts[0].Path != "/Users/u/projects/example-service" || wts[0].Branch != "main" {
		t.Errorf("wts[0] = %+v", wts[0])
	}
	if wts[1].Branch != "feature-x" {
		t.Errorf("wts[1] = %+v", wts[1])
	}
	if wts[2].Branch != "" {
		t.Errorf("detached worktree should have empty branch, got %+v", wts[2])
	}
}

func TestParseGitHubRemote(t *testing.T) {
	tests := []struct {
		url         string
		owner, repo string
	}{
		{"git@github.com:acme/example-service.git", "acme", "example-service"},
		{"https://github.com/acme/example-service.git", "acme", "example-service"},
		{"https://github.com/acme/example-service", "acme", "example-service"},
		{"ssh://git@github.com/acme/example-service.git", "acme", "example-service"},
	}
	for _, tt := range tests {
		owner, repo, ok := parseGitHubRemote(tt.url)
		if !ok || owner != tt.owner || repo != tt.repo {
			t.Errorf("parseGitHubRemote(%q) = %q, %q, %v", tt.url, owner, repo, ok)
		}
	}
	if _, _, ok := parseGitHubRemote("https://gitlab.com/acme/example-service.git"); ok {
		t.Error("non-github remote should not parse")
	}
}

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

func TestCloneUsesANormalClone(t *testing.T) {
	f := &execx.Fake{}
	if err := (Git{R: f}).Clone("acme/proj", "/dest"); err != nil {
		t.Fatalf("Clone error: %v", err)
	}
	if len(f.Calls) != 1 {
		t.Fatalf("got %d calls, want 1: %v", len(f.Calls), f.Joined())
	}
	if got := f.Joined()[0]; got != "gh repo clone acme/proj /dest" {
		t.Errorf("command = %q, want %q", got, "gh repo clone acme/proj /dest")
	}
	for _, c := range f.Joined() {
		if strings.Contains(c, "--bare") || strings.Contains(c, "remote.origin.fetch") || strings.Contains(c, "remote set-head") {
			t.Errorf("unexpected bare-clone setup call: %q", c)
		}
	}
}

func TestProjectNameFromOrigin(t *testing.T) {
	f := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "git remote get-url origin", Out: "git@github.com:axklim/trade.git"},
	}}
	if got := (Git{R: f}).ProjectName("/Users/u/projects/trade"); got != "trade" {
		t.Errorf("ProjectName = %q, want %q", got, "trade")
	}
}

func TestProjectNameFallsBackToBasename(t *testing.T) {
	f := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "git remote get-url origin", Err: errFake},
	}}
	if got := (Git{R: f}).ProjectName("/Users/u/projects/trade.git"); got != "trade" {
		t.Errorf("ProjectName = %q, want %q", got, "trade")
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

// newBranchFake scripts a repository where branch feat exists nowhere yet
// and the default branch is origin's main.
func newBranchFake(extra ...execx.FakeResponse) *execx.Fake {
	return &execx.Fake{Responses: append(extra,
		execx.FakeResponse{Prefix: "git show-ref --verify --quiet refs/heads/feat", Err: errFake},
		execx.FakeResponse{Prefix: "git show-ref --verify --quiet refs/remotes/origin/feat", Err: errFake},
		execx.FakeResponse{Prefix: "git symbolic-ref --short refs/remotes/origin/HEAD", Out: "origin/main"},
		execx.FakeResponse{Prefix: "git rev-parse --short refs/heads/feat", Out: "abc1234"},
	)}
}

func callIndex(f *execx.Fake, prefix string) int {
	for i, c := range f.Joined() {
		if strings.HasPrefix(c, prefix) {
			return i
		}
	}
	return -1
}

func TestAddWorktreeFetchesDefaultBranchFirst(t *testing.T) {
	f := newBranchFake()
	base, err := (Git{R: f}).AddWorktree("/repo", t.TempDir()+"/wt", "feat")
	if err != nil {
		t.Fatalf("AddWorktree error: %v", err)
	}
	if base.Ref != "origin/main" || base.Commit != "abc1234" || base.FetchErr != nil {
		t.Errorf("base = %+v", base)
	}
	fetch := callIndex(f, "git fetch origin +refs/heads/main:refs/remotes/origin/main")
	add := callIndex(f, "git worktree add -b feat ")
	if fetch < 0 || add < 0 || fetch > add {
		t.Errorf("want the fetch before worktree add:\n%s", strings.Join(f.Joined(), "\n"))
	}
	if !strings.HasSuffix(f.Joined()[add], " origin/main") {
		t.Errorf("worktree add = %q, want it to start from origin/main", f.Joined()[add])
	}
	if c := f.Calls[fetch]; c.Dir != "/repo" || c.Method != "Output" {
		t.Errorf("fetch ran as %s in %q, want Output in /repo", c.Method, c.Dir)
	}
}

func TestAddWorktreeFallsBackWhenFetchFails(t *testing.T) {
	f := newBranchFake(execx.FakeResponse{Prefix: "git fetch", Err: errFake})
	base, err := (Git{R: f}).AddWorktree("/repo", t.TempDir()+"/wt", "feat")
	if err != nil {
		t.Fatalf("a failed fetch must not fail AddWorktree: %v", err)
	}
	if !errors.Is(base.FetchErr, errFake) || base.Ref != "origin/main" {
		t.Errorf("base = %+v", base)
	}
	if callIndex(f, "git worktree add -b feat ") < 0 {
		t.Errorf("worktree add never ran:\n%s", strings.Join(f.Joined(), "\n"))
	}
}

func TestAddWorktreeWithoutOriginDoesNotFetch(t *testing.T) {
	f := newBranchFake(
		execx.FakeResponse{Prefix: "git remote get-url origin", Err: errFake},
		execx.FakeResponse{Prefix: "git show-ref --verify --quiet refs/remotes/origin/main", Err: errFake},
		execx.FakeResponse{Prefix: "git symbolic-ref --short refs/remotes/origin/HEAD", Err: errFake},
		execx.FakeResponse{Prefix: "git symbolic-ref --short HEAD", Out: "main"},
	)
	base, err := (Git{R: f}).AddWorktree("/repo", t.TempDir()+"/wt", "feat")
	if err != nil {
		t.Fatalf("AddWorktree error: %v", err)
	}
	if base.Ref != "main" || base.FetchErr != nil {
		t.Errorf("base = %+v", base)
	}
	if callIndex(f, "git fetch") >= 0 {
		t.Errorf("unexpected fetch without an origin:\n%s", strings.Join(f.Joined(), "\n"))
	}
}

func TestAddWorktreeExistingBranchesDoNotFetch(t *testing.T) {
	for name, f := range map[string]*execx.Fake{
		"local branch": {}, // show-ref succeeds by default
		"origin branch": {Responses: []execx.FakeResponse{
			{Prefix: "git show-ref --verify --quiet refs/heads/feat", Err: errFake},
		}},
	} {
		base, err := (Git{R: f}).AddWorktree("/repo", t.TempDir()+"/wt", "feat")
		if err != nil {
			t.Fatalf("%s: AddWorktree error: %v", name, err)
		}
		if base != (Base{}) {
			t.Errorf("%s: base = %+v, want zero", name, base)
		}
		if callIndex(f, "git fetch") >= 0 {
			t.Errorf("%s: unexpected fetch:\n%s", name, strings.Join(f.Joined(), "\n"))
		}
	}
}
