package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"workenv/internal/config"
	"workenv/internal/execx"
	"workenv/internal/state"
	"workenv/internal/we"
)

// newUIEnv builds an Env over a seeded temp registry with two environments
// whose worktree directories exist, and a fake scripted for the repair an
// open runs: a checked-out branch and live tagged tmux sessions (the fake's
// default success makes has-session report every session live, so
// show-options must say it is ours).
func newUIEnv(t *testing.T, extra ...execx.FakeResponse) (*we.Env, *execx.Fake) {
	t.Helper()
	dir := t.TempDir()
	statePath := filepath.Join(dir, "envs.json")
	st := &state.Store{Path: statePath}
	for _, b := range []string{"a", "b"} {
		wt := filepath.Join(dir, "proj."+b)
		if err := os.MkdirAll(wt, 0o755); err != nil {
			t.Fatal(err)
		}
		st.Add(&state.Env{Project: "proj", Branch: b, TmuxSession: "proj-" + b, WorktreePath: wt, RepoPath: dir})
	}
	if err := st.Save(); err != nil {
		t.Fatal(err)
	}
	fake := &execx.Fake{Responses: append(extra,
		execx.FakeResponse{Prefix: "git symbolic-ref", Out: "a"},
		execx.FakeResponse{Prefix: "tmux show-options", Out: "@workenv 1"},
	)}
	cfg := config.Config{ClaudeCmd: "claude", RemoteWe: "we", ZedCmd: "zed"}
	return &we.Env{Cfg: cfg, R: fake, GOOS: "linux", Cwd: dir, StatePath: statePath}, fake
}

func callsContain(f *execx.Fake, prefix string) bool {
	return slices.ContainsFunc(f.Joined(), func(c string) bool {
		return strings.HasPrefix(c, prefix)
	})
}

func TestUIQuitEntersAndRestoresRawMode(t *testing.T) {
	env, fake := newUIEnv(t)
	var out bytes.Buffer

	if err := runUI(env, strings.NewReader("q"), &out, ""); err != nil {
		t.Fatalf("runUI: %v", err)
	}

	joined := fake.Joined()
	var stty []string
	for _, c := range joined {
		if strings.HasPrefix(c, "stty") {
			stty = append(stty, c)
		}
	}
	// stty -g (unscripted, so nothing saved), raw -echo, size, then the
	// sane fallback on the way out.
	if len(stty) < 3 || stty[0] != "stty -g" || stty[1] != "stty raw -echo" || stty[len(stty)-1] != "stty sane" {
		t.Errorf("stty calls = %v", stty)
	}
	s := out.String()
	if !strings.Contains(s, "\x1b[?1049h") || !strings.Contains(s, "\x1b[?1049l") {
		t.Error("output does not enter and leave the alternate screen")
	}
	if !strings.Contains(s, "\x1b[?25h") {
		t.Error("output does not show the cursor again")
	}
}

func TestUIListsEnvironments(t *testing.T) {
	env, _ := newUIEnv(t)
	var out bytes.Buffer

	if err := runUI(env, strings.NewReader("q"), &out, ""); err != nil {
		t.Fatalf("runUI: %v", err)
	}
	s := out.String()
	for _, want := range []string{"PROJECT", "SESSION", "proj-a", "proj-b"} {
		if !strings.Contains(s, want) {
			t.Errorf("frame does not contain %q:\n%s", want, s)
		}
	}
}

func TestUIEnterOpensSelected(t *testing.T) {
	env, fake := newUIEnv(t)
	var out bytes.Buffer

	stdout := captureStdout(t, func() {
		if err := runUI(env, strings.NewReader("\r"), &out, ""); err != nil {
			t.Fatalf("runUI: %v", err)
		}
	})
	if !strings.Contains(stdout, "found environment 1") {
		t.Errorf("stdout = %q, want the open summary for environment 1", stdout)
	}
	if !callsContain(fake, "ghostty -e tmux attach-session -t proj-a") {
		t.Errorf("no terminal attached to proj-a; calls = %v", fake.Joined())
	}
}

