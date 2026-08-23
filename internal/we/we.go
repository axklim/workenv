// Package we orchestrates the work environment flows: open (find or create
// the project checkout, worktree and tmux session running claude, then
// surface it in a terminal), attach (find only), delete and list.
//
// A work environment is a record in the state registry (package state)
// keyed by an integer id: its branch, tmux session and worktree path are
// stored, not derived, so nothing has to be encoded in names. GitHub issue
// and PR references are attached to the record as canonical URLs, which is
// how an issue URL and its linked PR URL resolve to the same environment.
// Git stays the truth for the branch: it is refreshed from the worktree
// whenever an environment is opened or listed, and the refreshed value is
// persisted back to the registry.
//
// Resolution order is always registry, then GitHub, then git worktrees
// (see resolve.go). open and attach share that one path and differ only in
// whether resolution may create a new environment when nothing is found.
package we

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"workenv/internal/config"
	"workenv/internal/execx"
	"workenv/internal/gh"
	"workenv/internal/gitx"
	"workenv/internal/naming"
	"workenv/internal/state"
	"workenv/internal/target"
	"workenv/internal/tmuxx"
	"workenv/internal/wtpath"
)

type Env struct {
	Cfg config.Config
	R   execx.Runner
	// GOOS controls terminal integration (Ghostty via `open` on darwin).
	GOOS string
	// Cwd is where we was invoked; a repository containing it is preferred
	// when resolving GitHub targets, and used to mark the current environment
	// in List.
	Cwd string
	// InsideTmux indicates we runs inside a tmux client already.
	InsideTmux bool
	// StatePath is the JSON registry of work environments.
	StatePath string
}

// OpenOptions describes a target to find or create a work environment for.
// Branch, Session and Wt only take effect when a new environment is
// created; on a hit they are reported back as ignored (OpenResult.IgnoredOverrides).
type OpenOptions struct {
	Target target.Target
	Repo   string // --repo <name|path>: repository for a plain-name/branch target

	Branch, Session, Wt string // creation overrides

	// AttachOnly finds — through the registry, GitHub links and git
	// worktrees — but never creates, clones or fetches.
	AttachOnly bool
	NoTerminal bool
}

type OpenResult struct {
	ID                       int
	Project, Branch, Session string
	WorktreePath, RepoPath   string
	Created                  bool
	IgnoredOverrides         []string // e.g. ["--branch", "--wt"]
}

type DeleteOptions struct {
	Force        bool // remove the worktree even if it has local changes
	DeleteBranch bool
	KeepWorktree bool
}

// Item is one row of `we ls` / the result of `we show`.
type Item struct {
	ID                       int
	Project, Branch, Session string
	SessionState             string // attached | detached | none
	WorktreePath, RepoPath   string
	Issues, PRs              []string
	Exists, Current          bool
	CreatedAt                time.Time
}

func (e *Env) git() gitx.Git     { return gitx.Git{R: e.R} }
func (e *Env) tmux() tmuxx.Tmux  { return tmuxx.Tmux{R: e.R} }
func (e *Env) github() gh.Client { return gh.Client{R: e.R} }

// Open finds the work environment for the target — creating it unless
// opts.AttachOnly — repairs its worktree and tmux session, saves the
// registry and surfaces the session in a terminal.
func (e *Env) Open(opts OpenOptions) (OpenResult, error) {
	st, err := state.Load(e.StatePath)
	if err != nil {
		return OpenResult{}, err
	}
	env, created, err := e.resolve(st, opts)
	if err != nil {
		return OpenResult{}, err
	}
	if err := e.repair(env, created); err != nil {
		return OpenResult{}, err
	}
	if err := st.Save(); err != nil {
		return OpenResult{}, err
	}
	var ignored []string
	if !created {
		ignored = ignoredOverrides(opts)
	}
	if !opts.NoTerminal {
		if err := e.showInTerminal(env.TmuxSession); err != nil {
			return OpenResult{}, err
		}
	}
	return OpenResult{
		ID: env.ID, Project: env.Project, Branch: env.Branch, Session: env.TmuxSession,
		WorktreePath: env.WorktreePath, RepoPath: env.RepoPath, Created: created,
		IgnoredOverrides: ignored,
	}, nil
}

// ignoredOverrides names the creation overrides the caller passed, for
// reporting when they landed on a hit instead of a creation.
func ignoredOverrides(opts OpenOptions) []string {
	var out []string
	if opts.Branch != "" {
		out = append(out, "--branch")
	}
	if opts.Session != "" {
		out = append(out, "--session")
	}
	if opts.Wt != "" {
		out = append(out, "--wt")
	}
	return out
}

