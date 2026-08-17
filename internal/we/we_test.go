package we

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"workenv/internal/config"
	"workenv/internal/execx"
	"workenv/internal/state"
	"workenv/internal/target"
	"workenv/internal/wtpath"
)

// newTestEnv builds an Env over a temp projects_path holding one repository
// ("proj", a directory with a .git marker), a scripted fake runner and a
// registry path under the temp root. Cwd defaults to the temp root itself
// (not inside any repository) — tests that need to be "inside" a repository
// set env.Cwd and script git rev-parse --git-common-dir explicitly.
func newTestEnv(t *testing.T, fake *execx.Fake) (*Env, string) {
	t.Helper()
	root := t.TempDir()
	projects := filepath.Join(root, "projects")
	repo := filepath.Join(projects, "proj")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{ProjectsPath: projects, WorktreePath: wtpath.Default, ClaudeCmd: "claude", RemoteWe: "we"}
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

// seed writes envs to the registry (assigning ids from a fresh store, so it
// must be called at most once per test); paths that should exist on disk
// are the caller's business.
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
func repoURL(owner, repo string) target.Target {
	return target.Target{Kind: target.KindRepo, Owner: owner, Repo: repo}
}

// noLiveSession scripts a missing tmux session. Every open runs repair,
// which checks tmux has-session; left unscripted the fake's default success
// would make repair believe a stranger's untagged session is already
// there and refuse it, so tests that are not specifically about that
// guard (TestOpenRefusesUntaggedSession) or about reusing/recreating an
// existing session script this instead.
var noLiveSession = execx.FakeResponse{Prefix: "tmux has-session", Err: errFake}

// newBranchResponses scripts "no local/remote branch anywhere, default
// branch main, no live session" — the create-a-brand-new-branch repair
// path. The broad "git show-ref" prefix matches every show-ref call
// (heads, remotes/origin, for any ref), which is what forces
// gitx.AddWorktree and gitx.EnsureOriginBranch/FetchPRBranch down their
// from-scratch branches instead of the fake's default "success" reading as
// "branch already exists".
var newBranchResponses = []execx.FakeResponse{
	{Prefix: "git show-ref", Err: errFake},
	{Prefix: "git symbolic-ref --short refs/remotes/origin/HEAD", Err: errFake},
	{Prefix: "git symbolic-ref --short HEAD", Out: "main"},
	noLiveSession,
}

func TestOpenIssueCreatesEverything(t *testing.T) {
	fake := &execx.Fake{Responses: append([]execx.FakeResponse{
		{Prefix: "gh issue view 123", Out: `{"number":123,"title":"Add Kafka publisher","closedByPullRequestsReferences":[]}`},
	}, newBranchResponses...)}
	env, repo := newTestEnv(t, fake)

	res, err := env.Open(OpenOptions{Target: issue(123), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	wtPath := filepath.Join(env.Cfg.ProjectsPath, "proj.add-kafka-publisher")
	if res.ID != 1 || res.Project != "proj" || res.Branch != "add-kafka-publisher" ||
		res.Session != "proj-add-kafka-publisher" || res.WorktreePath != wtPath || res.RepoPath != repo || !res.Created {
		t.Errorf("res = %+v", res)
	}
	for _, want := range []string{
		"git worktree add -b add-kafka-publisher " + wtPath + " main",
		"tmux new-session -d -s proj-add-kafka-publisher -c " + wtPath,
		"tmux set-option -t proj-add-kafka-publisher @workenv_id 1",
		"tmux send-keys -t proj-add-kafka-publisher claude Enter",
	} {
		if !hasCall(fake, want) {
			t.Errorf("missing call %q in:\n%s", want, strings.Join(fake.Joined(), "\n"))
		}
	}
	worktreeAddRan := false
	for _, c := range fake.Calls {
		if len(c.Argv) >= 3 && c.Argv[0] == "git" && c.Argv[1] == "worktree" && c.Argv[2] == "add" {
			worktreeAddRan = true
			if c.Dir != repo {
				t.Errorf("git worktree add ran in %q, want the repository directory %q", c.Dir, repo)
			}
		}
	}
	if !worktreeAddRan {
		t.Fatal("git worktree add was never called")
	}
	rec := loadState(t, env).ByID(1)
	if rec == nil || rec.Branch != "add-kafka-publisher" || rec.WorktreePath != wtPath || rec.RepoPath != repo ||
		rec.TmuxSession != "proj-add-kafka-publisher" ||
		!slices.Equal(rec.Issues, []string{"https://github.com/acme/proj/issues/123"}) {
		t.Errorf("record = %+v", rec)
	}
}

func TestOpenIssueUsesLinkedPRHead(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "gh issue view 59", Out: `{"number":59,"title":"Review CLAUDE.md file","closedByPullRequestsReferences":[` +
			`{"number":61,"url":"https://github.com/acme/proj/pull/61","repository":{"name":"proj","owner":{"login":"acme"}}}]}`},
		{Prefix: "gh pr view 61", Out: `{"number":61,"headRefName":"review_claude-file","isCrossRepository":false,"closingIssuesReferences":[]}`},
		noLiveSession,
	}}
	env, _ := newTestEnv(t, fake)

	res, err := env.Open(OpenOptions{Target: issue(59), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Branch != "review_claude-file" || res.Session != "proj-review_claude-file" || !res.Created {
		t.Errorf("res = %+v", res)
	}
	rec := loadState(t, env).ByID(res.ID)
	if rec == nil ||
		!slices.Equal(rec.Issues, []string{"https://github.com/acme/proj/issues/59"}) ||
		!slices.Equal(rec.PRs, []string{"https://github.com/acme/proj/pull/61"}) {
		t.Errorf("issue and its linked PR must both be recorded: %+v", rec)
	}
}

// TestOpenIssueLinksCrossRepositoryPR covers the State section's promise
// that "links to another repository are kept rather than dropped": an
// issue's closedByPullRequestsReferences can point at a fork, and if that
// PR's environment is already registered, opening the issue must land on
// it — not create a fresh one, which per-repo filtering used to do because
// it never even checked the registry for a cross-repository ref.
func TestOpenIssueLinksCrossRepositoryPR(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "gh issue view 59", Out: `{"number":59,"title":"Review CLAUDE.md file","closedByPullRequestsReferences":[` +
			`{"number":77,"url":"https://github.com/other/fork/pull/77","repository":{"name":"fork","owner":{"login":"other"}}}]}`},
		{Prefix: "git symbolic-ref --short -q HEAD", Out: "some-branch"},
		noLiveSession,
	}}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{
		Project: "fork", Branch: "some-branch", TmuxSession: "fork-some-branch",
		WorktreePath: path, RepoPath: filepath.Join(filepath.Dir(repo), "fork"),
		PRs: []string{"https://github.com/other/fork/pull/77"},
	})

	res, err := env.Open(OpenOptions{Target: issue(59), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Created {
		t.Error("a cross-repository linked PR already in the registry must be a hit, not a fresh creation")
	}
	if hasCall(fake, "gh pr view") {
		t.Error("the registry hit must not go on to look up the PR")
	}
	rec := loadState(t, env).ByID(res.ID)
	if rec == nil || !slices.Equal(rec.Issues, []string{"https://github.com/acme/proj/issues/59"}) {
		t.Errorf("issue must be linked onto the cross-repository PR's environment: %+v", rec)
	}
}

func TestOpenPRThenIssueShareOneEnvironment(t *testing.T) {
	t.Run("PR then issue", func(t *testing.T) {
		fake := &execx.Fake{Responses: []execx.FakeResponse{
			{Prefix: "gh pr view 61", Out: `{"number":61,"headRefName":"review_claude-file","isCrossRepository":false,` +
				`"closingIssuesReferences":[{"number":59,"url":"https://github.com/acme/proj/issues/59",` +
				`"repository":{"name":"proj","owner":{"login":"acme"}}}]}`},
			// The second Open call below is a registry hit on the worktree
			// mkdir'd after the first — repairWorktree must see it as a real
			// checkout, not a stray directory.
			{Prefix: "git symbolic-ref --short -q HEAD", Out: "review_claude-file"},
			noLiveSession,
		}}
		env, _ := newTestEnv(t, fake)

		first, err := env.Open(OpenOptions{Target: pull(61), NoTerminal: true})
		if err != nil {
			t.Fatalf("Open PR error: %v", err)
		}
		mkdir(t, first.WorktreePath) // the fake runner never really creates the worktree
		fake.Calls = nil

		second, err := env.Open(OpenOptions{Target: issue(59), NoTerminal: true})
		if err != nil {
			t.Fatalf("Open issue error: %v", err)
		}
		if second.ID != first.ID || second.Created {
			t.Errorf("issue must resolve to the PR's environment: first=%+v second=%+v", first, second)
		}
		if hasCall(fake, "gh") {
			t.Errorf("a registry hit must not call gh at all: %v", fake.Joined())
		}
		if hasCall(fake, "git worktree add") {
			t.Error("must not create a second worktree")
		}
		rec := loadState(t, env).ByID(first.ID)
		if rec == nil ||
			!slices.Equal(rec.Issues, []string{"https://github.com/acme/proj/issues/59"}) ||
			!slices.Equal(rec.PRs, []string{"https://github.com/acme/proj/pull/61"}) {
			t.Errorf("record must carry both refs: %+v", rec)
		}
	})

	t.Run("issue then PR", func(t *testing.T) {
		fake := &execx.Fake{Responses: []execx.FakeResponse{
			{Prefix: "gh issue view 59", Out: `{"number":59,"title":"Review CLAUDE.md file","closedByPullRequestsReferences":[` +
				`{"number":61,"url":"https://github.com/acme/proj/pull/61","repository":{"name":"proj","owner":{"login":"acme"}}}]}`},
			{Prefix: "gh pr view 61", Out: `{"number":61,"headRefName":"review-claude-md-file","isCrossRepository":false,"closingIssuesReferences":[]}`},
			{Prefix: "git symbolic-ref --short -q HEAD", Out: "review-claude-md-file"},
			noLiveSession,
		}}
		env, _ := newTestEnv(t, fake)

		first, err := env.Open(OpenOptions{Target: issue(59), NoTerminal: true})
		if err != nil {
			t.Fatalf("Open issue error: %v", err)
		}
		mkdir(t, first.WorktreePath)
		fake.Calls = nil

		second, err := env.Open(OpenOptions{Target: pull(61), NoTerminal: true})
		if err != nil {
			t.Fatalf("Open PR error: %v", err)
		}
		if second.ID != first.ID || second.Created {
			t.Errorf("PR must resolve to the issue's environment: first=%+v second=%+v", first, second)
		}
		if hasCall(fake, "gh") {
			t.Errorf("a registry hit must not call gh at all: %v", fake.Joined())
		}
		rec := loadState(t, env).ByID(first.ID)
		if rec == nil ||
			!slices.Equal(rec.Issues, []string{"https://github.com/acme/proj/issues/59"}) ||
			!slices.Equal(rec.PRs, []string{"https://github.com/acme/proj/pull/61"}) {
			t.Errorf("record must carry both refs: %+v", rec)
		}
	})
}

func TestOpenForkPRUsesPRBranch(t *testing.T) {
	fake := &execx.Fake{Responses: append([]execx.FakeResponse{
		{Prefix: "gh pr view 456", Out: `{"number":456,"headRefName":"fix/crash","isCrossRepository":true,"closingIssuesReferences":[]}`},
	}, newBranchResponses...)}
	env, _ := newTestEnv(t, fake)

	res, err := env.Open(OpenOptions{Target: pull(456), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Branch != "pr-456" || res.Session != "proj-pr-456" || !res.Created {
		t.Errorf("res = %+v", res)
	}
	if !hasCall(fake, "git fetch origin pull/456/head:refs/heads/pr-456") {
		t.Errorf("missing PR head fetch:\n%s", strings.Join(fake.Joined(), "\n"))
	}
	rec := loadState(t, env).ByID(res.ID)
	if rec == nil || rec.Branch != "pr-456" || !slices.Equal(rec.PRs, []string{"https://github.com/acme/proj/pull/456"}) {
		t.Errorf("record = %+v", rec)
	}
}

func TestOpenSameRepoPRFetchesHead(t *testing.T) {
	fake := &execx.Fake{Responses: append([]execx.FakeResponse{
		{Prefix: "gh pr view 456", Out: `{"number":456,"headRefName":"fix/crash","isCrossRepository":false,"closingIssuesReferences":[]}`},
	}, newBranchResponses...)}
	env, _ := newTestEnv(t, fake)

	res, err := env.Open(OpenOptions{Target: pull(456), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Branch != "fix/crash" || res.Session != "proj-fix-crash" || !res.Created {
		t.Errorf("res = %+v", res)
	}
	if !hasCall(fake, "git fetch origin +refs/heads/fix/crash:refs/remotes/origin/fix/crash") {
		t.Errorf("missing head fetch:\n%s", strings.Join(fake.Joined(), "\n"))
	}
}

func TestOpenRepoURLAdoptsDefaultBranchWorktree(t *testing.T) {
	fake := &execx.Fake{}
	env, repo := newTestEnv(t, fake)
	fake.Responses = []execx.FakeResponse{
		{Prefix: "git symbolic-ref --short refs/remotes/origin/HEAD", Out: "origin/main"},
		{Prefix: "git worktree list", Out: "worktree " + repo + "\nHEAD abc\nbranch refs/heads/main\n"},
		{Prefix: "git symbolic-ref --short -q HEAD", Out: "main"},
		noLiveSession,
	}

	res, err := env.Open(OpenOptions{Target: repoURL("acme", "proj"), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.WorktreePath != repo || res.Branch != "main" || res.Session != "proj-main" || !res.Created {
		t.Errorf("res = %+v", res)
	}
	if hasCall(fake, "git worktree add") {
		t.Error("the main working tree must be adopted, not recreated")
	}
	if rec := loadState(t, env).ByID(res.ID); rec == nil || rec.WorktreePath != repo {
		t.Errorf("record = %+v", rec)
	}
}

func TestOpenRepoURLClonesWhenMissing(t *testing.T) {
	fake := &execx.Fake{Responses: newBranchResponses}
	env, _ := newTestEnv(t, fake)

	res, err := env.Open(OpenOptions{Target: repoURL("acme", "newrepo"), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	dest := filepath.Join(env.Cfg.ProjectsPath, "newrepo")
	if !hasCall(fake, "gh repo clone acme/newrepo "+dest) {
		t.Errorf("missing clone into <projects>/newrepo:\n%s", strings.Join(fake.Joined(), "\n"))
	}
	for _, c := range fake.Joined() {
		if strings.Contains(c, "--bare") {
			t.Errorf("clone must not be bare: %q", c)
		}
	}
	if res.RepoPath != dest || res.Project != "newrepo" {
		t.Errorf("res = %+v, want repo path %s", res, dest)
	}
	if rec := loadState(t, env).ByID(res.ID); rec == nil || rec.RepoPath != dest {
		t.Errorf("record = %+v", rec)
	}
}

func TestOpenPlainNameCreatesBranch(t *testing.T) {
	t.Run("inside the repo", func(t *testing.T) {
		fake := &execx.Fake{}
		env, repo := newTestEnv(t, fake)
		env.Cwd = repo
		fake.Responses = append([]execx.FakeResponse{
			{Prefix: "git rev-parse --path-format=absolute --git-common-dir", Out: filepath.Join(repo, ".git")},
		}, newBranchResponses...)

		res, err := env.Open(OpenOptions{Target: name("spike-latency"), NoTerminal: true})
		if err != nil {
			t.Fatalf("Open error: %v", err)
		}
		wtPath := filepath.Join(env.Cfg.ProjectsPath, "proj.spike-latency")
		if res.Branch != "spike-latency" || res.Project != "proj" || res.Session != "proj-spike-latency" ||
			res.WorktreePath != wtPath || !res.Created {
			t.Errorf("res = %+v", res)
		}
	})

	t.Run("via --repo name", func(t *testing.T) {
		fake := &execx.Fake{Responses: newBranchResponses}
		env, _ := newTestEnv(t, fake)

		res, err := env.Open(OpenOptions{Target: name("spike-latency"), Repo: "proj", NoTerminal: true})
		if err != nil {
			t.Fatalf("Open error: %v", err)
		}
		if res.Branch != "spike-latency" || res.Project != "proj" || !res.Created {
			t.Errorf("res = %+v", res)
		}
	})

	t.Run("via --repo path", func(t *testing.T) {
		fake := &execx.Fake{}
		env, repo := newTestEnv(t, fake)
		fake.Responses = append([]execx.FakeResponse{
			{Prefix: "git rev-parse --path-format=absolute --git-common-dir", Out: filepath.Join(repo, ".git")},
		}, newBranchResponses...)

		res, err := env.Open(OpenOptions{Target: name("spike-latency"), Repo: repo, NoTerminal: true})
		if err != nil {
			t.Fatalf("Open error: %v", err)
		}
		if res.Branch != "spike-latency" || res.RepoPath != repo || !res.Created {
			t.Errorf("res = %+v", res)
		}
	})
}

// TestOpenExplicitRepoDoesNotFallBackGlobally covers the design spec's
// "an explicit --repo scopes the search" rule (updated in commit 381e774):
// unlike a bare plain-name lookup, a --repo that matches no environment
// must not fall back to a same-named branch elsewhere, and a --repo that
// does not even resolve to a repository must surface that error rather
// than let the (skipped) fallback silently swallow it.
func TestOpenExplicitRepoDoesNotFallBackGlobally(t *testing.T) {
	t.Run("valid --repo without the branch does not fall back", func(t *testing.T) {
		fake := &execx.Fake{}
		env, repo := newTestEnv(t, fake)
		other := filepath.Join(filepath.Dir(repo), "other")
		if err := os.MkdirAll(filepath.Join(other, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		path := mkdir(t, filepath.Join(env.Cwd, "wt"))
		seed(t, env, &state.Env{Project: "other", Branch: "spike", TmuxSession: "other-spike", WorktreePath: path, RepoPath: other})

		_, err := env.Open(OpenOptions{Target: name("spike"), Repo: "proj", AttachOnly: true, NoTerminal: true})
		if err == nil || !strings.Contains(err.Error(), "we open") {
			t.Fatalf("expected a not-found error scoped to --repo proj, got %v", err)
		}
	})

	t.Run("invalid --repo is not swallowed by the global fallback", func(t *testing.T) {
		fake := &execx.Fake{}
		env, repo := newTestEnv(t, fake)
		other := filepath.Join(filepath.Dir(repo), "other")
		if err := os.MkdirAll(filepath.Join(other, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		path := mkdir(t, filepath.Join(env.Cwd, "wt"))
		// A branch of the same name exists elsewhere; without scoping this
		// would be the "unique match anywhere" fallback hit.
		seed(t, env, &state.Env{Project: "other", Branch: "spike", TmuxSession: "other-spike", WorktreePath: path, RepoPath: other})

		_, err := env.Open(OpenOptions{Target: name("spike"), Repo: "nosuch", AttachOnly: true, NoTerminal: true})
		if err == nil || strings.Contains(err.Error(), "we open") {
			t.Fatalf("expected a repository-not-found error for --repo nosuch, got %v", err)
		}
		if !strings.Contains(err.Error(), "nosuch") {
			t.Fatalf("expected the error to name the bad --repo value, got %v", err)
		}
	})
}

// TestOpenPlainAllDigitsRefusesToCreateBranch covers the amended spec's
// Resolution rule: after every id/session/branch lookup above has already
// come up empty, an all-digits plain name can only be a stale id (e.g.
// re-running "we open 7" from shell history after "we delete 7") — never a
// branch worth creating — unless --branch says the digits are genuinely
// meant as one.
func TestOpenPlainAllDigitsRefusesToCreateBranch(t *testing.T) {
	fake := &execx.Fake{}
	env, _ := newTestEnv(t, fake)

	_, err := env.Open(OpenOptions{Target: name("7"), Repo: "proj", NoTerminal: true})
	if err == nil || !strings.Contains(err.Error(), "no work environment has id 7") {
		t.Fatalf("expected a stale-id error naming id 7, got %v", err)
	}
	if hasCall(fake, "git worktree add") {
		t.Error("must not create a branch literally named \"7\"")
	}
	if len(loadState(t, env).Envs) != 0 {
		t.Error("the refusal must not have created a record")
	}

	fake2 := &execx.Fake{Responses: newBranchResponses}
	env2, _ := newTestEnv(t, fake2)
	res, err := env2.Open(OpenOptions{Target: name("7"), Repo: "proj", Branch: "seven", NoTerminal: true})
	if err != nil {
		t.Fatalf("Open with --branch error: %v", err)
	}
	if res.Branch != "seven" || !res.Created {
		t.Errorf("res = %+v, want --branch to override the all-digits refusal", res)
	}
}

func TestOpenByIDAndBySession(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "git symbolic-ref --short -q HEAD", Out: "feature-123"},
		noLiveSession,
	}}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "proj", Branch: "feature-123", TmuxSession: "proj-feature-123", WorktreePath: path, RepoPath: repo})

	byID, err := env.Open(OpenOptions{Target: name("1"), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open by id error: %v", err)
	}
	if byID.ID != 1 || byID.Created {
		t.Errorf("by id: res = %+v", byID)
	}

	bySession, err := env.Open(OpenOptions{Target: name("proj-feature-123"), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open by session error: %v", err)
	}
	if bySession.ID != 1 || bySession.Created {
		t.Errorf("by session: res = %+v", bySession)
	}
	if hasCall(fake, "git worktree add") {
		t.Error("an existing environment must not be recreated")
	}
	if len(loadState(t, env).Envs) != 1 {
		t.Error("neither lookup should have created a new record")
	}
}

func TestAttachNeverCreates(t *testing.T) {
	fake := &execx.Fake{}
	env, _ := newTestEnv(t, fake)

	for _, tgt := range []target.Target{name("999"), name("proj-nope"), name("nope-branch")} {
		_, err := env.Open(OpenOptions{Target: tgt, Repo: "proj", AttachOnly: true, NoTerminal: true})
		if err == nil || !strings.Contains(err.Error(), "we open") {
			t.Errorf("attach of %v must fail mentioning we open, got %v", tgt, err)
		}
	}
	if hasCall(fake, "git worktree add") || hasCall(fake, "tmux new-session") {
		t.Errorf("attach must not create anything:\n%s", strings.Join(fake.Joined(), "\n"))
	}
	if len(loadState(t, env).Envs) != 0 {
		t.Error("attach must not record anything")
	}

	// An issue in a repository that has never been cloned locally: attach
	// must still point at "we open", not surface a raw clone-target error,
	// and must not clone.
	fake.Responses = []execx.FakeResponse{
		{Prefix: "gh issue view 9", Out: `{"number":9,"title":"Ghost","closedByPullRequestsReferences":[]}`},
	}
	_, err := env.Open(OpenOptions{
		Target:     target.Target{Kind: target.KindIssue, Owner: "acme", Repo: "nowhere", Number: 9},
		AttachOnly: true, NoTerminal: true,
	})
	if err == nil || !strings.Contains(err.Error(), "we open") {
		t.Fatalf("attach of an issue in an uncloned repo must fail pointing at we open, got %v", err)
	}
	if hasCall(fake, "gh repo clone") {
		t.Error("attach must not clone a missing project")
	}
}

func TestOpenRefreshesRenamedBranch(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "git symbolic-ref --short -q HEAD", Out: "renamed"},
		noLiveSession,
	}}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "proj", Branch: "old", TmuxSession: "proj-old", WorktreePath: path, RepoPath: repo})

	res, err := env.Open(OpenOptions{Target: name("proj-old"), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Branch != "renamed" {
		t.Errorf("Branch = %q, want the worktree's current branch", res.Branch)
	}
	if rec := loadState(t, env).ByID(res.ID); rec == nil || rec.Branch != "renamed" {
		t.Errorf("stored branch = %+v, want refreshed to \"renamed\"", rec)
	}
}

func TestOpenRecreatesMissingWorktreeAndSession(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{noLiveSession}}
	env, repo := newTestEnv(t, fake)
	gone := filepath.Join(env.Cwd, "gone")
	seed(t, env, &state.Env{Project: "proj", Branch: "feature-123", TmuxSession: "proj-feature-123", WorktreePath: gone, RepoPath: repo})

	res, err := env.Open(OpenOptions{Target: name("proj-feature-123"), NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Created {
		t.Error("repairing an existing environment must not report Created")
	}
	for _, want := range []string{
		"git worktree prune",
		"git worktree add " + gone + " feature-123", // branch already exists locally: plain add, no -b
		"tmux new-session -d -s proj-feature-123 -c " + gone,
		"tmux set-option -t proj-feature-123 @workenv_id " + strconv.Itoa(res.ID),
		"tmux send-keys -t proj-feature-123 claude Enter",
	} {
		if !hasCall(fake, want) {
			t.Errorf("missing %q in:\n%s", want, strings.Join(fake.Joined(), "\n"))
		}
	}
}

// TestOpenRejectsStrayDirectoryAtWorktreePath covers the fix for
// repairWorktree treating any directory at the recorded path as a valid
// worktree: a stray directory (left behind by whatever removed the real
// worktree, or created by hand) is not a git checkout, so
// gitx.CurrentBranch reports no branch for it — the same signal a detached,
// non-worktree directory gives. Starting a session there instead of a real
// checkout would be worse than the missing-directory case repair exists to
// handle, so Open must refuse with a useful error instead of silently
// adopting it.
func TestOpenRejectsStrayDirectoryAtWorktreePath(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{noLiveSession}}
	env, repo := newTestEnv(t, fake)
	stray := mkdir(t, filepath.Join(env.Cwd, "stray"))
	seed(t, env, &state.Env{Project: "proj", Branch: "feature-123", TmuxSession: "proj-feature-123", WorktreePath: stray, RepoPath: repo})

	_, err := env.Open(OpenOptions{Target: name("proj-feature-123"), NoTerminal: true})
	if err == nil || !strings.Contains(err.Error(), stray) || !strings.Contains(err.Error(), "not a git checkout") {
		t.Fatalf("expected an error naming %q as not a git checkout, got %v", stray, err)
	}
	if hasCall(fake, "tmux new-session") {
		t.Error("must not start a session rooted in a directory that is not a checkout")
	}
}

// TestOpenRefusesUntaggedSessionOnHit covers the Adoption paragraph's
// hit-path refusal (the design doc's amendment in 9e59f0b): --session only
// takes effect on creation, so on a hit it cannot help. The message must
// name the conflicting session and the environment, and point at renaming
// or killing the stray session, or `we delete <id>` — never suggest
// --session, which the caller cannot use here.
func TestOpenRefusesUntaggedSessionOnHit(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "git symbolic-ref --short -q HEAD", Out: "feature-123"},
		{Prefix: "tmux show-options", Out: ""}, // has-session succeeds by default; not @workenv-tagged
	}}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "proj", Branch: "feature-123", TmuxSession: "proj-feature-123", WorktreePath: path, RepoPath: repo})

	_, err := env.Open(OpenOptions{Target: name("proj-feature-123"), NoTerminal: true})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "--session") {
		t.Errorf("--session is a creation-only override and cannot help on a hit; message must not suggest it: %v", err)
	}
	for _, want := range []string{"proj-feature-123", "environment 1", "we delete 1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the message to mention %q, got %v", want, err)
		}
	}
}

