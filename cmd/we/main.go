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
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/jessevdk/go-flags"

	"workenv/internal/config"
	"workenv/internal/execx"
	"workenv/internal/state"
	"workenv/internal/target"
	"workenv/internal/we"
)

// options is the whole command surface. go-flags renders `we --help` and
// every `we <command> --help` from these tags, so the synopsis, the command
// list and the aliases have no hand-written copy to drift from.
type options struct {
	Open    openCmd    `command:"open" description:"find or create a work environment and show it"`
	Attach  attachCmd  `command:"attach" description:"find an existing work environment and show it"`
	List    listCmd    `command:"list" alias:"ls" description:"list work environments"`
	Show    showCmd    `command:"show" description:"print one environment in the stacked form"`
	Delete  deleteCmd  `command:"delete" alias:"rm" alias:"down" description:"kill the session, remove the worktree, drop the record"`
	Version versionCmd `command:"version" description:"print the version"`
}

// hostOpt is --host, carried by every command: run this same command on a
// remote machine over ssh.
type hostOpt struct {
	Host string `long:"host" value-name:"HOST" description:"run on a remote host over ssh"`
}

// repoOpt is --repo, carried by the commands that take a plain-name target.
type repoOpt struct {
	Repo string `long:"repo" value-name:"NAME|PATH" description:"repository a plain-name/branch target belongs to"`
}

// targetArg is the positional <target> — an id, session, branch or GitHub
// URL. Required, so a bare `we open` is go-flags' error rather than a
// hand-written one.
type targetArg struct {
	Args struct {
		Target string `positional-arg-name:"target" description:"id, session, branch, or issue/PR/repository URL"`
	} `positional-args:"yes" required:"yes"`
}

// attachCmd is also open's shape minus the creation overrides: attach never
// creates, so --branch, --session and --wt are not defined for it and
// passing one is an unknown-flag error rather than a silent no-op.
type attachCmd struct {
	hostOpt
	repoOpt
	targetArg
	NoTerminal bool `long:"no-terminal" description:"do not open or switch a terminal"`
}

type openCmd struct {
	attachCmd
	overrides
}

// overrides are the creation-only flags: they decide what a new environment
// looks like and are reported as ignored when the target resolves to one
// that already exists.
type overrides struct {
	Branch  string `long:"branch" value-name:"NAME" description:"branch to check out or create (creation only)"`
	Session string `long:"session" value-name:"NAME" description:"tmux session name (creation only)"`
	Wt      string `long:"wt" value-name:"PATH|NAME" description:"worktree location (creation only)"`
}

type listCmd struct {
	hostOpt
	Long bool `short:"l" long:"long" description:"print the stacked form"`
}

type showCmd struct {
	hostOpt
	targetArg
}

type deleteCmd struct {
	hostOpt
	repoOpt
	targetArg
	Force        bool `long:"force" description:"remove the worktree even if it has local changes"`
	DeleteBranch bool `long:"delete-branch" description:"also delete the branch"`
	KeepWorktree bool `long:"keep-worktree" description:"kill the session only"`
}

type versionCmd struct{}