// create records a new environment for sp: project and session are derived
// (or taken from opts), the worktree location is decided (override,
// adoption, or the placement template), the two are checked against the
// registry for collisions, the branch is prepared for fork/same-repo PRs,
// and the record is added — but not yet materialised on disk or in tmux;
// that is repair's job, run uniformly for a hit and a fresh creation alike.
func (e *Env) create(st *state.Store, opts OpenOptions, sp spec, existingPath string) (*state.Env, error) {
	project := e.git().ProjectName(sp.repoPath)

	session := naming.Sanitize(opts.Session)
	if session == "" {
		session = naming.SessionName(project, sp.branch)
	}
	if other := st.BySession(session); other != nil {
		return nil, fmt.Errorf("tmux session %q already belongs to environment %d; pass --session", session, other.ID)
	}

	wtPath, err := e.worktreePath(sp, project, opts.Wt, existingPath)
	if err != nil {
		return nil, err
	}
	if other := st.ByWorktree(wtPath); other != nil {
		return nil, fmt.Errorf("worktree %q already belongs to environment %d; pass --wt", wtPath, other.ID)
	}

	switch {
	case sp.forkPR > 0:
		if err := e.git().FetchPRBranch(sp.repoPath, sp.forkPR, sp.branch); err != nil {
			return nil, err
		}
	case sp.fetch:
		if err := e.git().EnsureOriginBranch(sp.repoPath, sp.branch); err != nil {
			return nil, err
		}
	}

	env := &state.Env{
		Project: project, Branch: sp.branch, TmuxSession: session,
		WorktreePath: wtPath, RepoPath: sp.repoPath, CreatedAt: time.Now().UTC(),
	}
	st.Link(env, sp.issues, sp.prs)
	st.Add(env)
	return env, nil
}

// worktreePath decides where a new (or freshly adopted) environment's
// worktree lives: an explicit --wt wins outright — verbatim (expanded,
// resolved against the repository and cleaned) when it looks like a path,
// otherwise substituted for the branch when rendering the placement
// template; absent an override, a worktree already checked out on the
// branch (existingPath) is adopted; otherwise the template is rendered
// against the real branch.
//
// A branch can only be checked out in one worktree, so when both an
// override and an existing checkout are present and they disagree, --wt is
// asking git to check the same branch out a second time — which `git
// worktree add` refuses with a confusing "already checked out at ..."
// error. That case is caught here instead, before create ever calls git.
func (e *Env) worktreePath(sp spec, project, override, existingPath string) (string, error) {
	if override == "" {
		if existingPath != "" {
			return existingPath, nil
		}
		return e.renderPlacement(sp, project, sp.branch)
	}
	wtPath, err := e.overriddenWtPath(sp, project, override)
	if err != nil {
		return "", err
	}
	if existingPath != "" && wtPath != existingPath {
		return "", fmt.Errorf("branch %q is already checked out at %s; --wt cannot check it out a second time", sp.branch, existingPath)
	}
	return wtPath, nil
}

// overriddenWtPath resolves a non-empty --wt: verbatim (expanded, resolved
// against the repository and cleaned) when it looks like a path, otherwise
// substituted for the branch when rendering the placement template.
func (e *Env) overriddenWtPath(sp spec, project, override string) (string, error) {
	if strings.ContainsAny(override, "/\\") || strings.HasPrefix(override, "~") {
		return resolveWtPath(override, sp.repoPath), nil
	}
	return e.renderPlacement(sp, project, override)
}

// resolveWtPath turns a verbatim --wt value into an absolute, clean path:
// ~ expands, a still-relative value resolves against the repository (the
// same base wtpath.Render uses), and the result is cleaned — so it matches
// exactly what git and a later os.Stat/ByWorktree lookup will see, a
// trailing slash included.
func resolveWtPath(override, repoPath string) string {
	p := expandHome(override)
	if !filepath.IsAbs(p) {
		p = filepath.Join(repoPath, p)
	}
	return filepath.Clean(p)
}

func (e *Env) renderPlacement(sp spec, project, branch string) (string, error) {
	return wtpath.Render(e.Cfg.WorktreePath, wtpath.Vars{
		RepoPath: sp.repoPath, Repo: repoBase(sp.repoPath), Project: project, Owner: sp.owner, Branch: branch,
	})
}

