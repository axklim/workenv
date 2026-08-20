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

// TestRunPathPrintsBarePath pins the whole contract `cd "$(we path 7)"`
// rests on: stdout is the absolute worktree path and nothing else — no
// label, no ~ abbreviation (the shell does not expand a quoted one), one
// trailing newline — and stderr is silent when the directory is there.
func TestRunPathPrintsBarePath(t *testing.T) {
	dir := t.TempDir()
	wt := filepath.Join(dir, "proj.x")
	if err := os.MkdirAll(wt, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	env := seedOneEnv(t, dir, wt)

	out, errOut := captureOutput(t, func() {
		if err := runPath(env, []string{"proj-x"}); err != nil {
			t.Fatalf("runPath: %v", err)
		}
	})
	if out != wt+"\n" {
		t.Errorf("stdout = %q, want %q", out, wt+"\n")
	}
	if errOut != "" {
		t.Errorf("stderr = %q, want it empty", errOut)
	}
	// A location is a registry read: no git, no tmux, nothing spawned.
	if fake, ok := env.R.(*execx.Fake); ok && len(fake.Calls) != 0 {
		t.Errorf("path ran commands: %v", fake.Joined())
	}
}

// TestRunPathMissingWorktreeKeepsStdoutClean covers the case `we ls` marks
// "(missing)": the recorded location is still the answer, so it is still
// the only thing on stdout, and the warning about the directory being gone
// goes to stderr where a command substitution will not pick it up.
func TestRunPathMissingWorktreeKeepsStdoutClean(t *testing.T) {
	dir := t.TempDir()
	wt := filepath.Join(dir, "proj.gone")
	env := seedOneEnv(t, dir, wt)

	out, errOut := captureOutput(t, func() {
		if err := runPath(env, []string{"proj-x"}); err != nil {
			t.Fatalf("runPath: %v", err)
		}
	})
	if out != wt+"\n" {
		t.Errorf("stdout = %q, want %q", out, wt+"\n")
	}
	if !strings.Contains(errOut, "does not exist") {
		t.Errorf("stderr = %q, want it to report the missing directory", errOut)
	}
}

// TestRunPathRemoteStreamsSSHOutput checks --host asks the host that owns
// the environment, passing --repo through, and streams the answer rather
// than capturing it: the remote path is the payload, so Run's stdout is
// already exactly what a local lookup would have printed.
func TestRunPathRemoteStreamsSSHOutput(t *testing.T) {
	fake := &execx.Fake{}
	env := &we.Env{Cfg: config.Config{RemoteWe: "we"}, R: fake}

	if err := runPath(env, []string{"7", "--host", "devbox", "--repo", "trade"}); err != nil {
		t.Fatalf("runPath: %v", err)
	}
	if len(fake.Calls) != 1 {
		t.Fatalf("expected 1 recorded call, got %d: %+v", len(fake.Calls), fake.Calls)
	}
	call := fake.Calls[0]
	if call.Method != "Run" {
		t.Errorf("Method = %q, want %q (the remote stdout is the output)", call.Method, "Run")
	}
	want := "ssh devbox we path 7 --repo trade"
	if got := strings.Join(call.Argv, " "); got != want {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

// seedOneEnv writes a registry under dir holding a single environment
// ("proj-x", worktree wt) and returns an Env reading it over a fake runner,
// so anything the lookup did spawn is recorded rather than run.
func seedOneEnv(t *testing.T, dir, wt string) *we.Env {
	t.Helper()
	statePath := filepath.Join(dir, "envs.json")
	st := &state.Store{Path: statePath}
	st.Add(&state.Env{Project: "proj", Branch: "x", TmuxSession: "proj-x", WorktreePath: wt, RepoPath: dir})
	if err := st.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return &we.Env{Cfg: config.Config{}, R: &execx.Fake{}, StatePath: statePath, Cwd: dir}
}

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	out, _ := captureOutput(t, fn)
	return out
}

// captureOutput redirects both streams for the duration of fn and returns
// what each received, for the commands that deliberately split their
// output across the two.
func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	outR, outW := pipe(t)
	errR, errW := pipe(t)
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	fn()
	os.Stdout, os.Stderr = origOut, origErr
	return drain(t, outW, outR), drain(t, errW, errR)
}

func pipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe: %v", err)
	}
	return r, w
}

// drain closes the write end — nothing more is coming — and reads what is
// left in the pipe. Both captured streams here are a handful of lines,
// well under the pipe buffer, so reading after the fact cannot deadlock.
func drain(t *testing.T, w, r *os.File) string {
	t.Helper()
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(data)
}

// TestVersionCommandPrintsBareVersion pins the output shape the Homebrew
// formula's test block depends on: the version alone, with no "we " prefix,
// so the formula can assert equality rather than match a pattern.
func TestVersionCommandPrintsBareVersion(t *testing.T) {
	orig := version
	version = "9.9.9"
	t.Cleanup(func() { version = orig })

	for _, arg := range []string{"version", "--version"} {
		out := captureStdout(t, func() {
			if err := run([]string{arg}); err != nil {
				t.Fatalf("run(%q): %v", arg, err)
			}
		})
		if out != "9.9.9\n" {
			t.Errorf("run(%q) printed %q, want %q", arg, out, "9.9.9\n")
		}
	}
}

// TestVersionIgnoresBrokenConfig proves the version is answered before
// config.Load(). A binary that cannot report its version on a machine with a
// bad config is useless to `brew test`, which runs against a pristine
// sandbox and to a user trying to work out what they have installed. It
// asserts the actual stamped value, not just that something was printed, and
// covers both `version` and `--version` — either could regress independently
// of the other.
func TestVersionIgnoresBrokenConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))

	cfgDir := filepath.Join(dir, "config", "workenv")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	broken := []byte("this line has no equals sign\n")
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), broken, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Sanity check: the config really is broken enough to stop a normal
	// command. Without this, the assertion below could pass vacuously.
	if err := run([]string{"ls"}); err == nil {
		t.Fatal("expected `we ls` to fail on an unparsable config")
	}

	orig := version
	version = "8.8.8"
	t.Cleanup(func() { version = orig })

	for _, arg := range []string{"version", "--version"} {
		out := captureStdout(t, func() {
			if err := run([]string{arg}); err != nil {
				t.Fatalf("run(%q): %v", arg, err)
			}
		})
		if out != "8.8.8\n" {
			t.Errorf("run(%q) printed %q, want %q", arg, out, "8.8.8\n")
		}
	}
}

// TestHelpOutputContainsWeOpen pins a string the Go side has no other reason
// to keep stable: the Homebrew formula's test block asserts
// assert_match "we open", shell_output("#{bin}/we help"), and nothing in Go
// or CI otherwise checks that substring. Without this test, a usage reword
// would surface only as a red `brew test` after the formula was published.
func TestHelpOutputContainsWeOpen(t *testing.T) {
	out := captureStdout(t, func() {
		if err := run([]string{"help"}); err != nil {
			t.Fatalf("run(help): %v", err)
		}
	})
	if !strings.Contains(out, "we open") {
		t.Errorf("`we help` output does not contain %q:\n%s", "we open", out)
	}
}