// TestOpenCreateRefusesUntaggedSession covers the creation-path half of the
// same Adoption rule: a live untagged session already using the name a
// fresh environment would take is someone else's, and here --session
// genuinely helps (it picks a different name), so the original suggestion
// still applies.
func TestOpenCreateRefusesUntaggedSession(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "git show-ref", Err: errFake},
		{Prefix: "git symbolic-ref --short refs/remotes/origin/HEAD", Err: errFake},
		{Prefix: "git symbolic-ref --short HEAD", Out: "main"},
		// tmux has-session and show-options are deliberately left
		// unscripted: both default to success, i.e. a live session that is
		// not @workenv-tagged — the untagged-stranger scenario, on create.
	}}
	env, _ := newTestEnv(t, fake)

	_, err := env.Open(OpenOptions{Target: name("spike"), Repo: "proj", NoTerminal: true})
	if err == nil || !strings.Contains(err.Error(), "--session") {
		t.Fatalf("expected an error mentioning --session, got %v", err)
	}
}

func TestOpenOverridesOnCreate(t *testing.T) {
	fake := &execx.Fake{Responses: newBranchResponses}
	env, _ := newTestEnv(t, fake)

	res, err := env.Open(OpenOptions{Target: name("spike"), Repo: "proj", Branch: "feat/x", Session: "custom-session", NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Branch != "feat/x" || res.Session != "custom-session" || !res.Created {
		t.Errorf("res = %+v", res)
	}
	if rec := loadState(t, env).ByID(res.ID); rec == nil || rec.Branch != "feat/x" || rec.TmuxSession != "custom-session" {
		t.Errorf("record = %+v", rec)
	}

	fake2 := &execx.Fake{Responses: newBranchResponses}
	env2, _ := newTestEnv(t, fake2)
	res2, err := env2.Open(OpenOptions{Target: name("spike2"), Repo: "proj", Wt: "scratch", NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	wantWt := filepath.Join(env2.Cfg.ProjectsPath, "proj.scratch")
	if res2.WorktreePath != wantWt || res2.Branch != "spike2" {
		t.Errorf("res2 = %+v, want worktree %q (bare --wt name)", res2, wantWt)
	}

	fake3 := &execx.Fake{Responses: newBranchResponses}
	env3, _ := newTestEnv(t, fake3)
	abs := filepath.Join(env3.Cwd, "elsewhere")
	res3, err := env3.Open(OpenOptions{Target: name("spike3"), Repo: "proj", Wt: abs, NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res3.WorktreePath != abs {
		t.Errorf("WorktreePath = %q, want %q (verbatim --wt path)", res3.WorktreePath, abs)
	}
	if rec := loadState(t, env3).ByID(res3.ID); rec == nil || rec.WorktreePath != abs {
		t.Errorf("record = %+v", rec)
	}
}

func TestOpenWtOverridePathIsCleaned(t *testing.T) {
	fake := &execx.Fake{Responses: newBranchResponses}
	env, repo := newTestEnv(t, fake)

	// A relative --wt resolves against the repository, and a trailing
	// slash (or any other untidiness) is cleaned away, so git and a later
	// os.Stat/ByWorktree lookup all agree on the exact same string.
	res, err := env.Open(OpenOptions{Target: name("spike"), Repo: "proj", Wt: "../scratch/x/", NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	want := filepath.Clean(filepath.Join(repo, "../scratch/x"))
	if res.WorktreePath != want {
		t.Errorf("WorktreePath = %q, want %q", res.WorktreePath, want)
	}
	if !hasCall(fake, "git worktree add -b spike "+want) {
		t.Errorf("git worktree add must use the cleaned path:\n%s", strings.Join(fake.Joined(), "\n"))
	}
}

func TestOpenSanitizesSessionOverride(t *testing.T) {
	fake := &execx.Fake{Responses: newBranchResponses}
	env, _ := newTestEnv(t, fake)

	res, err := env.Open(OpenOptions{Target: name("spike"), Repo: "proj", Session: "a:b", NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Session != "a-b" {
		t.Errorf("Session = %q, want the sanitized \"a-b\" (tmux reserves ':')", res.Session)
	}
	if rec := loadState(t, env).ByID(res.ID); rec == nil || rec.TmuxSession != "a-b" {
		t.Errorf("record = %+v", rec)
	}
}

func TestOpenReportsIgnoredOverridesOnHit(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "git symbolic-ref --short -q HEAD", Out: "feature-123"},
		noLiveSession,
	}}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "proj", Branch: "feature-123", TmuxSession: "proj-feature-123", WorktreePath: path, RepoPath: repo})

	res, err := env.Open(OpenOptions{
		Target: name("proj-feature-123"), Branch: "other", Session: "other-session", Wt: "/somewhere", NoTerminal: true,
	})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	if res.Created {
		t.Error("a hit must not report Created")
	}
	want := []string{"--branch", "--session", "--wt"}
	if !slices.Equal(res.IgnoredOverrides, want) {
		t.Errorf("IgnoredOverrides = %v, want %v", res.IgnoredOverrides, want)
	}
	rec := loadState(t, env).ByID(res.ID)
	if rec == nil || rec.Branch != "feature-123" || rec.TmuxSession != "proj-feature-123" || rec.WorktreePath != path {
		t.Errorf("overrides on a hit must not change the record: %+v", rec)
	}
}

func TestOpenRejectsSessionAndWorktreeCollisions(t *testing.T) {
	fake := &execx.Fake{Responses: newBranchResponses}
	env, repo := newTestEnv(t, fake)
	other := mkdir(t, filepath.Join(env.Cwd, "other"))
	seed(t, env, &state.Env{Project: "proj", Branch: "existing", TmuxSession: "taken-session", WorktreePath: other, RepoPath: repo})

	_, err := env.Open(OpenOptions{Target: name("spike"), Repo: "proj", Session: "taken-session", NoTerminal: true})
	if err == nil || !strings.Contains(err.Error(), "environment 1") || !strings.Contains(err.Error(), "--session") {
		t.Fatalf("expected a session-collision error naming environment 1, got %v", err)
	}
	if len(loadState(t, env).Envs) != 1 {
		t.Error("the rejected create must not have added a record")
	}

	_, err = env.Open(OpenOptions{Target: name("spike2"), Repo: "proj", Wt: other, NoTerminal: true})
	if err == nil || !strings.Contains(err.Error(), "environment 1") || !strings.Contains(err.Error(), "--wt") {
		t.Fatalf("expected a worktree-collision error naming environment 1, got %v", err)
	}
	if len(loadState(t, env).Envs) != 1 {
		t.Error("the rejected create must not have added a record")
	}
}

// TestOpenWtRefusesWhenBranchAlreadyCheckedOutElsewhere covers the case a
// worktree for the branch already exists (made by hand or by worktrunk, so
// it is not yet in the registry) and --wt points somewhere else: git itself
// would refuse "git worktree add" with a confusing "already checked out at
// ..." error, so `we` must catch it first and name the existing path
// itself.
func TestOpenWtRefusesWhenBranchAlreadyCheckedOutElsewhere(t *testing.T) {
	fake := &execx.Fake{}
	env, repo := newTestEnv(t, fake)
	existing := filepath.Join(filepath.Dir(repo), "elsewhere-checkout")
	fake.Responses = []execx.FakeResponse{
		{Prefix: "git worktree list", Out: "worktree " + existing + "\nHEAD abc\nbranch refs/heads/spike\n"},
	}

	_, err := env.Open(OpenOptions{Target: name("spike"), Repo: "proj", Wt: "somewhere-else", NoTerminal: true})
	if err == nil || !strings.Contains(err.Error(), existing) {
		t.Fatalf("expected an error naming the existing checkout %q, got %v", existing, err)
	}
	if hasCall(fake, "git worktree add") {
		t.Error("must not attempt git worktree add when the branch is already checked out elsewhere")
	}
	if len(loadState(t, env).Envs) != 0 {
		t.Error("the refusal must not have created a record")
	}
}

func TestOpenUsesConfiguredTemplate(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory available")
	}
	fake := &execx.Fake{Responses: newBranchResponses}
	env, _ := newTestEnv(t, fake)
	env.Cfg.WorktreePath = "~/wt/{{ .project }}/{{ .branch | sanitize }}"

	res, err := env.Open(OpenOptions{Target: name("feat/x"), Repo: "proj", NoTerminal: true})
	if err != nil {
		t.Fatalf("Open error: %v", err)
	}
	want := filepath.Join(home, "wt", "proj", "feat-x")
	if res.WorktreePath != want {
		t.Errorf("WorktreePath = %q, want %q", res.WorktreePath, want)
	}
	if rec := loadState(t, env).ByID(res.ID); rec == nil || rec.WorktreePath != want {
		t.Errorf("record = %+v", rec)
	}
}

func TestDeleteRemovesEverything(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "tmux show-options -t proj-feature-123 @workenv", Out: "@workenv 1"},
	}}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "proj", Branch: "feature-123", TmuxSession: "proj-feature-123", WorktreePath: path, RepoPath: repo})

	id, session, err := env.Delete(name("proj-feature-123"), "", DeleteOptions{DeleteBranch: true})
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if id != 1 || session != "proj-feature-123" {
		t.Errorf("Delete = (%d, %q), want (1, \"proj-feature-123\") — the resolved id and session, not the raw target", id, session)
	}
	for _, want := range []string{
		"tmux kill-session -t =proj-feature-123",
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

// TestDeleteSkipsUntaggedSessionButFinishesTeardown covers the Adoption
// rule ("an untagged session is never adopted or killed") applying to
// delete too: a record's tmux_session name can collide with a live
// stranger's session (a coincidence, or a hand-edited registry), and that
// session must survive even though the rest of the teardown — worktree and
// record — still proceeds.
func TestDeleteSkipsUntaggedSessionButFinishesTeardown(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "tmux show-options", Out: ""}, // has-session succeeds by default; not @workenv-tagged
	}}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "proj", Branch: "feature-123", TmuxSession: "proj-feature-123", WorktreePath: path, RepoPath: repo})

	if _, _, err := env.Delete(name("proj-feature-123"), "", DeleteOptions{}); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if hasCall(fake, "tmux kill-session") {
		t.Errorf("an untagged session must never be killed:\n%s", strings.Join(fake.Joined(), "\n"))
	}
	if !hasCall(fake, "git worktree remove "+path) {
		t.Error("teardown must still remove the worktree")
	}
	if len(loadState(t, env).Envs) != 0 {
		t.Error("teardown must still drop the record")
	}
}