func TestUINavigationChangesWhatEnterOpens(t *testing.T) {
	env, fake := newUIEnv(t)
	var out bytes.Buffer

	stdout := captureStdout(t, func() {
		if err := runUI(env, strings.NewReader("j\r"), &out, ""); err != nil {
			t.Fatalf("runUI: %v", err)
		}
	})
	if !strings.Contains(stdout, "found environment 2") {
		t.Errorf("stdout = %q, want the open summary for environment 2", stdout)
	}
	if !callsContain(fake, "ghostty -e tmux attach-session -t proj-b") {
		t.Errorf("no terminal attached to proj-b; calls = %v", fake.Joined())
	}
}

// TestUIZedRepairsAndLaunches: z runs the open flow with no terminal (so a
// broken environment is repaired exactly like `we open` would) and then
// launches zed on the worktree — no Ghostty.
func TestUIZedRepairsAndLaunches(t *testing.T) {
	env, fake := newUIEnv(t)
	var out bytes.Buffer

	captureStdout(t, func() {
		if err := runUI(env, strings.NewReader("z"), &out, ""); err != nil {
			t.Fatalf("runUI: %v", err)
		}
	})
	wt := filepath.Join(env.Cwd, "proj.a")
	if !callsContain(fake, "zed "+wt) {
		t.Errorf("zed not launched on %s; calls = %v", wt, fake.Joined())
	}
	if !callsContain(fake, "tmux has-session -t =proj-a") {
		t.Errorf("open's repair did not run; calls = %v", fake.Joined())
	}
	if callsContain(fake, "ghostty") {
		t.Errorf("z must not attach a terminal; calls = %v", fake.Joined())
	}
}

const remoteItemJSON = `[{"id":3,"project":"proj","branch":"x","session":"proj-x",` +
	`"session_state":"detached","worktree_path":"/home/u/proj.x","repo_path":"/home/u/proj",` +
	`"issues":[],"prs":[],"exists":true,"current":false,"created_at":"2026-08-21T00:00:00Z"}]`

func TestUIRemoteListsOverSSH(t *testing.T) {
	env, fake := newUIEnv(t,
		execx.FakeResponse{Prefix: "ssh devbox we ls --json", Out: remoteItemJSON},
	)
	var out bytes.Buffer

	if err := runUI(env, strings.NewReader("q"), &out, "devbox"); err != nil {
		t.Fatalf("runUI: %v", err)
	}
	if !callsContain(fake, "ssh devbox we ls --json") {
		t.Errorf("remote list not fetched; calls = %v", fake.Joined())
	}
	s := out.String()
	if !strings.Contains(s, "proj-x") || !strings.Contains(s, "devbox") {
		t.Errorf("frame does not show the remote environment and host:\n%s", s)
	}
	// The local registry must not leak into a remote view.
	if strings.Contains(s, "proj-a") {
		t.Errorf("frame shows local environments in a remote view:\n%s", s)
	}
}

func TestUIRemoteEnterOpensOverSSH(t *testing.T) {
	env, fake := newUIEnv(t,
		execx.FakeResponse{Prefix: "ssh devbox we ls --json", Out: remoteItemJSON},
		execx.FakeResponse{Prefix: "ssh devbox we open 3 --no-terminal", Out: "found environment 3\nWE_SESSION=proj-x"},
	)
	var out bytes.Buffer

	captureStdout(t, func() {
		if err := runUI(env, strings.NewReader("\r"), &out, "devbox"); err != nil {
			t.Fatalf("runUI: %v", err)
		}
	})
	if !callsContain(fake, "ssh devbox we open 3 --no-terminal") {
		t.Errorf("remote open not run; calls = %v", fake.Joined())
	}
	if !callsContain(fake, "ghostty -e ssh -t devbox tmux attach-session -t proj-x") {
		t.Errorf("no local terminal attached over ssh; calls = %v", fake.Joined())
	}
}

