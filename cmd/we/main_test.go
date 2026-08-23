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

// parse runs args through the real parser and returns the filled options
// and the active command's canonical name.
func parse(t *testing.T, args ...string) (options, string) {
	t.Helper()
	var opts options
	p := newParser(&opts)
	if _, err := p.ParseArgs(args); err != nil {
		t.Fatalf("ParseArgs(%q): %v", args, err)
	}
	if p.Active == nil {
		t.Fatalf("ParseArgs(%q): no active command", args)
	}
	return opts, p.Active.Name
}

// TestParseAcceptsFlagsAroundTarget covers what the hand-rolled
// parseWithArg used to do: flags are allowed before and after the
// positional target. It also proves the embedded option structs
// (hostOpt/repoOpt/targetArg inside attachCmd, attachCmd and overrides
// inside openCmd) are actually reached by go-flags' reflection — a silent
// wiring failure there would leave every flag at its zero value.
func TestParseAcceptsFlagsAroundTarget(t *testing.T) {
	opts, name := parse(t, "open", "--branch", "b", "7", "--wt", "/tmp/wt", "--repo", "trade", "--no-terminal")
	if name != "open" {
		t.Fatalf("active command = %q, want \"open\"", name)
	}
	o := opts.Open
	if o.Args.Target != "7" || o.Branch != "b" || o.Wt != "/tmp/wt" || o.Repo != "trade" || !o.NoTerminal {
		t.Errorf("open parsed as %+v", o)
	}
}

// TestAttachRejectsCreationOverrides pins the design doc's claim that the
// creation overrides are "rejected there rather than silently ignored":
// attach does not define them, so passing one is a parse error.
func TestAttachRejectsCreationOverrides(t *testing.T) {
	for _, flag := range []string{"--branch", "--session", "--wt"} {
		var opts options
		_, err := newParser(&opts).ParseArgs([]string{"attach", "7", flag, "x"})
		if err == nil {
			t.Errorf("attach accepted %s; it must be rejected", flag)
			continue
		}
		if !strings.Contains(err.Error(), strings.TrimPrefix(flag, "--")) {
			t.Errorf("error for %s does not name the flag: %v", flag, err)
		}
	}
}

// TestCommandAliasesResolveToCanonicalNames guards the dispatch switch in
// run: go-flags resolves an alias to the command it belongs to, so the
// switch must key on the canonical name. A case on "ls" or "rm" would never
// fire, and the command would fall through to "unknown command".
func TestCommandAliasesResolveToCanonicalNames(t *testing.T) {
	for _, tc := range []struct{ typed, want string }{
		{"list", "list"}, {"ls", "list"},
		{"delete", "delete"}, {"rm", "delete"}, {"down", "delete"},
	} {
		args := []string{tc.typed}
		if tc.want == "delete" {
			args = append(args, "7")
		}
		if _, name := parse(t, args...); name != tc.want {
			t.Errorf("%q resolved to command %q, want %q", tc.typed, name, tc.want)
		}
	}
}

// TestHelpListsCommandsAndAliases covers what the generated help replaced:
// the hand-written synopsis and its "Aliases: ls = list; rm, down = delete"
// line. Every command and alias a user can type has to appear in `we help`,
// or the only documentation of them is the source.
func TestHelpListsCommandsAndAliases(t *testing.T) {
	out := captureStdout(t, func() {
		if err := run([]string{"help"}); err != nil {
			t.Fatalf("run(help): %v", err)
		}
	})
	for _, want := range []string{"open", "attach", "list", "show", "delete", "version", "aliases: ls", "rm, down"} {
		if !strings.Contains(out, want) {
			t.Errorf("`we help` does not mention %q:\n%s", want, out)
		}
	}
}

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

	var cmd deleteCmd
	cmd.Args.Target = "proj-x"
	out := captureStdout(t, func() {
		if err := runDelete(env, cmd); err != nil {
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