// repair makes sure env's worktree directory and tmux session exist —
// creating them when missing exactly like it would to bring a stale
// environment back after a reboot or a deleted directory — and refreshes
// its branch from git, the truth for it. It runs for a hit as much as for
// a fresh creation: create only records where things should be, this is
// what actually materialises them. created selects which of the two
// Adoption refusal messages repairSession gives on a session-name conflict:
// --session still helps on a fresh creation, but cannot on a hit.
func (e *Env) repair(env *state.Env, created bool) error {
	if err := e.repairWorktree(env); err != nil {
		return err
	}
	if err := e.repairSession(env, created); err != nil {
		return err
	}
	if b := e.git().CurrentBranch(env.WorktreePath); b != "" {
		env.Branch = b
	}
	return nil
}

// repairWorktree re-adds env's worktree when its directory is missing. When
// the directory is present it must actually be a git checkout — a stray
// directory left at the recorded path (by hand, or by whatever removed the
// real worktree) is not one, and starting a session there instead of a
// checkout would be worse than the missing-directory case repair exists to
// fix.
func (e *Env) repairWorktree(env *state.Env) error {
	if _, err := os.Stat(env.WorktreePath); err == nil {
		if e.git().CurrentBranch(env.WorktreePath) == "" {
			return fmt.Errorf("%s exists but is not a git checkout", env.WorktreePath)
		}
		return nil
	}
	if err := e.git().Prune(env.RepoPath); err != nil {
		return err
	}
	return e.git().AddWorktree(env.RepoPath, env.WorktreePath, env.Branch)
}

// repairSession creates and tags the session when it is missing. A session
// that is already live is reused only when it carries the @workenv tag —
// otherwise it belongs to someone else, and we must not touch it. The
// refusal differs by case (see the design doc's Adoption paragraph):
// creating a new environment, --session picks another name and genuinely
// helps; for an environment that already exists, --session cannot change
// anything (it is a creation-only override, ignored on a hit), so the
// message instead points at the conflicting session itself.
func (e *Env) repairSession(env *state.Env, created bool) error {
	if e.tmux().Has(env.TmuxSession) {
		if !e.tmux().IsWorkenv(env.TmuxSession) {
			if created {
				return fmt.Errorf("tmux session %q already exists and is not a workenv session; pass --session", env.TmuxSession)
			}
			return fmt.Errorf("tmux session %q (environment %d) is taken by a session we does not own; rename or kill it, or run `we delete %d`", env.TmuxSession, env.ID, env.ID)
		}
		return nil
	}
	if err := e.tmux().New(env.TmuxSession, env.WorktreePath, env.ID); err != nil {
		return err
	}
	return e.tmux().RunInFirstWindow(env.TmuxSession, claudeCommand(e.Cfg.ClaudeCmd, env.TmuxSession))
}

func claudeCommand(cmd, session string) string {
	if namesSession(cmd) {
		return cmd
	}
	return cmd + " --name " + naming.Sanitize(session)
}

func namesSession(cmd string) bool {
	for _, f := range strings.Fields(cmd) {
		if f == "-n" || f == "--name" || strings.HasPrefix(f, "--name=") {
			return true
		}
	}
	return false
}

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

func (e *Env) Delete(t target.Target, repo string, opts DeleteOptions) (id int, session string, err error) {
	st, err := state.Load(e.StatePath)
	if err != nil {
		return 0, "", err
	}
	env, err := e.lookupRegistry(st, t, repo)
	if err != nil {
		return 0, "", err
	}
	if env == nil {
		killed, err := e.killStray(t)
		if err != nil {
			return 0, "", err
		}
		return 0, killed, nil
	}
	id, session = env.ID, env.TmuxSession
	if e.tmux().Has(env.TmuxSession) {
		if e.tmux().IsWorkenv(env.TmuxSession) {
			if err := e.tmux().Kill(env.TmuxSession); err != nil {
				return 0, "", err
			}
		} else {
			// Someone else's session, not ours to touch (see Adoption in
			// the design doc); teardown still proceeds for the rest.
			fmt.Fprintf(os.Stderr, "we: tmux session %q is not a workenv session; leaving it running\n", env.TmuxSession)
		}
	}
	if opts.KeepWorktree {
		return id, session, nil
	}
	if env.WorktreePath == env.RepoPath {
		// The main working tree: git refuses to remove it (even with
		// --force) and removing its branch would leave the repository
		// without a checked-out HEAD, so both steps are skipped.
		fmt.Fprintf(os.Stderr, "we: %s is the repository's main working tree; leaving it and its branch in place\n", env.WorktreePath)
	} else {
		if _, err := os.Stat(env.WorktreePath); err == nil {
			if err := e.git().RemoveWorktree(env.RepoPath, env.WorktreePath, opts.Force); err != nil {
				return 0, "", err
			}
		} else {
			// Directory already gone: just let git forget it.
			_ = e.git().Prune(env.RepoPath)
		}
		if opts.DeleteBranch && env.Branch != "" {
			if err := e.git().DeleteBranch(env.RepoPath, env.Branch); err != nil {
				return 0, "", err
			}
		}
	}
	st.Remove(env.ID)
	if err := st.Save(); err != nil {
		return 0, "", err
	}
	return id, session, nil
}