func TestUIRemoteZedUsesSSHURL(t *testing.T) {
	env, fake := newUIEnv(t,
		execx.FakeResponse{Prefix: "ssh devbox we ls --json", Out: remoteItemJSON},
		execx.FakeResponse{Prefix: "ssh devbox we open 3 --no-terminal", Out: "found environment 3\nWE_SESSION=proj-x"},
	)
	var out bytes.Buffer

	captureStdout(t, func() {
		if err := runUI(env, strings.NewReader("z"), &out, "devbox"); err != nil {
			t.Fatalf("runUI: %v", err)
		}
	})
	if !callsContain(fake, "ssh devbox we open 3 --no-terminal") {
		t.Errorf("remote repair not run before zed; calls = %v", fake.Joined())
	}
	if !callsContain(fake, "zed ssh://devbox/home/u/proj.x") {
		t.Errorf("zed not launched over ssh; calls = %v", fake.Joined())
	}
}

// TestUICreatePromptPassesTargetAndRepoThrough drives `n` on a remote host,
// where the whole create is one observable ssh call.
func TestUICreatePromptPassesTargetAndRepoThrough(t *testing.T) {
	env, fake := newUIEnv(t,
		execx.FakeResponse{Prefix: "ssh devbox we ls --json", Out: "[]"},
		execx.FakeResponse{Prefix: "ssh devbox we open feature-x", Out: "created environment 4\nWE_SESSION=proj-feature-x"},
	)
	var out bytes.Buffer

	captureStdout(t, func() {
		if err := runUI(env, strings.NewReader("nfeature-x\rproj\r"), &out, "devbox"); err != nil {
			t.Fatalf("runUI: %v", err)
		}
	})
	if !callsContain(fake, "ssh devbox we open feature-x --no-terminal --repo proj") {
		t.Errorf("create not passed through; calls = %v", fake.Joined())
	}
	if !callsContain(fake, "ghostty -e ssh -t devbox tmux attach-session -t proj-feature-x") {
		t.Errorf("no local terminal attached to the created session; calls = %v", fake.Joined())
	}
}

func TestUIHostSwitchLoadsRemoteList(t *testing.T) {
	env, fake := newUIEnv(t,
		execx.FakeResponse{Prefix: "ssh devbox we ls --json", Out: remoteItemJSON},
	)
	var out bytes.Buffer

	if err := runUI(env, strings.NewReader("hdevbox\rq"), &out, ""); err != nil {
		t.Fatalf("runUI: %v", err)
	}
	if !callsContain(fake, "ssh devbox we ls --json") {
		t.Errorf("host switch did not fetch the remote list; calls = %v", fake.Joined())
	}
	if !strings.Contains(out.String(), "proj-x") {
		t.Errorf("frame does not show the remote environment:\n%s", out.String())
	}
}

// TestUICancelledPromptActsOnNothing: Ctrl-C inside the new-target prompt
// drops back to the list instead of creating anything or quitting into an
// action.
func TestUICancelledPromptActsOnNothing(t *testing.T) {
	env, fake := newUIEnv(t)
	var out bytes.Buffer

	if err := runUI(env, strings.NewReader("n\x03q"), &out, ""); err != nil {
		t.Fatalf("runUI: %v", err)
	}
	for _, banned := range []string{"ssh", "zed", "ghostty", "git worktree add"} {
		if callsContain(fake, banned) {
			t.Errorf("cancelled prompt still ran %q; calls = %v", banned, fake.Joined())
		}
	}
}

// TestUIEmptyRegistryIgnoresActions: enter and z on an empty list do
// nothing, and the frame says the list is empty.
func TestUIEmptyRegistryIgnoresActions(t *testing.T) {
	dir := t.TempDir()
	env := &we.Env{
		Cfg: config.Config{ClaudeCmd: "claude", RemoteWe: "we", ZedCmd: "zed"},
		R:   &execx.Fake{}, GOOS: "linux", Cwd: dir,
		StatePath: filepath.Join(dir, "envs.json"),
	}
	fake := env.R.(*execx.Fake)
	var out bytes.Buffer

	if err := runUI(env, strings.NewReader("\rzq"), &out, ""); err != nil {
		t.Fatalf("runUI: %v", err)
	}
	if !strings.Contains(out.String(), "no work environments") {
		t.Errorf("frame does not say the registry is empty:\n%s", out.String())
	}
	for _, banned := range []string{"zed", "ghostty"} {
		if callsContain(fake, banned) {
			t.Errorf("action on an empty list ran %q; calls = %v", banned, fake.Joined())
		}
	}
}
