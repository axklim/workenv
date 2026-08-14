// we — Smart work environment.
//
// Tears up a complete work environment for a task: project repository
// (cloned bare if missing), git worktree, tmux session running claude, and
// a Ghostty terminal attached to it. Stateless by design: everything is
// recoverable from branch names, session names, and the directory layout.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"
	"text/tabwriter"

	"workenv/internal/config"
	"workenv/internal/execx"
	"workenv/internal/target"
	"workenv/internal/we"
)

const usage = `we — Smart work environment

Usage:
  we create <target> [flags]   tear up a work environment
  we list                      list work environments
  we delete <name> [flags]     tear down a work environment

Targets for create:
  a GitHub issue URL   https://github.com/owner/repo/issues/123
  a GitHub PR URL      https://github.com/owner/repo/pull/456
  a plain name         feature-123 (current repo, or --project)

Flags for create:
  --project <name>   project in the projects dir (for plain-name targets)
  --host <host>      create on a remote host (needs we installed there),
                     then attach locally over ssh
  --no-terminal      skip opening/focusing the terminal

Flags for delete:
  --project <name>    project the environment belongs to (else inferred)
  --host <host>       delete on a remote host
  --force             remove the worktree even if it has local changes
  --delete-branch     also delete the branch
  --keep-worktree     only kill the tmux session

Config (XDG): ` + "~/.config/workenv/config.toml" + `
  projects_dir  = "~/projects"        where repositories live
  worktrees_dir = "<projects_dir>/.we" worktree layout root
  claude_cmd    = "claude"            command run in the first tmux window
  remote_we     = "we"                we binary path on remote hosts
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
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "create", "up":
		return runCreate(env, rest)
	case "list", "ls":
		return runList(env, rest)
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

func runCreate(env *we.Env, args []string) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	project := fs.String("project", "", "project name")
	host := fs.String("host", "", "remote host")
	noTerminal := fs.Bool("no-terminal", false, "skip terminal step")
	raw, err := parseWithArg(fs, args)
	if err != nil {
		return err
	}
	if raw == "" {
		return errors.New("create: missing target (issue URL, PR URL, or name)")
	}
	tgt, err := target.Parse(raw)
	if err != nil {
		return err
	}

	if *host != "" {
		return createRemote(env, *host, raw, *project, *noTerminal)
	}

	res, err := env.Up(we.UpOptions{Target: tgt, Project: *project, NoTerminal: *noTerminal})
	if err != nil {
		return err
	}
	fmt.Printf("project:  %s (%s)\n", res.Project, res.RepoDir)
	fmt.Printf("branch:   %s\n", res.Branch)
	fmt.Printf("worktree: %s\n", res.Path)
	fmt.Printf("session:  %s\n", res.Session)
	// Machine-readable marker; the remote-create flow parses it over ssh.
	fmt.Printf("WE_SESSION=%s\n", res.Session)
	return nil
}

// createRemote tears up the environment on the remote host (terminal step
// skipped there) and attaches to it from a local terminal over ssh.
func createRemote(env *we.Env, host, rawTarget, project string, noTerminal bool) error {
	remote := []string{host, env.Cfg.RemoteWe, "create", rawTarget, "--no-terminal"}
	if project != "" {
		remote = append(remote, "--project", project)
	}
	out, err := env.R.Output("", "ssh", remote...)
	if out != "" {
		fmt.Println(out)
	}
	if err != nil {
		return fmt.Errorf("remote create on %s: %w", host, err)
	}
	session := ""
	for _, line := range strings.Split(out, "\n") {
		if s, ok := strings.CutPrefix(strings.TrimSpace(line), "WE_SESSION="); ok {
			session = s
		}
	}
	if session == "" {
		return fmt.Errorf("remote we did not report a session (is %q installed on %s?)", env.Cfg.RemoteWe, host)
	}
	if noTerminal {
		fmt.Printf("attach with: ssh -t %s tmux attach-session -t %s\n", host, session)
		return nil
	}
	return env.AttachRemote(host, session)
}

func runList(env *we.Env, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	host := fs.String("host", "", "remote host")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *host != "" {
		return env.R.Run("", "ssh", *host, env.Cfg.RemoteWe, "list")
	}
	items, err := env.List()
	if err != nil {
		return err
	}
	if len(items) == 0 {
		fmt.Println("no work environments")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "PROJECT\tNAME\tBRANCH\tSESSION\tSTATE\tPATH")
	for _, it := range items {
		session := it.Session
		if session == "" {
			session = "-"
		}
		branch := it.Branch
		if branch == "" {
			branch = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", it.Project, it.Name, branch, session, it.SessionState, it.Path)
	}
	return w.Flush()
}

func runDelete(env *we.Env, args []string) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	project := fs.String("project", "", "project name")
	host := fs.String("host", "", "remote host")
	force := fs.Bool("force", false, "remove worktree even if dirty")
	deleteBranch := fs.Bool("delete-branch", false, "also delete the branch")
	keepWorktree := fs.Bool("keep-worktree", false, "only kill the tmux session")
	name, err := parseWithArg(fs, args)
	if err != nil {
		return err
	}
	if name == "" {
		return errors.New("delete: missing work environment name")
	}
	if *host != "" {
		remote := []string{*host, env.Cfg.RemoteWe, "delete", name}
		if *project != "" {
			remote = append(remote, "--project", *project)
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
	if err := env.Down(name, *project, we.DownOptions{
		Force:        *force,
		DeleteBranch: *deleteBranch,
		KeepWorktree: *keepWorktree,
	}); err != nil {
		return err
	}
	fmt.Printf("deleted work environment %q\n", name)
	return nil
}