// killStray is delete's last resort for a target that is not in the
// registry: a live @workenv-tagged tmux session by that name — a lost or
// hand-edited registry — still gets killed, and its name reported back so
// the caller has something to print. Anything else is unknown.
func (e *Env) killStray(t target.Target) (string, error) {
	if t.Kind == target.KindName && e.tmux().Has(t.Name) && e.tmux().IsWorkenv(t.Name) {
		return t.Name, e.tmux().Kill(t.Name)
	}
	return "", notFoundInRegistry(t)
}

// notFoundInRegistry is delete's and show's "nothing matched" message. A
// repository-URL target gets a more specific explanation: unlike an issue
// or PR URL, it was never a lookup key recorded on any environment, so it
// can never resolve to one.
func notFoundInRegistry(t target.Target) error {
	if t.Kind == target.KindRepo {
		return fmt.Errorf("a repository URL does not identify a single environment; pass its id, session or branch")
	}
	return fmt.Errorf("no work environment for %s", t.String())
}

// Show resolves like Delete — the registry only — and reports the same
// Item List would for that environment.
func (e *Env) Show(t target.Target, repo string) (Item, error) {
	st, err := state.Load(e.StatePath)
	if err != nil {
		return Item{}, err
	}
	env, err := e.lookupRegistry(st, t, repo)
	if err != nil {
		return Item{}, err
	}
	if env == nil {
		return Item{}, notFoundInRegistry(t)
	}
	sessions, err := e.tmux().List()
	if err != nil {
		return Item{}, err
	}
	item, _ := e.toItem(env, sessions)
	return item, nil
}

func (e *Env) List() ([]Item, error) {
	st, err := state.Load(e.StatePath)
	if err != nil {
		return nil, err
	}
	sessions, err := e.tmux().List()
	if err != nil {
		return nil, err
	}
	items := make([]Item, 0, len(st.Envs))
	changed := false
	for _, env := range st.Envs {
		item, refreshed := e.toItem(env, sessions)
		items = append(items, item)
		changed = changed || refreshed
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	if changed {
		if err := st.Save(); err != nil {
			return nil, err
		}
	}
	return items, nil
}

// toItem builds env's Item, refreshing its branch from the worktree when
// one exists. The bool reports whether that refresh actually changed the
// stored branch, so List can Save once at the end rather than per item;
// Show ignores it, since a single lookup that is not being saved has
// nothing to batch.
func (e *Env) toItem(env *state.Env, sessions []tmuxx.Session) (Item, bool) {
	item := Item{
		ID: env.ID, Project: env.Project, Branch: env.Branch, Session: env.TmuxSession,
		WorktreePath: env.WorktreePath, RepoPath: env.RepoPath,
		Issues: env.Issues, PRs: env.PRs, CreatedAt: env.CreatedAt,
		SessionState: "none",
	}
	for _, s := range sessions {
		if s.Name == env.TmuxSession {
			item.SessionState = "detached"
			if s.Attached {
				item.SessionState = "attached"
			}
			break
		}
	}
	changed := false
	if _, err := os.Stat(env.WorktreePath); err == nil {
		item.Exists = true
		if b := e.git().CurrentBranch(env.WorktreePath); b != "" && b != env.Branch {
			env.Branch = b
			changed = true
		}
		item.Branch = env.Branch
	}
	item.Current = within(env.WorktreePath, e.Cwd)
	return item, changed
}

// within reports whether child is parent itself or somewhere inside it.
func within(parent, child string) bool {
	if parent == "" || child == "" {
		return false
	}
	parent, child = filepath.Clean(parent), filepath.Clean(child)
	if parent == child {
		return true
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// repoBase is the .repo placement variable: the repository directory's
// basename, without any bare ".git" suffix.
func repoBase(repoPath string) string {
	return strings.TrimSuffix(filepath.Base(repoPath), ".git")
}

// expandHome expands a leading ~ or ~/ to the home directory; anything else
// is returned unchanged.
func expandHome(p string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}
