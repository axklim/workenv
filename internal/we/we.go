// Package we orchestrates the Smart work environment flows described in the
// design doc: tear up (project → worktree → tmux session running claude →
// terminal), tear down, and a stateless listing.
//
// Statelessness: nothing is persisted. A work environment is fully
// identified by (project, name) — the worktree lives at the deterministic
// path <worktrees_dir>/<project>/<name>, the tmux session is named
// we-<project>-<name> and tagged with @workenv_* user options, and issue/PR
// numbers are encoded in branch and environment names.
package we

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"workenv/internal/config"
	"workenv/internal/execx"
	"workenv/internal/gh"
	"workenv/internal/gitx"
	"workenv/internal/naming"
	"workenv/internal/target"
	"workenv/internal/tmuxx"
)

type Env struct {
	Cfg config.Config
	R   execx.Runner
	// GOOS controls terminal integration (Ghostty via `open` on darwin).
	GOOS string
	// Cwd is where we was invoked; a repo containing it is preferred over
	// the projects directory.
	Cwd string
	// InsideTmux indicates we runs inside a tmux client already.
	InsideTmux bool
}

type UpOptions struct {
	Target     target.Target
	Project    string // explicit project name override
	NoTerminal bool
}

type DownOptions struct {
	Force        bool // remove worktree even if dirty
	DeleteBranch bool
	KeepWorktree bool
}

type UpResult struct {
	Project string
	Name    string
	Branch  string
	Path    string
	Session string
	RepoDir string
}

type Item struct {
	Project      string
	Name         string
	Branch       string
	Path         string
	Session      string
	SessionState string // attached | detached | none
}

func (e *Env) git() gitx.Git     { return gitx.Git{R: e.R} }
func (e *Env) tmux() tmuxx.Tmux  { return tmuxx.Tmux{R: e.R} }
func (e *Env) github() gh.Client { return gh.Client{R: e.R} }

// Up is the tear-up flow: resolve the project repository (cloning bare if
// needed), find or create the worktree, find or create the tagged tmux
// session with claude in the first window, and surface it in a terminal.
func (e *Env) Up(opts UpOptions) (UpResult, error) {
	project, repoDir, name, branch, prNumber, err := e.resolve(opts)
	if err != nil {
		return UpResult{}, err
	}

	if prNumber > 0 {
		if err := e.git().FetchPRBranch(repoDir, prNumber, branch); err != nil {
			return UpResult{}, err
		}
	}

	// Worktree: reuse wherever the branch is already checked out.
	path, ok := e.git().WorktreeForBranch(repoDir, branch)
	if !ok {
		path = filepath.Join(e.Cfg.WorktreesDir, project, naming.Sanitize(name))
		if err := e.git().AddWorktree(repoDir, path, branch); err != nil {
			return UpResult{}, err
		}
	}

	// tmux session, tagged so `we list` can find it later.
	session := naming.SessionName(project, name)
	if !e.tmux().Has(session) {
		if err := e.tmux().New(session, path, project, naming.Sanitize(name)); err != nil {
			return UpResult{}, err
		}
		if err := e.tmux().RunInFirstWindow(session, e.Cfg.ClaudeCmd); err != nil {
			return UpResult{}, err
		}
	}

	if !opts.NoTerminal {
		if err := e.showInTerminal(session); err != nil {
			return UpResult{}, err
		}
	}

	return UpResult{Project: project, Name: name, Branch: branch, Path: path, Session: session, RepoDir: repoDir}, nil
}

