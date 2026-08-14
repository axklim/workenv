package we

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"workenv/internal/config"
	"workenv/internal/execx"
	"workenv/internal/target"
)

// newTestEnv builds an Env over a temp projects dir containing one project
// repo directory ("proj" with a .git marker), plus a scripted fake runner.
func newTestEnv(t *testing.T, fake *execx.Fake) (*Env, string) {
	t.Helper()
	root := t.TempDir()
	projects := filepath.Join(root, "projects")
	repo := filepath.Join(projects, "proj")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		ProjectsDir:  projects,
		WorktreesDir: filepath.Join(projects, ".we"),
		ClaudeCmd:    "claude",
		RemoteWe:     "we",
	}
	return &Env{Cfg: cfg, R: fake, GOOS: "darwin", Cwd: root}, repo
}

func hasCall(f *execx.Fake, want string) bool {
	return slices.ContainsFunc(f.Joined(), func(c string) bool {
		return strings.HasPrefix(c, want)
	})
}

func TestUpCreatesWorktreeSessionAndClaude(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		// No worktree for the branch yet.
		{Prefix: "git worktree list", Out: "worktree /projects/proj\nHEAD abc\nbranch refs/heads/main\n"},
		// Branch does not exist locally or on origin.
		{Prefix: "git show-ref", Err: errFake},
		// No origin/HEAD; repo HEAD is main.
		{Prefix: "git symbolic-ref --short refs/remotes/origin/HEAD", Err: errFake},
		{Prefix: "git symbolic-ref --short HEAD", Out: "main"},
		// No session yet.
		{Prefix: "tmux has-session", Err: errFake},
	}}
	env, repo := newTestEnv(t, fake)

	res, err := env.Up(UpOptions{Target: target.Target{Kind: target.KindName, Name: "feature-123"}, Project: "proj", NoTerminal: true})
	if err != nil {
		t.Fatalf("Up error: %v", err)
	}
	wtPath := filepath.Join(env.Cfg.WorktreesDir, "proj", "feature-123")
	if res.Project != "proj" || res.Branch != "feature-123" || res.Path != wtPath || res.Session != "we-proj-feature-123" {
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
	if repo == "" {
		t.Fatal("unreachable")
	}
}

func TestUpReusesExistingWorktreeAndSession(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "git worktree list", Out: "worktree /elsewhere/feature-123\nHEAD abc\nbranch refs/heads/feature-123\n"},
		// has-session succeeds (default fake response is success).
	}}
	env, _ := newTestEnv(t, fake)

	res, err := env.Up(UpOptions{Target: target.Target{Kind: target.KindName, Name: "feature-123"}, Project: "proj", NoTerminal: true})
	if err != nil {
		t.Fatalf("Up error: %v", err)
	}
	if res.Path != "/elsewhere/feature-123" {
		t.Errorf("Path = %q, want existing worktree path", res.Path)
	}
	for _, forbidden := range []string{"git worktree add", "tmux new-session", "tmux send-keys"} {
		if hasCall(fake, forbidden) {
			t.Errorf("unexpected call %q — existing worktree/session must be reused", forbidden)
		}
	}
}

func TestUpFromIssueDerivesBranchFromNumberAndTitle(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "gh issue view 123", Out: `{"number":123,"title":"Add Kafka publisher"}`},
		{Prefix: "git worktree list", Out: ""},
		{Prefix: "git show-ref", Err: errFake},
		{Prefix: "git symbolic-ref --short refs/remotes/origin/HEAD", Out: "origin/main"},
		{Prefix: "tmux has-session", Err: errFake},
	}}
	env, _ := newTestEnv(t, fake)

	tgt := target.Target{Kind: target.KindIssue, Owner: "acme", Repo: "proj", Number: 123}
	res, err := env.Up(UpOptions{Target: tgt, NoTerminal: true})
	if err != nil {
		t.Fatalf("Up error: %v", err)
	}
	if res.Branch != "issue-123-add-kafka-publisher" {
		t.Errorf("Branch = %q", res.Branch)
	}
	if res.Session != "we-proj-issue-123-add-kafka-publisher" {
		t.Errorf("Session = %q", res.Session)
	}
	if !hasCall(fake, "git worktree add -b issue-123-add-kafka-publisher") {
		t.Errorf("missing worktree add:\n%s", strings.Join(fake.Joined(), "\n"))
	}
}