// TestDeleteOnMainWorkingTreeKeepsWorktreeAndBranch covers the "Just open a
// project" / "Finish up" combination from the design doc's use cases: `we
// open <repo-url>` adopts a repository's main working tree, and `git
// worktree remove` refuses that (even with --force), so delete must skip
// both the worktree removal and (for the same reason) --delete-branch
// rather than fail partway through.
func TestDeleteOnMainWorkingTreeKeepsWorktreeAndBranch(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "tmux show-options -t proj-main @workenv", Out: "@workenv 1"},
	}}
	env, repo := newTestEnv(t, fake)
	seed(t, env, &state.Env{Project: "proj", Branch: "main", TmuxSession: "proj-main", WorktreePath: repo, RepoPath: repo})

	if _, _, err := env.Delete(name("proj-main"), "", DeleteOptions{DeleteBranch: true}); err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if hasCall(fake, "git worktree remove") {
		t.Errorf("the main working tree must never be removed:\n%s", strings.Join(fake.Joined(), "\n"))
	}
	if hasCall(fake, "git branch -D") {
		t.Errorf("the main working tree's branch must never be deleted:\n%s", strings.Join(fake.Joined(), "\n"))
	}
	if !hasCall(fake, "tmux kill-session -t =proj-main") {
		t.Error("the session must still be killed")
	}
	if len(loadState(t, env).Envs) != 0 {
		t.Error("the record must still be removed")
	}
}