// resolve maps the target onto (project, repoDir, name, branch) and, for
// PRs, the number whose head must be fetched (0 otherwise).
func (e *Env) resolve(opts UpOptions) (project, repoDir, name, branch string, prNumber int, err error) {
	t := opts.Target
	switch t.Kind {
	case target.KindIssue:
		issue, ierr := e.github().Issue(t.Owner, t.Repo, t.Number)
		if ierr != nil {
			return "", "", "", "", 0, ierr
		}
		repoDir, err = e.projectRepo(t.Owner, t.Repo, true)
		if err != nil {
			return "", "", "", "", 0, err
		}
		branch = naming.BranchForIssue(issue.Number, issue.Title)
		return t.Repo, repoDir, branch, branch, 0, nil

	case target.KindPR:
		pr, perr := e.github().PR(t.Owner, t.Repo, t.Number)
		if perr != nil {
			return "", "", "", "", 0, perr
		}
		repoDir, err = e.projectRepo(t.Owner, t.Repo, true)
		if err != nil {
			return "", "", "", "", 0, err
		}
		name = naming.PRName(pr.Number)
		// Same-repo PRs track the real head branch; fork PRs get a local
		// pr-N branch materialized from refs/pull/N/head.
		if e.git().BranchExists(repoDir, "refs/heads/"+pr.HeadRefName) ||
			e.git().BranchExists(repoDir, "refs/remotes/origin/"+pr.HeadRefName) {
			return t.Repo, repoDir, name, pr.HeadRefName, 0, nil
		}
		return t.Repo, repoDir, name, name, pr.Number, nil

	default: // KindName
		name = t.Name
		project = opts.Project
		if project == "" {
			if top := e.git().TopLevel(e.Cwd); top != "" {
				project = filepath.Base(top)
				repoDir = top
			}
		}
		if project == "" {
			return "", "", "", "", 0, fmt.Errorf("not inside a repository: pass --project (looked in %s)", e.Cfg.ProjectsDir)
		}
		if repoDir == "" {
			dir, found := gitx.FindProjectDir(e.Cfg.ProjectsDir, project)
			if !found {
				return "", "", "", "", 0, fmt.Errorf("project %q not found in %s", project, e.Cfg.ProjectsDir)
			}
			repoDir = dir
		}
		return project, repoDir, name, name, 0, nil
	}
}

