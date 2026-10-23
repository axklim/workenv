package we

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"workenv/internal/execx"
	"workenv/internal/state"
	"workenv/internal/target"
)

// rootsRunner answers git rev-parse --git-common-dir per directory, which a
// prefix-matched Fake cannot: a directory inside one of roots is in that
// repository, any other is in none. Every call is still recorded.
type rootsRunner struct {
	*execx.Fake
	roots []string
}

func (r rootsRunner) Output(dir, name string, args ...string) (string, error) {
	out, err := r.Fake.Output(dir, name, args...)
	if name == "git" && slices.Contains(args, "--git-common-dir") {
		for _, root := range r.roots {
			if within(root, dir) {
				return filepath.Join(root, ".git"), nil
			}
		}
		return "", errFake
	}
	return out, err
}

// newEnvWithRepos is newTestEnv plus a second repository "other" in
// projects_path, with rev-parse answered for both.
func newEnvWithRepos(t *testing.T, fake *execx.Fake) (env *Env, proj, other string) {
	t.Helper()
	env, proj = newTestEnv(t, fake)
	other = mkdir(t, filepath.Join(env.Cfg.ProjectsPath, "other", ".git"))
	other = filepath.Dir(other)
	env.R = rootsRunner{Fake: fake, roots: []string{proj, other}}
	return env, proj, other
}

func TestPlanNewReadings(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		cwd      string // "proj" or "" for outside any repository
		repo     string // "proj", "other" or "" for a URL
		branch   string
		readings []string
	}{
		{"issue URL", []string{"https://github.com/acme/proj/issues/59"}, "", "", "",
			[]string{"target: acme/proj#59"}},
		{"issue URL with a name", []string{"https://github.com/acme/proj/issues/59", "fix"}, "", "", "fix",
			[]string{"target: acme/proj#59", "branch: fix (given)"}},
		{"repo and name", []string{"other", "review"}, "proj", "other", "review",
			[]string{"repo: other (projects_path)", "branch: review (given)"}},
		{"alias and name", []string{"ot", "review"}, "", "other", "review",
			[]string{"repo: other (alias ot)", "branch: review (given)"}},
		{"repo alone", []string{"other"}, "proj", "other", "feature-1",
			[]string{"repo: other (projects_path)", "branch: feature-1 (next free)"}},
		{"name in the cwd's repository", []string{"spike"}, "proj", "proj", "spike",
			[]string{"repo: proj (cwd)", "branch: spike (given)"}},
		{"the cwd's own repository is a branch", []string{"proj"}, "proj", "proj", "proj",
			[]string{"repo: proj (cwd)", "branch: proj (given)"}},
		{"a branch with a slash is not a path", []string{"feat/login"}, "proj", "proj", "feat/login",
			[]string{"repo: proj (cwd)", "branch: feat/login (given)"}},
		{"dot", []string{"."}, "proj", "proj", "feature-1",
			[]string{"repo: proj (cwd)", "branch: feature-1 (next free)"}},
		{"dot and name", []string{".", "x"}, "proj", "proj", "x",
			[]string{"repo: proj (cwd)", "branch: x (given)"}},
		{"repository URL found locally", []string{"https://github.com/acme/other", "x"}, "", "other", "x",
			[]string{"repo: other (projects_path)", "branch: x (given)"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &execx.Fake{}
			env, proj, other := newEnvWithRepos(t, fake)
			env.Cfg.Aliases = map[string]string{"ot": "other"}
			if tc.cwd == "proj" {
				env.Cwd = proj
			}
			p, err := env.PlanNew(tc.args)
			if err != nil {
				t.Fatalf("PlanNew(%q): %v", tc.args, err)
			}
			wantRepo := map[string]string{"proj": proj, "other": other, "": ""}[tc.repo]
			if p.RepoPath != wantRepo || p.Branch != tc.branch || !slices.Equal(p.Reading, tc.readings) {
				t.Errorf("PlanNew(%q) = %+v, want repo %q, branch %q, reading %q", tc.args, p, wantRepo, tc.branch, tc.readings)
			}
			if (tc.repo == "") != (p.Target != target.Target{}) {
				t.Errorf("Target = %+v; only a URL keeps its target", p.Target)
			}
		})
	}
}

func TestPlanNewRepoPath(t *testing.T) {
	fake := &execx.Fake{}
	env, _, other := newEnvWithRepos(t, fake)
	p, err := env.PlanNew([]string{other, "x"})
	if err != nil {
		t.Fatal(err)
	}
	if p.RepoPath != other || p.Reading[0] != "repo: other (path)" {
		t.Errorf("plan = %+v", p)
	}
}

func TestPlanNewErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"outside any repository", []string{"spike"}, "not inside a repository"},
		{"dot outside any repository", []string{"."}, "not inside a repository"},
		{"unknown explicit repository", []string{"nosuch", "x"}, `"nosuch" not found`},
		{"explicit path that is no repository", []string{"/nowhere/x", "x"}, "not a git repository"},
		{"repository URL not cloned", []string{"https://github.com/acme/nosuch"}, "we open https://github.com/acme/nosuch clones it"},
		{"three arguments", []string{"a", "b", "c"}, "optionally a name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env, _, _ := newEnvWithRepos(t, &execx.Fake{})
			_, err := env.PlanNew(tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("PlanNew(%q) error = %v, want one containing %q", tc.args, err, tc.want)
			}
		})
	}
}

// TestPlanNewNextFreeFeature: feature-N skips a local branch, a branch
// only on origin and a branch an environment holds.
func TestPlanNewNextFreeFeature(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "git for-each-ref", Out: "refs/heads/main\nrefs/heads/feature-1\nrefs/remotes/origin/feature-3"},
	}}
	env, proj, other := newEnvWithRepos(t, fake)
	seed(t, env,
		&state.Env{Project: "other", Branch: "feature-2", TmuxSession: "other-feature-2", WorktreePath: "/w/2", RepoPath: other},
		&state.Env{Project: "proj", Branch: "feature-4", TmuxSession: "proj-feature-4", WorktreePath: "/w/4", RepoPath: proj},
	)
	p, err := env.PlanNew([]string{"other"})
	if err != nil {
		t.Fatal(err)
	}
	if p.Branch != "feature-4" {
		t.Errorf("Branch = %q, want feature-4 (1 local, 2 an environment, 3 on origin; 4 in another repository is free)", p.Branch)
	}
	for _, c := range fake.Calls {
		if c.Argv[0] == "git" && c.Argv[1] == "for-each-ref" && c.Dir != other {
			t.Errorf("for-each-ref ran in %q, want %q", c.Dir, other)
		}
	}
}