func TestDeleteByURLAndByID(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "tmux show-options -t proj-x @workenv", Out: "@workenv 1"},
	}}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{
		Project: "proj", Branch: "x", TmuxSession: "proj-x", WorktreePath: path, RepoPath: repo,
		Issues: []string{"https://github.com/acme/proj/issues/59"},
	})

	id, session, err := env.Delete(issue(59), "", DeleteOptions{KeepWorktree: true})
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if session != "proj-x" {
		t.Errorf("session = %q, want the resolved session \"proj-x\"", session)
	}
	if !hasCall(fake, "tmux kill-session -t =proj-x") {
		t.Error("session must be killed")
	}
	if hasCall(fake, "git worktree remove") {
		t.Error("--keep-worktree must not remove the worktree")
	}
	rec := loadState(t, env).ByRef("https://github.com/acme/proj/issues/59")
	if rec == nil {
		t.Fatal("--keep-worktree keeps the record")
	}
	if id != rec.ID {
		t.Errorf("id = %d, want the resolved environment's id %d", id, rec.ID)
	}

	if _, _, err := env.Delete(name(strconv.Itoa(rec.ID)), "", DeleteOptions{}); err != nil {
		t.Fatalf("Delete by id error: %v", err)
	}
	if len(loadState(t, env).Envs) != 0 {
		t.Error("record must be removed after the full delete")
	}
}