// projectRepo locates the repository for owner/repo: the repo containing
// the current directory when its origin matches, then the projects
// directory, then (optionally) a fresh bare clone with refs setup.
func (e *Env) projectRepo(owner, repo string, cloneIfMissing bool) (string, error) {
	if top := e.git().TopLevel(e.Cwd); top != "" {
		if o, r, ok := e.git().OriginGitHubRepo(top); ok && o == owner && r == repo {
			return top, nil
		}
	}
	if dir, found := gitx.FindProjectDir(e.Cfg.ProjectsDir, repo); found {
		return dir, nil
	}
	if !cloneIfMissing {
		return "", fmt.Errorf("project %q not found in %s", repo, e.Cfg.ProjectsDir)
	}
	dest := filepath.Join(e.Cfg.ProjectsDir, repo+".git")
	if err := os.MkdirAll(e.Cfg.ProjectsDir, 0o755); err != nil {
		return "", err
	}
	fmt.Fprintf(os.Stderr, "cloning %s/%s (bare) into %s\n", owner, repo, dest)
	if err := e.git().CloneBare(owner+"/"+repo, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// showInTerminal implements the "define terminal" step: switch the current
// tmux client if we already runs inside tmux; focus Ghostty if some client
// is already attached to the session; otherwise open a new Ghostty window
// attached to it.
func (e *Env) showInTerminal(session string) error {
	if e.InsideTmux {
		return e.tmux().SwitchClient(session)
	}
	if e.tmux().HasClients(session) {
		return e.focusTerminal()
	}
	return e.openTerminal("tmux", "attach-session", "-t", session)
}

func (e *Env) focusTerminal() error {
	if e.GOOS == "darwin" {
		return e.R.Run("", "open", "-a", "Ghostty")
	}
	return nil // no portable focus mechanism; the session is attachable manually
}

// openTerminal opens a new Ghostty window running argv.
func (e *Env) openTerminal(argv ...string) error {
	if e.GOOS == "darwin" {
		args := append([]string{"-na", "Ghostty", "--args", "-e"}, argv...)
		return e.R.Run("", "open", args...)
	}
	return e.R.Run("", "ghostty", append([]string{"-e"}, argv...)...)
}

// AttachRemote opens a local terminal attached (over ssh) to a session on a
// remote host.
func (e *Env) AttachRemote(host, session string) error {
	return e.openTerminal("ssh", "-t", host, "tmux", "attach-session", "-t", session)
}

// Down is the tear-down flow: kill the tmux session and remove the
// worktree (and optionally the branch). It is forgiving: whatever half of
// the environment still exists gets cleaned up.
func (e *Env) Down(name, project string, opts DownOptions) error {
	if project == "" {
		resolved, err := e.findProjectFor(name)
		if err != nil {
			return err
		}
		project = resolved
	}
	repoDir, found := gitx.FindProjectDir(e.Cfg.ProjectsDir, project)
	if !found {
		if top := e.git().TopLevel(e.Cwd); top != "" && filepath.Base(top) == project {
			repoDir = top
			found = true
		}
	}

	session := naming.SessionName(project, name)
	killed := false
	if e.tmux().Has(session) {
		if err := e.tmux().Kill(session); err != nil {
			return err
		}
		killed = true
	}

	if opts.KeepWorktree {
		if !killed {
			return fmt.Errorf("nothing to tear down for %s/%s", project, name)
		}
		return nil
	}
	if !found {
		return fmt.Errorf("project %q not found in %s (session %s %s)", project, e.Cfg.ProjectsDir, session,
			map[bool]string{true: "killed", false: "not found either"}[killed])
	}

	path := filepath.Join(e.Cfg.WorktreesDir, project, naming.Sanitize(name))
	branch := ""
	removed := false
	if wts, err := e.git().ListWorktrees(repoDir); err == nil {
		for _, wt := range wts {
			if wt.Path == path {
				branch = wt.Branch
				if err := e.git().RemoveWorktree(repoDir, path, opts.Force); err != nil {
					return err
				}
				removed = true
				break
			}
		}
	}
	if !killed && !removed {
		return fmt.Errorf("nothing to tear down for %s/%s", project, name)
	}
	if opts.DeleteBranch && branch != "" {
		if err := e.git().DeleteBranch(repoDir, branch); err != nil {
			return err
		}
	}
	return nil
}

// findProjectFor recovers the project of a work environment from the tmux
// session tags or the worktree directory layout — no state file needed.
func (e *Env) findProjectFor(name string) (string, error) {
	var candidates []string
	if sessions, err := e.tmux().List(); err == nil {
		for _, s := range sessions {
			if s.WeName == naming.Sanitize(name) {
				candidates = append(candidates, s.Project)
			}
		}
	}
	if len(candidates) == 0 {
		entries, _ := os.ReadDir(e.Cfg.WorktreesDir)
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			if _, err := os.Stat(filepath.Join(e.Cfg.WorktreesDir, entry.Name(), naming.Sanitize(name))); err == nil {
				candidates = append(candidates, entry.Name())
			}
		}
	}
	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("no work environment named %q found; pass --project", name)
	case 1:
		return candidates[0], nil
	default:
		return "", fmt.Errorf("%q exists in multiple projects (%v); pass --project", name, candidates)
	}
}

// List merges tmux sessions (tagged with @workenv_*) and worktrees found
// under the deterministic layout into one view.
func (e *Env) List() ([]Item, error) {
	items := map[string]Item{}

	sessions, err := e.tmux().List()
	if err != nil {
		return nil, err
	}
	for _, s := range sessions {
		state := "detached"
		if s.Attached {
			state = "attached"
		}
		items[s.Project+"/"+s.WeName] = Item{
			Project: s.Project, Name: s.WeName, Path: s.Path,
			Session: s.Name, SessionState: state,
		}
	}

	projects, _ := os.ReadDir(e.Cfg.WorktreesDir)
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		wts, _ := os.ReadDir(filepath.Join(e.Cfg.WorktreesDir, p.Name()))
		for _, wt := range wts {
			if !wt.IsDir() {
				continue
			}
			key := p.Name() + "/" + wt.Name()
			path := filepath.Join(e.Cfg.WorktreesDir, p.Name(), wt.Name())
			item, exists := items[key]
			if !exists {
				item = Item{Project: p.Name(), Name: wt.Name(), Path: path, SessionState: "none"}
			}
			if branch, berr := e.R.Output(path, "git", "rev-parse", "--abbrev-ref", "HEAD"); berr == nil {
				item.Branch = branch
			}
			items[key] = item
		}
	}

	var out []Item
	for _, item := range items {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Project != out[j].Project {
			return out[i].Project < out[j].Project
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