// guide is everything the generated help cannot express: what a target may
// be, how the flags that need a paragraph behave, and where the config and
// state files live. It is printed after go-flags' own help for `we help`
// and `we --help`.
const guide = `
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

Examples:
  we open https://github.com/owner/repo/issues/59
  we open feature-123 --repo trade
  we attach 7
  we ls -l
  we delete 7 --delete-branch

Config (XDG): ~/.config/workenv/config.toml
  projects_path = "~/projects"   where repositories live / get cloned
  worktree_path = "{{ .repo_path }}/../{{ .repo }}.{{ .branch | sanitize }}"
  claude_cmd    = "claude"       command run in the first tmux window,
                                 with --name <session> appended
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
	// Answered before the parser and before config.Load() on purpose: a
	// machine with an unparsable config must still be able to say which
	// version it has, and `brew test` runs the binary in a sandbox with no
	// config at all. The version command below is the same answer for
	// anyone who gets that far.
	if len(args) > 0 && (args[0] == "version" || args[0] == "--version") {
		fmt.Println(version)
		return nil
	}

	var opts options
	p := newParser(&opts)

	if len(args) == 0 || args[0] == "help" {
		writeHelp(p, os.Stdout)
		if len(args) == 0 {
			return errors.New("missing command")
		}
		return nil
	}

	rest, err := p.ParseArgs(args)
	if err != nil {
		var ferr *flags.Error
		if errors.As(err, &ferr) && ferr.Type == flags.ErrHelp {
			// -h/--help: go-flags renders the help for whichever command
			// it was asked on into the message. The guide only belongs
			// under the top-level one.
			fmt.Print(ferr.Message)
			if p.Active == nil {
				fmt.Print(guide)
			}
			return nil
		}
		return err
	}
	if len(rest) > 0 {
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	if p.Active == nil {
		return errors.New("missing command")
	}
	if p.Active.Name == "version" {
		fmt.Println(version)
		return nil
	}

	env, err := newEnv()
	if err != nil {
		return err
	}
	switch p.Active.Name {
	case "open":
		return runOpen(env, "open", opts.Open.attachCmd, opts.Open.overrides, false)
	case "attach":
		return runOpen(env, "attach", opts.Attach, overrides{}, true)
	case "list":
		return runList(env, opts.List)
	case "show":
		return runShow(env, opts.Show)
	case "delete":
		return runDelete(env, opts.Delete)
	}
	return fmt.Errorf("unknown command %q", p.Active.Name)
}

// newParser builds the parser over opts. Tests parse through it too, so
// what they assert is the same surface `we` itself presents.
func newParser(opts *options) *flags.Parser {
	p := flags.NewParser(opts, flags.HelpFlag|flags.PassDoubleDash)
	p.Name = "we"
	p.ShortDescription = "work environments over an id-keyed registry"
	return p
}

// writeHelp prints the generated help — synopsis, commands, aliases, flags
// — followed by the prose guide.
func writeHelp(p *flags.Parser, w io.Writer) {
	p.WriteHelp(w)
	fmt.Fprint(w, guide)
}

func newEnv() (*we.Env, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return &we.Env{
		Cfg:        cfg,
		R:          execx.Real{},
		GOOS:       runtime.GOOS,
		Cwd:        cwd,
		InsideTmux: os.Getenv("TMUX") != "",
		StatePath:  state.DefaultPath(),
	}, nil
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
// differ in attachOnly and in attach having no creation overrides to pass.
func runOpen(env *we.Env, cmd string, c attachCmd, ov overrides, attachOnly bool) error {
	raw := c.Args.Target
	tgt, err := target.Parse(raw)
	if err != nil {
		return err
	}

	if c.Host != "" {
		return openRemote(env, cmd, c.Host, raw, c.Repo, ov.Branch, ov.Session, ov.Wt, c.NoTerminal)
	}

	res, err := env.Open(we.OpenOptions{
		Target: tgt, Repo: c.Repo, Branch: ov.Branch, Session: ov.Session, Wt: ov.Wt,
		AttachOnly: attachOnly, NoTerminal: c.NoTerminal,
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

func runList(env *we.Env, c listCmd) error {
	if c.Host != "" {
		remote := []string{c.Host, env.Cfg.RemoteWe, "ls"}
		if c.Long {
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
	opts.Long = c.Long
	return renderList(os.Stdout, items, opts)
}

func runShow(env *we.Env, c showCmd) error {
	raw := c.Args.Target
	if c.Host != "" {
		return env.R.Run("", "ssh", c.Host, env.Cfg.RemoteWe, "show", raw)
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

func runDelete(env *we.Env, c deleteCmd) error {
	raw := c.Args.Target
	if c.Host != "" {
		remote := []string{c.Host, env.Cfg.RemoteWe, "delete", raw}
		if c.Repo != "" {
			remote = append(remote, "--repo", c.Repo)
		}
		if c.Force {
			remote = append(remote, "--force")
		}
		if c.DeleteBranch {
			remote = append(remote, "--delete-branch")
		}
		if c.KeepWorktree {
			remote = append(remote, "--keep-worktree")
		}
		return env.R.Run("", "ssh", remote...)
	}
	tgt, err := target.Parse(raw)
	if err != nil {
		return err
	}
	id, session, err := env.Delete(tgt, c.Repo, we.DeleteOptions{
		Force:        c.Force,
		DeleteBranch: c.DeleteBranch,
		KeepWorktree: c.KeepWorktree,
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