func TestDeleteExplicitRepoDoesNotFallBackGlobally(t *testing.T) {
	fake := &execx.Fake{}
	env, repo := newTestEnv(t, fake)
	other := filepath.Join(filepath.Dir(repo), "other")
	if err := os.MkdirAll(filepath.Join(other, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "other", Branch: "x", TmuxSession: "other-x", WorktreePath: path, RepoPath: other})

	_, _, err := env.Delete(name("x"), "proj", DeleteOptions{})
	if err == nil {
		t.Fatal("expected an error: --repo proj must not fall back to another repository's environment")
	}
	if hasCall(fake, "tmux kill-session") || hasCall(fake, "git worktree remove") {
		t.Errorf("must not touch another repository's environment:\n%s", strings.Join(fake.Joined(), "\n"))
	}
	if loadState(t, env).ByBranch(other, "x") == nil {
		t.Error("the other repository's record must remain")
	}
}

func TestDeleteKillsStrayTaggedSession(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "tmux show-options -t proj-orphan @workenv", Out: "@workenv 1"},
	}}
	env, _ := newTestEnv(t, fake)

	id, session, err := env.Delete(name("proj-orphan"), "", DeleteOptions{})
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if id != 0 || session != "proj-orphan" {
		t.Errorf("Delete = (%d, %q), want (0, \"proj-orphan\") — a stray session has no id", id, session)
	}
	if !hasCall(fake, "tmux kill-session -t =proj-orphan") {
		t.Errorf("stray tagged session must be killed:\n%s", strings.Join(fake.Joined(), "\n"))
	}

	fake2 := &execx.Fake{Responses: []execx.FakeResponse{{Prefix: "tmux show-options", Out: ""}}}
	env2, _ := newTestEnv(t, fake2)
	if _, _, err := env2.Delete(name("proj-stranger"), "", DeleteOptions{}); err == nil {
		t.Error("expected an error: an untagged session must not be adopted or killed")
	}
	if hasCall(fake2, "tmux kill-session") {
		t.Error("must not kill an untagged session")
	}
}

