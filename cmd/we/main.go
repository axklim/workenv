// we — Smart work environment.
//
// Opens a complete work environment for a task: the project repository
// (cloned normally if missing), a git worktree on the right branch, a tmux
// session running claude, and a Ghostty terminal attached to it.
// Environments are recorded in a JSON registry keyed by an integer id, so a
// GitHub issue and its linked PR resolve to the same one and branches,
// session names and worktree paths never have to be re-derived.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"workenv/internal/config"
	"workenv/internal/execx"
	"workenv/internal/state"
	"workenv/internal/target"
	"workenv/internal/we"
)

const usage = `we — work environments over an id-keyed registry

Usage:
  we open   <target> [--repo R] [--branch B] [--session S] [--wt W]
                     [--host H] [--no-terminal]
  we attach <target> [--repo R] [--host H] [--no-terminal]
  we ls     [-l] [--host H]
  we show   <target> [--host H]
  we delete <target> [--repo R] [--host H]
                     [--force] [--delete-branch] [--keep-worktree]

Aliases: ls = list; rm, down = delete

<target> is one of:
  id              7
  session name    trade-review_claude-file
  branch          review_claude-file
  issue URL       https://github.com/owner/repo/issues/59
  PR URL          https://github.com/owner/repo/pull/61
  repository URL  https://github.com/owner/repo
  plain name      feature-123 (open creates it as a branch)

A plain name and a branch are the same syntax: an existing environment on
that branch is found; otherwise open creates one there.

--repo <name|path> names the repository a plain-name/branch target belongs
to, for when you are not standing in it. A bare name is looked up in
projects_path; a value containing a separator or starting with ~ is a path
to the repository. Other target kinds carry their own repository, so it is
ignored there.

open and attach are one code path; attach never creates, so --branch,
--session and --wt are rejected there rather than silently ignored. On a
hit, open prints a note to stderr saying they were ignored.

--host <host> runs the same command over ssh (creation overrides passed
through) and, for open/attach, opens a local Ghostty attached to the
resulting session. The remote host needs we installed; its path is
remote_we.

Config (XDG): ~/.config/workenv/config.toml
  projects_path = "~/projects"   where repositories live / get cloned
  worktree_path = "{{ .repo_path }}/../{{ .repo }}.{{ .branch | sanitize }}"
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
	case "open":
		return runOpen(env, "open", rest, false)
	case "attach":
		return runOpen(env, "attach", rest, true)
	case "list", "ls":
		return runList(env, rest)
	case "show":
		return runShow(env, rest)
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

// renderOptsFromEnv decides Color and Links from the environment: both true
// only when stdout is a character device and NO_COLOR is unset.
func renderOptsFromEnv() renderOpts {
	color := false
	if fi, err := os.Stdout.Stat(); err == nil {
		color = fi.Mode()&os.ModeCharDevice != 0
	}
	if os.Getenv("NO_COLOR") != "" {
		color = false
	}
	return renderOpts{Color: color, Links: color}
}

// runOpen serves both open (find or create) and attach (find only); the two
// differ in attachOnly and in attach not defining --branch/--session/--wt —
// passing one to attach fails with Go's own "flag provided but not defined".
func runOpen(env *we.Env, cmd string, args []string, attachOnly bool) error {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	repo := fs.String("repo", "", "repository for a plain-name/branch target")
	host := fs.String("host", "", "run on a remote host")
	noTerminal := fs.Bool("no-terminal", false, "skip attaching a terminal")
	var branch, session, wt string
	if !attachOnly {
		fs.StringVar(&branch, "branch", "", "branch to check out or create (creation only)")
		fs.StringVar(&session, "session", "", "tmux session name (creation only)")
		fs.StringVar(&wt, "wt", "", "worktree location (creation only)")
	}
	raw, err := parseWithArg(fs, args)
	if err != nil {
		return err
	}
	if raw == "" {
		return fmt.Errorf("%s: missing target (id, session, branch, issue/PR/repository URL)", cmd)
	}
	tgt, err := target.Parse(raw)
	if err != nil {
		return err
	}

	if *host != "" {
		return openRemote(env, cmd, *host, raw, *repo, branch, session, wt, *noTerminal)
	}

	res, err := env.Open(we.OpenOptions{
		Target: tgt, Repo: *repo, Branch: branch, Session: session, Wt: wt,
		AttachOnly: attachOnly, NoTerminal: *noTerminal,
	})
	if err != nil {
		return err
	}
	printOpenResult(res)
	return nil
}

// printOpenResult prints the human summary, then the machine-readable
// WE_SESSION= line the remote flow parses; ignored creation overrides go to
// stderr, one line, after everything else.
func printOpenResult(res we.OpenResult) {
	verb := "found"
	if res.Created {
		verb = "created"
	}
	fmt.Printf("%s environment %d\n", verb, res.ID)
	fmt.Printf("project:  %s\n", res.Project)
	fmt.Printf("branch:   %s\n", res.Branch)
	fmt.Printf("worktree: %s\n", res.WorktreePath)
	fmt.Printf("session:  %s\n", res.Session)
	fmt.Printf("WE_SESSION=%s\n", res.Session)
	if len(res.IgnoredOverrides) > 0 {
		fmt.Fprintf(os.Stderr, "we: environment %d already exists; %s ignored\n",
			res.ID, strings.Join(res.IgnoredOverrides, ", "))
	}
}

// openRemote runs open/attach on host over ssh — --no-terminal first, then
// whichever of --repo, --branch, --session, --wt were given, in that order
// — prints what it printed, and attaches a local terminal to the resulting
// session unless noTerminal was requested locally too.
//
// It uses OutputPassStderr rather than Output: stdout must be captured so
// the WE_SESSION= marker can be parsed out of it, but the remote's stderr
// — notably the "environment N already exists; ... ignored" note on a hit
// — has to reach the user directly. Output only surfaces stderr inside the
// error it returns, so on a successful (non-error) remote call that note
// would otherwise be silently dropped.
func openRemote(env *we.Env, cmd, host, rawTarget, repo, branch, session, wt string, noTerminal bool) error {
	remote := []string{host, env.Cfg.RemoteWe, cmd, rawTarget, "--no-terminal"}
	if repo != "" {
		remote = append(remote, "--repo", repo)
	}
	if branch != "" {
		remote = append(remote, "--branch", branch)
	}
	if session != "" {
		remote = append(remote, "--session", session)
	}
	if wt != "" {
		remote = append(remote, "--wt", wt)
	}
	out, err := env.R.OutputPassStderr("", "ssh", remote...)
	if out != "" {
		fmt.Println(out)
	}
	if err != nil {
		return fmt.Errorf("remote %s on %s: %w", cmd, host, err)
	}
	sess := parseSessionMarker(out)
	if sess == "" {
		return fmt.Errorf("remote we did not report a session (is %q installed on %s?)", env.Cfg.RemoteWe, host)
	}
	if noTerminal {
		fmt.Printf("attach with: ssh -t %s tmux attach-session -t %s\n", host, sess)
		return nil
	}
	return env.AttachRemote(host, sess)
}

// parseSessionMarker extracts the WE_SESSION=<session> line printOpenResult
// prints, from the captured output of a remote open/attach.
func parseSessionMarker(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if s, ok := strings.CutPrefix(strings.TrimSpace(line), "WE_SESSION="); ok {
			return s
		}
	}
	return ""
}

func runList(env *we.Env, args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	long := fs.Bool("l", false, "print the stacked form")
	host := fs.String("host", "", "list on a remote host")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("ls: unexpected argument %q", fs.Arg(0))
	}
	if *host != "" {
		remote := []string{*host, env.Cfg.RemoteWe, "ls"}
		if *long {
			remote = append(remote, "-l")
		}
		return env.R.Run("", "ssh", remote...)
	}
	items, err := env.List()
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Println("no work environments")
		return nil
	}
	opts := renderOptsFromEnv()
	opts.Long = *long
	return renderList(os.Stdout, items, opts)
}

func runShow(env *we.Env, args []string) error {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	host := fs.String("host", "", "show on a remote host")
	raw, err := parseWithArg(fs, args)
	if err != nil {
		return err
	}
	if raw == "" {
		return errors.New("show: missing target")
	}
	if *host != "" {
		return env.R.Run("", "ssh", *host, env.Cfg.RemoteWe, "show", raw)
	}
	tgt, err := target.Parse(raw)
	if err != nil {
		return err
	}
	it, err := env.Show(tgt, "")
	if err != nil {
		return err
	}
	return renderShow(os.Stdout, it, renderOptsFromEnv())
}

func runDelete(env *we.Env, args []string) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	repo := fs.String("repo", "", "repository for a plain-name/branch target")
	host := fs.String("host", "", "delete on a remote host")
	force := fs.Bool("force", false, "remove the worktree even if it has local changes")
	deleteBranch := fs.Bool("delete-branch", false, "also delete the branch")
	keepWorktree := fs.Bool("keep-worktree", false, "kill the session only")
	raw, err := parseWithArg(fs, args)
	if err != nil {
		return err
	}
	if raw == "" {
		return errors.New("delete: missing target (id, session, branch, issue/PR/repository URL)")
	}
	if *host != "" {
		remote := []string{*host, env.Cfg.RemoteWe, "delete", raw}
		if *repo != "" {
			remote = append(remote, "--repo", *repo)
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
	id, session, err := env.Delete(tgt, *repo, we.DeleteOptions{
		Force:        *force,
		DeleteBranch: *deleteBranch,
		KeepWorktree: *keepWorktree,
	})
	if err != nil {
		return err
	}
	if id != 0 {
		fmt.Printf("deleted environment %d (session %s)\n", id, session)
	} else {
		fmt.Printf("killed stray session %s\n", session)
	}
	return nil
}