// TestNewCreatesBranchNeverReadAsID: a name is a branch in its repository,
// even one that is an environment's id or session elsewhere.
func TestNewCreatesBranchNeverReadAsID(t *testing.T) {
	fake := &execx.Fake{Responses: newBranchResponses}
	env, proj, other := newEnvWithRepos(t, fake)
	seed(t, env, &state.Env{Project: "proj", Branch: "x", TmuxSession: "1", WorktreePath: "/w/x", RepoPath: proj})

	p, err := env.PlanNew([]string{"other", "1"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := env.New(p, OpenOptions{NoTerminal: true, Session: "s"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	wt := filepath.Join(env.Cfg.ProjectsPath, "other.1")
	if !res.Created || res.ID != 2 || res.Branch != "1" || res.RepoPath != other || res.Session != "s" || res.WorktreePath != wt {
		t.Errorf("res = %+v", res)
	}
	if !hasCall(fake, "git worktree add -b 1 "+wt+" main") {
		t.Errorf("worktree add missing:\n%s", strings.Join(fake.Joined(), "\n"))
	}
	if rec := loadState(t, env).ByID(2); rec == nil || rec.Branch != "1" || rec.RepoPath != other {
		t.Errorf("record = %+v", rec)
	}
}

// TestNewFindsExistingEnvironment: new is find-or-create like open.
func TestNewFindsExistingEnvironment(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{noLiveSession, {Prefix: "git symbolic-ref --short -q HEAD", Out: "review"}}}
	env, _, other := newEnvWithRepos(t, fake)
	wt := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "other", Branch: "review", TmuxSession: "other-review", WorktreePath: wt, RepoPath: other})

	p, _ := env.PlanNew([]string{"other", "review"})
	res, err := env.New(p, OpenOptions{NoTerminal: true, Wt: "elsewhere"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Created || res.ID != 1 || !slices.Equal(res.IgnoredOverrides, []string{"--wt"}) {
		t.Errorf("res = %+v", res)
	}
}

func TestNewIssueURLWithNameIsBranch(t *testing.T) {
	fake := &execx.Fake{Responses: append([]execx.FakeResponse{
		{Prefix: "gh issue view 59", Out: `{"number":59,"title":"Some title","closedByPullRequestsReferences":[]}`},
	}, newBranchResponses...)}
	env, _ := newTestEnv(t, fake)
	p, err := env.PlanNew([]string{"https://github.com/acme/proj/issues/59", "mine"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := env.New(p, OpenOptions{NoTerminal: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Branch != "mine" || !res.Created {
		t.Errorf("res = %+v", res)
	}
	if rec := loadState(t, env).ByID(1); rec == nil || !slices.Equal(rec.Issues, []string{"https://github.com/acme/proj/issues/59"}) {
		t.Errorf("record = %+v", rec)
	}
}

func TestNoWorkersRefusesCreation(t *testing.T) {
	const msg = "other takes no work environments: its sessions are chats"
	setup := func(t *testing.T, fake *execx.Fake, key string) (*Env, string) {
		env, _, other := newEnvWithRepos(t, fake)
		env.Cfg.Aliases = map[string]string{"ot": "other"}
		env.Cfg.NoWorkers = map[string]string{key: "its sessions are chats"}
		return env, other
	}

	t.Run("new, by repository name", func(t *testing.T) {
		env, _ := setup(t, &execx.Fake{}, "other")
		if _, err := env.PlanNew([]string{"ot", "x"}); err == nil || err.Error() != msg {
			t.Errorf("err = %v, want %q", err, msg)
		}
	})
	t.Run("new, by alias", func(t *testing.T) {
		env, _ := setup(t, &execx.Fake{}, "ot")
		_, err := env.PlanNew([]string{"other"})
		if want := "ot takes no work environments: its sessions are chats"; err == nil || err.Error() != want {
			t.Errorf("err = %v, want %q", err, want)
		}
	})
	t.Run("new, issue URL", func(t *testing.T) {
		env, _ := setup(t, &execx.Fake{}, "other")
		if _, err := env.PlanNew([]string{"https://github.com/acme/other/issues/3"}); err == nil || err.Error() != msg {
			t.Errorf("err = %v, want %q", err, msg)
		}
	})
	t.Run("no hint", func(t *testing.T) {
		env, _ := setup(t, &execx.Fake{}, "other")
		env.Cfg.NoWorkers["other"] = ""
		if _, err := env.PlanNew([]string{"other"}); err == nil || err.Error() != "other takes no work environments" {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("open that would create", func(t *testing.T) {
		fake := &execx.Fake{Responses: newBranchResponses}
		env, _ := setup(t, fake, "other")
		_, err := env.Open(OpenOptions{Target: name("x"), Repo: "other", NoTerminal: true})
		if err == nil || err.Error() != msg {
			t.Errorf("err = %v, want %q", err, msg)
		}
		if hasCall(fake, "git worktree add") || hasCall(fake, "tmux new-session") {
			t.Errorf("a refused open must create nothing:\n%s", strings.Join(fake.Joined(), "\n"))
		}
		if st := loadState(t, env); len(st.Envs) != 0 {
			t.Errorf("registry = %+v", st.Envs)
		}
	})
	t.Run("open of an existing environment", func(t *testing.T) {
		fake := &execx.Fake{Responses: []execx.FakeResponse{noLiveSession, {Prefix: "git symbolic-ref --short -q HEAD", Out: "x"}}}
		env, other := setup(t, fake, "other")
		wt := mkdir(t, filepath.Join(env.Cwd, "wt"))
		seed(t, env, &state.Env{Project: "other", Branch: "x", TmuxSession: "other-x", WorktreePath: wt, RepoPath: other})
		if _, err := env.Open(OpenOptions{Target: name("x"), Repo: "other", NoTerminal: true}); err != nil {
			t.Errorf("an existing environment must still open: %v", err)
		}
	})
}

func TestDeleteCurrentEnvironment(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "tmux show-options", Out: "@workenv 1"},
	}}
	env, repo := newTestEnv(t, fake)
	outer := mkdir(t, filepath.Join(env.Cwd, "outer"))
	inner := mkdir(t, filepath.Join(outer, ".worktrees", "inner"))
	seed(t, env,
		&state.Env{Project: "proj", Branch: "o", TmuxSession: "proj-o", WorktreePath: outer, RepoPath: repo},
		&state.Env{Project: "proj", Branch: "i", TmuxSession: "proj-i", WorktreePath: inner, RepoPath: repo},
	)
	env.Cwd = mkdir(t, filepath.Join(inner, "sub"))

	id, session, err := env.Delete(target.Target{}, "", DeleteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if id != 2 || session != "proj-i" {
		t.Errorf("Delete = (%d, %q), want the innermost environment (2, proj-i)", id, session)
	}
	if st := loadState(t, env); len(st.Envs) != 1 || st.Envs[0].ID != 1 {
		t.Errorf("registry = %+v", st.Envs)
	}

	env.Cwd = t.TempDir()
	if _, _, err := env.Delete(target.Target{}, "", DeleteOptions{}); err == nil || !strings.Contains(err.Error(), "not inside a work environment") {
		t.Errorf("err = %v", err)
	}
}

// TestDeleteKillsSessionLast: delete may run inside the session it deletes,
// so the kill is the last command, sent once the worktree, the branch and
// the record are gone.
func TestDeleteKillsSessionLast(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "tmux show-options -t proj-x @workenv", Out: "@workenv 1"},
	}}
	env, repo := newTestEnv(t, fake)
	wt := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "proj", Branch: "x", TmuxSession: "proj-x", WorktreePath: wt, RepoPath: repo})
	env.Cwd = wt
	recordAtKill := true
	env.R = watchRunner{Fake: fake, before: func(cmd string) {
		if strings.HasPrefix(cmd, "tmux kill-session") {
			recordAtKill = loadState(t, env).ByID(1) != nil
		}
	}}

	if _, _, err := env.Delete(target.Target{}, "", DeleteOptions{DeleteBranch: true}); err != nil {
		t.Fatal(err)
	}
	calls := fake.Joined()
	if last := calls[len(calls)-1]; last != "tmux kill-session -t =proj-x" {
		t.Errorf("last call = %q, want the kill:\n%s", last, strings.Join(calls, "\n"))
	}
	if !hasCall(fake, "git worktree remove "+wt) || !hasCall(fake, "git branch -D x") {
		t.Errorf("teardown incomplete:\n%s", strings.Join(calls, "\n"))
	}
	if recordAtKill {
		t.Error("the record must be saved away before the session is killed")
	}
}

func TestGCKillsSessionsAfterSaving(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		prState(61, "MERGED"),
		{Prefix: "tmux show-options", Out: "@workenv 1"},
	}}
	env, repo := newTestEnv(t, fake)
	seed(t, env, &state.Env{Project: "proj", Branch: "x", TmuxSession: "proj-x", WorktreePath: filepath.Join(env.Cwd, "gone"), RepoPath: repo, PRs: []string{prURL(61)}})
	recordAtKill := true
	env.R = watchRunner{Fake: fake, before: func(cmd string) {
		if strings.HasPrefix(cmd, "tmux kill-session") {
			recordAtKill = loadState(t, env).ByID(1) != nil
		}
	}}
	retired, err := env.GC(GCOptions{})
	if err != nil || len(retired) != 1 {
		t.Fatalf("GC = %+v, %v", retired, err)
	}
	if !hasCall(fake, "tmux kill-session -t =proj-x") || recordAtKill {
		t.Errorf("kill must come after the save (record at kill: %v):\n%s", recordAtKill, strings.Join(fake.Joined(), "\n"))
	}
}

func TestRemoteControlURL(t *testing.T) {
	const url = "https://claude.ai/code/session_01ABCxyz"
	for _, tc := range []struct {
		name, screen, want string
	}{
		{"on the next line", "claude\n/remote-control is active. Code in CLI or at\n" + url + "\n> ", url},
		{"on the same line", "/remote-control is active: " + url, url},
		{"the last one wins", "/remote-control is active\nhttps://claude.ai/code/session_old\n/remote-control is active\n" + url, url},
		{"a link elsewhere is not it", "see " + url + "\n> ", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := findRCURL(tc.screen); got != tc.want {
				t.Errorf("findRCURL = %q, want %q", got, tc.want)
			}
		})
	}

	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "tmux capture-pane -p -J -S - -t proj-x", Out: "/remote-control is active\n" + url},
	}}
	env, _ := newTestEnv(t, fake)
	got, _, err := env.RemoteControlURL("proj-x")
	if err != nil || got != url {
		t.Errorf("RemoteControlURL = %q, %v", got, err)
	}

	fake.Responses = []execx.FakeResponse{{Prefix: "tmux capture-pane", Out: "Is this a project you trust?"}}
	_, screen, err := env.RemoteControlURL("proj-x")
	if !errors.Is(err, ErrRCTimeout) || screen != "Is this a project you trust?" {
		t.Errorf("timeout = %q, %v", screen, err)
	}
}
