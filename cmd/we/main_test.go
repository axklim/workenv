package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workenv/internal/config"
	"workenv/internal/execx"
	"workenv/internal/state"
	"workenv/internal/we"
)

// TestOpenRemoteUsesOutputPassStderr guards the fix for the remote path
// silently dropping a hit's "ignored" note: Output only surfaces stderr
// inside the error it returns, so on a successful remote call (a hit) the
// note never reached the user. openRemote must call OutputPassStderr, not
// Output, so the remote's stderr streams straight through instead.
func TestOpenRemoteUsesOutputPassStderr(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "ssh devbox we open 7 --no-terminal", Out: "found environment 7\nWE_SESSION=trade-review\n"},
	}}
	env := &we.Env{Cfg: config.Config{RemoteWe: "we"}, R: fake}

	// noTerminal:true keeps this to the one ssh call — no local AttachRemote.
	if err := openRemote(env, "open", "devbox", "7", "", "", "", "", true); err != nil {
		t.Fatalf("openRemote: %v", err)
	}

	if len(fake.Calls) != 1 {
		t.Fatalf("expected 1 recorded call, got %d: %+v", len(fake.Calls), fake.Calls)
	}
	call := fake.Calls[0]
	if call.Method != "OutputPassStderr" {
		t.Errorf("openRemote must call OutputPassStderr (so the remote's stderr streams through on a hit), got Method %q", call.Method)
	}
	wantArgv := "ssh devbox we open 7 --no-terminal"
	if got := strings.Join(call.Argv, " "); got != wantArgv {
		t.Errorf("argv = %q, want %q", got, wantArgv)
	}
}

// TestOpenRemotePassesThroughOverridesInOrder checks the documented
// pass-through order survives the switch to OutputPassStderr: --no-terminal
// first, then whichever of --repo, --branch, --session, --wt were given.
func TestOpenRemotePassesThroughOverridesInOrder(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "ssh devbox we open feature-1", Out: "created environment 9\nWE_SESSION=trade-feature-1\n"},
	}}
	env := &we.Env{Cfg: config.Config{RemoteWe: "we"}, R: fake}

	err := openRemote(env, "open", "devbox", "feature-1", "trade", "feature-1", "sess", "~/wt", true)
	if err != nil {
		t.Fatalf("openRemote: %v", err)
	}
	want := "ssh devbox we open feature-1 --no-terminal --repo trade --branch feature-1 --session sess --wt ~/wt"
	if got := strings.Join(fake.Calls[0].Argv, " "); got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

// TestRunDeletePrintsResolvedID guards the fix for delete echoing the raw
// target back verbatim: the printed line must name the environment's
// resolved id (and session), not whatever ambiguous string — id, session,
// branch, or issue/PR URL — the user happened to type as the target.
func TestRunDeletePrintsResolvedID(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "envs.json")
	st := &state.Store{Path: statePath}
	// WorktreePath == RepoPath marks this the repository's main working
	// tree, which Delete skips removing — keeping the fake script to just
	// the tmux calls this test actually cares about.
	st.Add(&state.Env{Project: "proj", Branch: "x", TmuxSession: "proj-x", WorktreePath: dir, RepoPath: dir})
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "tmux show-options -t proj-x @workenv", Out: "@workenv 1"},
	}}
	env := &we.Env{Cfg: config.Config{}, R: fake, StatePath: statePath, Cwd: dir}

	out := captureStdout(t, func() {
		if err := runDelete(env, []string{"proj-x"}); err != nil {
			t.Fatalf("runDelete: %v", err)
		}
	})
	want := "deleted environment 1 (session proj-x)\n"
	if out != want {
		t.Errorf("output = %q, want %q", out, want)
	}
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	fn()
	os.Stdout = orig
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(data)
}
