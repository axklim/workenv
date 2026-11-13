package we

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"workenv/internal/gitx"
	"workenv/internal/state"
	"workenv/internal/target"
)

// NewPlan is how `we new` read its arguments: either an issue or PR URL to
// open as such, or a branch in a repository. Reading is that decision, one
// line each, for the caller to print before the open's own output.
type NewPlan struct {
	Target   target.Target // an issue or PR URL; the zero Target for a branch
	Branch   string        // with Target: --branch; otherwise the branch to open
	RepoPath string        // the repository of Branch, without Target
	Reading  []string

	trustErr error // the clone PlanNew made could not be marked trusted
}

// PlanNew reads `we new`'s one or two arguments (see the design doc's
// *new*): an issue or PR URL, optionally with a branch name; a repository
// and a name; a repository alone, for the next free feature-N; or a name in
// the cwd's repository. It clones a repository that is not on disk, and
// refuses one listed in no_workers.
func (e *Env) PlanNew(args []string) (NewPlan, error) {
	e.trustErr = nil
	if len(args) == 0 || len(args) > 2 {
		return NewPlan{}, errors.New("we new takes a repository, an issue URL or a name, and optionally a name")
	}
	first, err := target.Parse(args[0])
	if err != nil {
		return NewPlan{}, err
	}
	name := ""
	if len(args) == 2 {
		name = args[1]
	}

	if first.Kind == target.KindIssue || first.Kind == target.KindPR {
		if err := e.refuseNoWorkers(first.Repo); err != nil {
			return NewPlan{}, err
		}
		p := NewPlan{Target: first, Branch: name, Reading: []string{"target: " + first.String()}}
		if name != "" {
			p.Reading = append(p.Reading, "branch: "+name+" (given)")
		}
		return p, nil
	}

	repoPath, how, err := e.newRepo(args[0], first, len(args) == 2)
	if err != nil {
		return NewPlan{}, err
	}
	if repoPath == "" {
		// One argument that is no repository: a branch in the cwd's.
		if repoPath = e.git().RepoRoot(e.Cwd); repoPath == "" {
			return NewPlan{}, fmt.Errorf("%q is no alias, repository in %s or path, and %s is not inside a repository", args[0], e.Cfg.ProjectsPath, e.Cwd)
		}
		how, name = "cwd", args[0]
	}
	if err := e.refuseNoWorkers(e.git().ProjectName(repoPath)); err != nil {
		return NewPlan{}, err
	}
	p := NewPlan{RepoPath: repoPath, Branch: name, Reading: []string{fmt.Sprintf("repo: %s (%s)", repoBase(repoPath), how)}, trustErr: e.trustErr}
	if name != "" {
		p.Reading = append(p.Reading, "branch: "+name+" (given)")
		return p, nil
	}
	if p.Branch, err = e.nextFeature(repoPath); err != nil {
		return NewPlan{}, err
	}
	p.Reading = append(p.Reading, "branch: "+p.Branch+" (next free)")
	return p, nil
}

// newRepo resolves arg as `we new`'s repository and says how: "." is the
// cwd's; a repository URL is found where open would find it, or cloned
// like open clones it; an alias, a directory in projects_path or a path
// follow --repo. When explicit — a name follows — a bare name that is none
// of these is cloned from default_owner, when set. Unless explicit, an arg
// that is none of these, or is the cwd's own repository, gives "" and no
// error: it is a branch.
func (e *Env) newRepo(arg string, t target.Target, explicit bool) (path, how string, err error) {
	cwdRepo := e.git().RepoRoot(e.Cwd)
	switch {
	case arg == ".":
		if cwdRepo == "" {
			return "", "", fmt.Errorf("%s is not inside a repository", e.Cwd)
		}
		return cwdRepo, "cwd", nil
	case t.Kind == target.KindRepo:
		path, err := e.repoForTarget(t, true)
		if err != nil {
			return e.cloneForNew(t.Owner, t.Repo)
		}
		if path == cwdRepo {
			return path, "cwd", nil
		}
		return path, "projects_path", nil
	}
	if _, ok := e.Cfg.Aliases[arg]; ok {
		path, err := e.repoFromFlag(arg)
		if err != nil {
			return "", "", err
		}
		return e.notCwd(path, cwdRepo, "alias "+arg, explicit)
	}
	if strings.ContainsAny(arg, "/\\") || strings.HasPrefix(arg, "~") {
		if path := e.git().RepoRoot(expandHome(arg)); path != "" {
			return e.notCwd(path, cwdRepo, "path", explicit)
		}
		if explicit {
			return "", "", fmt.Errorf("%s is not a git repository", arg)
		}
		return "", "", nil
	}
	if path, ok := gitx.FindProjectDir(e.Cfg.ProjectsPath, arg); ok {
		return e.notCwd(path, cwdRepo, "projects_path", explicit)
	}
	if !explicit {
		return "", "", nil
	}
	if e.Cfg.DefaultOwner == "" {
		return "", "", fmt.Errorf("repository %q not found in %s; pass its URL to clone it, or set default_owner", arg, e.Cfg.ProjectsPath)
	}
	return e.cloneForNew(e.Cfg.DefaultOwner, arg)
}