func TestListReportsStateExistsCurrentAndRefs(t *testing.T) {
	fake := &execx.Fake{}
	env, repo := newTestEnv(t, fake)
	live := mkdir(t, filepath.Join(env.Cwd, "live"))
	seed(t, env,
		&state.Env{Project: "proj", Branch: "b", TmuxSession: "proj-b", WorktreePath: filepath.Join(env.Cwd, "gone"), RepoPath: repo},
		&state.Env{
			Project: "proj", Branch: "a", TmuxSession: "proj-a", WorktreePath: live, RepoPath: repo,
			Issues: []string{"https://github.com/acme/proj/issues/59"}, PRs: []string{"https://github.com/acme/proj/pull/61"},
		},
	)
	env.Cwd = live
	fake.Responses = []execx.FakeResponse{
		{Prefix: "tmux list-sessions", Out: "proj-a\t2\t" + live + "\t1\n"},
		{Prefix: "git symbolic-ref --short -q HEAD", Out: "a-renamed"},
	}

	items, err := env.List()
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %+v", len(items), items)
	}
	b, a := items[0], items[1]
	if b.ID != 1 || b.SessionState != "none" || b.Exists || b.Current {
		t.Errorf("b (missing directory) = %+v", b)
	}
	if a.ID != 2 || a.SessionState != "attached" || !a.Exists || !a.Current || a.Branch != "a-renamed" ||
		!slices.Equal(a.Issues, []string{"https://github.com/acme/proj/issues/59"}) ||
		!slices.Equal(a.PRs, []string{"https://github.com/acme/proj/pull/61"}) {
		t.Errorf("a (live, attached, current) = %+v", a)
	}
	// a's branch actually changed (live git said "a-renamed" where the
	// registry had "a"), so it must be persisted; b's worktree is missing
	// and was never checked, so its stored branch is untouched.
	if rec := loadState(t, env).ByID(2); rec == nil || rec.Branch != "a-renamed" {
		t.Errorf("stored branch for id 2 = %+v, want refreshed to \"a-renamed\"", rec)
	}
	if rec := loadState(t, env).ByID(1); rec == nil || rec.Branch != "b" {
		t.Errorf("stored branch for id 1 = %+v, want untouched \"b\" (its worktree never existed)", rec)
	}
}