func TestUpFromPRUsesHeadBranchWhenOnOrigin(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "gh pr view 456", Out: `{"number":456,"title":"Fix crash","headRefName":"fix/crash"}`},
		{Prefix: "git worktree list", Out: ""},
		{Prefix: "git show-ref --verify --quiet refs/heads/fix/crash", Err: errFake},
		// origin has the head branch (same-repo PR).
		{Prefix: "git show-ref --verify --quiet refs/remotes/origin/fix/crash", Out: ""},
		{Prefix: "tmux has-session", Err: errFake},
	}}
	env, _ := newTestEnv(t, fake)

	tgt := target.Target{Kind: target.KindPR, Owner: "acme", Repo: "proj", Number: 456}
	res, err := env.Up(UpOptions{Target: tgt, NoTerminal: true})
	if err != nil {
		t.Fatalf("Up error: %v", err)
	}
	if res.Branch != "fix/crash" || res.Name != "pr-456" {
		t.Errorf("res = %+v", res)
	}
	if res.Session != "we-proj-pr-456" {
		t.Errorf("Session = %q", res.Session)
	}
	if !hasCall(fake, "git worktree add --track -b fix/crash") {
		t.Errorf("missing tracking worktree add:\n%s", strings.Join(fake.Joined(), "\n"))
	}
}

func TestUpOpensTerminalWhenNoClientAttached(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "git worktree list", Out: "worktree /elsewhere/feature-123\nHEAD abc\nbranch refs/heads/feature-123\n"},
		{Prefix: "tmux list-clients", Err: errFake},
	}}
	env, _ := newTestEnv(t, fake)

	_, err := env.Up(UpOptions{Target: target.Target{Kind: target.KindName, Name: "feature-123"}, Project: "proj"})
	if err != nil {
		t.Fatalf("Up error: %v", err)
	}
	if !hasCall(fake, "open -na Ghostty --args -e tmux attach-session -t we-proj-feature-123") {
		t.Errorf("missing ghostty open:\n%s", strings.Join(fake.Joined(), "\n"))
	}
}

func TestUpFocusesTerminalWhenClientAttached(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "git worktree list", Out: "worktree /elsewhere/feature-123\nHEAD abc\nbranch refs/heads/feature-123\n"},
		{Prefix: "tmux list-clients", Out: "/dev/ttys001: 0 [204x59 xterm-256color]"},
	}}
	env, _ := newTestEnv(t, fake)

	_, err := env.Up(UpOptions{Target: target.Target{Kind: target.KindName, Name: "feature-123"}, Project: "proj"})
	if err != nil {
		t.Fatalf("Up error: %v", err)
	}
	if !hasCall(fake, "open -a Ghostty") {
		t.Errorf("expected focus (open -a Ghostty):\n%s", strings.Join(fake.Joined(), "\n"))
	}
	if hasCall(fake, "open -na Ghostty") {
		t.Error("must not open a new window when a client is already attached")
	}
}

func TestDownKillsSessionRemovesWorktree(t *testing.T) {
	fake := &execx.Fake{}
	env, _ := newTestEnv(t, fake)
	wtPath := filepath.Join(env.Cfg.WorktreesDir, "proj", "feature-123")
	fake.Responses = []execx.FakeResponse{
		{Prefix: "git worktree list", Out: "worktree " + wtPath + "\nHEAD abc\nbranch refs/heads/feature-123\n"},
	}

	if err := env.Down("feature-123", "proj", DownOptions{DeleteBranch: true}); err != nil {
		t.Fatalf("Down error: %v", err)
	}
	for _, want := range []string{
		"tmux kill-session -t =we-proj-feature-123",
		"git worktree remove " + wtPath,
		"git worktree prune",
		"git branch -D feature-123",
	} {
		if !hasCall(fake, want) {
			t.Errorf("missing call %q in:\n%s", want, strings.Join(fake.Joined(), "\n"))
		}
	}
}

func TestListMergesSessionsAndWorktrees(t *testing.T) {
	fake := &execx.Fake{}
	env, _ := newTestEnv(t, fake)
	// A worktree on disk without a session.
	orphan := filepath.Join(env.Cfg.WorktreesDir, "proj", "feature-999")
	if err := os.MkdirAll(orphan, 0o755); err != nil {
		t.Fatal(err)
	}
	fake.Responses = []execx.FakeResponse{
		{Prefix: "tmux list-sessions", Out: "we-proj-feature-123\tproj\tfeature-123\t/p/wt\t1\n"},
		{Prefix: "git rev-parse --abbrev-ref HEAD", Out: "feature-999"},
	}

	items, err := env.List()
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %+v", len(items), items)
	}
	byName := map[string]Item{}
	for _, it := range items {
		byName[it.Name] = it
	}
	if it := byName["feature-123"]; it.Project != "proj" || it.SessionState != "attached" {
		t.Errorf("feature-123 = %+v", it)
	}
	if it := byName["feature-999"]; it.SessionState != "none" || it.Branch != "feature-999" {
		t.Errorf("feature-999 = %+v", it)
	}
}