// cloneForNew clones owner/repo for newRepo, unless repo takes no work
// environments: a refused repository is not worth a clone.
func (e *Env) cloneForNew(owner, repo string) (path, how string, err error) {
	if err := e.refuseNoWorkers(repo); err != nil {
		return "", "", err
	}
	if path, err = e.clone(owner, repo); err != nil {
		return "", "", fmt.Errorf("%s/%s is not in %s and could not be cloned: %w", owner, repo, e.Cfg.ProjectsPath, err)
	}
	return path, "cloned " + owner + "/" + repo, nil
}

// notCwd is newRepo's answer for a repository it found: a lone argument
// naming the cwd's own repository is a branch there instead.
func (e *Env) notCwd(path, cwdRepo, how string, explicit bool) (string, string, error) {
	if !explicit && path == cwdRepo {
		return "", "", nil
	}
	return path, how, nil
}

// nextFeature is feature-N for the smallest N ≥ 1 that repoPath has neither
// as a branch, locally or on origin, nor as an environment's branch.
func (e *Env) nextFeature(repoPath string) (string, error) {
	branches, err := e.git().Branches(repoPath)
	if err != nil {
		return "", err
	}
	st, err := state.Load(e.StatePath)
	if err != nil {
		return "", err
	}
	for n := 1; ; n++ {
		b := "feature-" + strconv.Itoa(n)
		if !branches[b] && st.ByBranch(repoPath, b) == nil {
			return b, nil
		}
	}
}

// New opens what PlanNew read: the URL like open does, or the branch in its
// repository — found, adopted or created like a plain name under --repo,
// but never read as an id or a session name.
func (e *Env) New(p NewPlan, opts OpenOptions) (OpenResult, error) {
	if p.Target != (target.Target{}) {
		opts.Target, opts.Branch = p.Target, p.Branch
		return e.Open(opts)
	}
	opts.Target = target.Target{Kind: target.KindName, Name: p.Branch}
	e.trustErr = p.trustErr
	return e.open(opts, func(st *state.Store) (*state.Env, bool, error) {
		owner, _, _ := e.git().OriginGitHubRepo(p.RepoPath)
		return e.finish(st, opts, spec{repoPath: p.RepoPath, owner: owner, branch: p.Branch}, opts.Target)
	})
}

// refuseNoWorkers is the no_workers refusal for the repository named repo.
func (e *Env) refuseNoWorkers(repo string) error {
	key, hint, ok := e.Cfg.NoWorkersFor(repo)
	if !ok {
		return nil
	}
	if hint == "" {
		return fmt.Errorf("%s takes no work environments", key)
	}
	return fmt.Errorf("%s takes no work environments: %s", key, hint)
}

// rcActive is the line Claude Code prints once Remote Control is up; the
// session's claude.ai link is on it or on the line after. Both texts are
// Claude Code's, and a UI change may need them updated.
const rcActive = "/remote-control is active"

var rcURL = regexp.MustCompile(`https://claude\.ai/code/session_[A-Za-z0-9]+`)

// trustDialog is the confirm button of claude's "Is this a project you
// created or one you trust?" dialog, Claude Code's text like rcActive. It is
// looked for only among the screen's last rcTailLines lines, where a waiting
// dialog sits, so an answered one left in the history does not count.
const (
	trustDialog = "Yes, I trust this folder"
	rcTailLines = 8
)

// ErrRCTimeout is RemoteControlURL giving up.
var ErrRCTimeout = errors.New("Remote Control did not come up")

// ErrTrustDialog is RemoteControlURL finding claude stopped on its trust
// dialog, which nothing in we answers for a repository it did not clone.
var ErrTrustDialog = errors.New("claude is waiting on its trust dialog")

// RemoteControlURL waits up to RCTimeout for Remote Control to come up in
// session's first pane and returns its link. The pane's history is read,
// so a session that is already up answers at once. It returns
// ErrTrustDialog as soon as the pane shows claude's trust dialog, and
// ErrRCTimeout on timeout, each with the last screen it read.
func (e *Env) RemoteControlURL(session string) (url, screen string, err error) {
	deadline := time.Now().Add(e.RCTimeout)
	for {
		if out, err := e.tmux().Capture(session); err == nil {
			screen = out
			if url := findRCURL(out); url != "" {
				return url, screen, nil
			}
			if onTrustDialog(out) {
				return "", screen, fmt.Errorf("%w in session %s: answer it there (we attach %s) or run claude in the repository once; "+
					"a repository we clones itself skips it with trust_clones = true", ErrTrustDialog, session, session)
			}
		}
		if !time.Now().Before(deadline) {
			return "", screen, fmt.Errorf("%w in session %s within %s", ErrRCTimeout, session, e.RCTimeout)
		}
		time.Sleep(e.RCInterval)
	}
}

// onTrustDialog reports whether screen's last lines show the trust dialog.
func onTrustDialog(screen string) bool {
	for _, line := range LastLines(screen, rcTailLines) {
		if strings.Contains(line, trustDialog) {
			return true
		}
	}
	return false
}

// findRCURL is the link on, or right after, the last rcActive line.
func findRCURL(screen string) string {
	lines := strings.Split(screen, "\n")
	url := ""
	for i, line := range lines {
		if !strings.Contains(line, rcActive) {
			continue
		}
		next := ""
		if i+1 < len(lines) {
			next = lines[i+1]
		}
		if u := rcURL.FindString(line + "\n" + next); u != "" {
			url = u
		}
	}
	return url
}

// LastLines is the last n non-blank lines of screen.
func LastLines(screen string, n int) []string {
	var lines []string
	for _, line := range strings.Split(screen, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