// TestListRefreshesAndPersistsBranchFromGit isolates the "Rename the branch
// mid-flight" use case from the design doc: `git branch -m` outside `we`,
// then `we ls` must show the renamed branch, read live from the worktree —
// and, per the Model section ("refreshed ... whenever an environment is
// opened or listed") and the use case's own "the record is updated", List
// must persist that refreshed branch back to the registry, exactly as open
// does on a hit.
func TestListRefreshesAndPersistsBranchFromGit(t *testing.T) {
	fake := &execx.Fake{}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "proj", Branch: "old-name", TmuxSession: "proj-old-name", WorktreePath: path, RepoPath: repo})
	fake.Responses = []execx.FakeResponse{{Prefix: "git symbolic-ref --short -q HEAD", Out: "renamed"}}

	items, err := env.List()
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(items) != 1 || items[0].Branch != "renamed" {
		t.Errorf("items = %+v, want a single item with the live branch \"renamed\"", items)
	}
	if rec := loadState(t, env).ByID(1); rec == nil || rec.Branch != "renamed" {
		t.Errorf("stored record = %+v, want the branch persisted as \"renamed\"", rec)
	}
}

// TestListDoesNotSaveWhenNothingChanged is the flip side of
// TestListRefreshesAndPersistsBranchFromGit: when the live branch matches
// what is already stored, List must not touch the registry file at all —
// persisting a refresh must not turn into a write on every `ls`.
func TestListDoesNotSaveWhenNothingChanged(t *testing.T) {
	fake := &execx.Fake{}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{Project: "proj", Branch: "same", TmuxSession: "proj-same", WorktreePath: path, RepoPath: repo})
	fake.Responses = []execx.FakeResponse{{Prefix: "git symbolic-ref --short -q HEAD", Out: "same"}}

	before, err := os.ReadFile(env.StatePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	items, err := env.List()
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(items) != 1 || items[0].Branch != "same" {
		t.Errorf("items = %+v, want a single item with branch \"same\"", items)
	}
	after, err := os.ReadFile(env.StatePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(before) != string(after) {
		t.Error("List must not write to the registry when no branch actually changed")
	}
}

// TestShowResolvesLikeDelete is not one of the enumerated resolution
// scenarios (those are exercised through Open/Delete, which share the same
// registry lookups), but Show is part of the package's exported surface
// and otherwise has no coverage at all.
func TestShowResolvesLikeDelete(t *testing.T) {
	fake := &execx.Fake{}
	env, repo := newTestEnv(t, fake)
	path := mkdir(t, filepath.Join(env.Cwd, "wt"))
	seed(t, env, &state.Env{
		Project: "proj", Branch: "x", TmuxSession: "proj-x", WorktreePath: path, RepoPath: repo,
		Issues: []string{"https://github.com/acme/proj/issues/59"},
	})

	item, err := env.Show(issue(59), "")
	if err != nil {
		t.Fatalf("Show error: %v", err)
	}
	if item.Project != "proj" || item.Branch != "x" || item.Session != "proj-x" {
		t.Errorf("item = %+v", item)
	}

	if _, err := env.Show(name("nope"), "proj"); err == nil {
		t.Error("expected an error for an unknown target")
	}
}

// TestShowAndDeleteRepoURLGiveHelpfulError covers the case a repository URL
// can never resolve for show/delete (there is no field on a record it
// could match): the message should say why, not just "no work
// environment for owner/repo" as if a record with that URL could exist.
func TestShowAndDeleteRepoURLGiveHelpfulError(t *testing.T) {
	fake := &execx.Fake{}
	env, _ := newTestEnv(t, fake)

	want := "pass its id, session or branch"
	if _, err := env.Show(repoURL("acme", "proj"), ""); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Show: expected an error containing %q, got %v", want, err)
	}
	if _, _, err := env.Delete(repoURL("acme", "proj"), "", DeleteOptions{}); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("Delete: expected an error containing %q, got %v", want, err)
	}
}
