package we

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"workenv/internal/gh"
	"workenv/internal/gitx"
	"workenv/internal/naming"
	"workenv/internal/state"
	"workenv/internal/target"
)

// spec is a fully resolved target: everything finish needs to find the
// environment already on this branch, or create it.
type spec struct {
	repoPath string
	owner    string // GitHub owner; "" when the target has none
	branch   string
	forkPR   int      // >0: materialise the branch from refs/pull/<n>/head
	fetch    bool     // same-repo PR: make sure origin/<branch> is fetched
	issues   []string // canonical URLs to link once the environment is known
	prs      []string
}

// resolve maps the target to its environment: registry, then GitHub links,
// then git worktrees (see the design doc's Resolution section). When
// nothing matches, it creates the record unless opts.AttachOnly. The bool
// reports whether a new environment was created.
func (e *Env) resolve(st *state.Store, opts OpenOptions) (*state.Env, bool, error) {
	switch opts.Target.Kind {
	case target.KindIssue:
		return e.resolveIssue(st, opts)
	case target.KindPR:
		return e.resolvePR(st, opts)
	case target.KindRepo:
		return e.resolveRepo(st, opts)
	default:
		return e.resolvePlain(st, opts)
	}
}

func notFound(what string) error {
	return fmt.Errorf("no work environment for %s (attach never creates one — use we open)", what)
}

// resolveIssue implements the design spec's issue-URL resolution: registry
// by URL, then a linked PR already in the registry, then a branch derived
// from --branch, the highest-numbered linked PR's head, or the title slug.
func (e *Env) resolveIssue(st *state.Store, opts OpenOptions) (*state.Env, bool, error) {
	t := opts.Target
	issueURL := t.URL()
	if env := st.ByRef(issueURL); env != nil {
		return env, false, nil
	}

	issue, err := e.github().Issue(t.Owner, t.Repo, t.Number)
	if err != nil {
		return nil, false, err
	}

	// Every linked PR is checked against the registry regardless of which
	// repository it lives in — the State section keeps cross-repository
	// links rather than dropping them, and a fork PR's environment can
	// already exist. Only a same-repository PR is a branch-derivation
	// candidate below: a fork's head cannot be resolved (or checked out)
	// from here.
	var linkedPRs []int
	for _, ref := range issue.LinkedPRs {
		if env := st.ByRef(refURL(target.KindPR, ref)); env != nil {
			st.Link(env, []string{issueURL}, nil)
			return env, false, nil
		}
		if ref.In(t.Owner, t.Repo) {
			linkedPRs = append(linkedPRs, ref.Number)
		}
	}

	repoPath, err := e.repoForTarget(t, opts.AttachOnly)
	if err != nil {
		return nil, false, err
	}

	sp := spec{repoPath: repoPath, owner: t.Owner, branch: opts.Branch, issues: []string{issueURL}}
	if sp.branch == "" && len(linkedPRs) > 0 {
		// The highest-numbered linked PR is the one being worked on; its
		// branch (or fork materialisation) is the issue's branch too.
		n := slices.Max(linkedPRs)
		pr, err := e.github().PR(t.Owner, t.Repo, n)
		if err != nil {
			return nil, false, err
		}
		sp.prs = []string{target.Target{Kind: target.KindPR, Owner: t.Owner, Repo: t.Repo, Number: n}.URL()}
		sp.branch, sp.forkPR, sp.fetch = prBranch(pr)
	}
	if sp.branch == "" {
		sp.branch = naming.BranchForIssue(t.Number, issue.Title)
	}
	return e.finish(st, opts, sp, t)
}

// resolvePR implements the design spec's PR-URL resolution: registry by
// URL, then --branch or the PR's head (a fork PR materialises pr-<n>), then
// linking any closing issues once the environment is known.
func (e *Env) resolvePR(st *state.Store, opts OpenOptions) (*state.Env, bool, error) {
	t := opts.Target
	prURL := t.URL()
	if env := st.ByRef(prURL); env != nil {
		return env, false, nil
	}

	pr, err := e.github().PR(t.Owner, t.Repo, t.Number)
	if err != nil {
		return nil, false, err
	}
	repoPath, err := e.repoForTarget(t, opts.AttachOnly)
	if err != nil {
		return nil, false, err
	}

	sp := spec{repoPath: repoPath, owner: t.Owner, branch: opts.Branch, prs: []string{prURL}}
	if sp.branch == "" {
		sp.branch, sp.forkPR, sp.fetch = prBranch(pr)
	}
	// Every closing issue is recorded, regardless of repository (see the
	// same note in resolveIssue) — state.Link skips any that already
	// belong to a different environment.
	for _, ref := range pr.LinkedIssues {
		sp.issues = append(sp.issues, refURL(target.KindIssue, ref))
	}
	return e.finish(st, opts, sp, t)
}

// resolveRepo implements the design spec's repository-URL resolution:
// locate the repository (cwd, projects_path, else clone), then --branch or
// the default branch.
func (e *Env) resolveRepo(st *state.Store, opts OpenOptions) (*state.Env, bool, error) {
	t := opts.Target
	repoPath, err := e.repoForTarget(t, opts.AttachOnly)
	if err != nil {
		return nil, false, err
	}
	branch := opts.Branch
	if branch == "" {
		branch, err = e.git().DefaultBranch(repoPath)
		if err != nil {
			return nil, false, err
		}
	}
	sp := spec{repoPath: repoPath, owner: t.Owner, branch: branch}
	return e.finish(st, opts, sp, t)
}

// resolvePlain implements the design spec's plain-string resolution: an id,
// then a session name, then a branch within the repository of the cwd (or
// --repo). With no explicit --repo, a unique match anywhere is also a hit;
// an explicit --repo scopes the search to that repository — the same flag
// means the same thing here as it does for delete. open creates on that
// branch (or --branch) in the cwd's or --repo's repository.
func (e *Env) resolvePlain(st *state.Store, opts OpenOptions) (*state.Env, bool, error) {
	raw := opts.Target.Name
	if id, err := strconv.Atoi(raw); err == nil {
		if env := st.ByID(id); env != nil {
			return env, false, nil
		}
	}
	if env := st.BySession(raw); env != nil {
		return env, false, nil
	}

	repoPath, repoErr := e.repoFromFlag(opts.Repo)
	if repoPath != "" {
		if env := st.ByBranch(repoPath, raw); env != nil {
			return env, false, nil
		}
	}
	// An explicit --repo that turned out invalid is reported now, before
	// AttachOnly gets a chance to report a generic "not found" instead and
	// bury why: the bad --repo value is the actual problem here, on both
	// open and attach.
	if opts.Repo != "" && repoErr != nil {
		return nil, false, repoErr
	}

	if opts.Repo == "" {
		matches := st.Matching(func(x *state.Env) bool { return x.Branch == raw })
		switch len(matches) {
		case 1:
			return matches[0], false, nil
		case 0:
			// fall through to attach-error or create
		default:
			return nil, false, ambiguous(raw, matches)
		}
	}

	if opts.AttachOnly {
		return nil, false, notFound(fmt.Sprintf("%q", raw))
	}
	// A string that is all digits can only be a stale id here — every id
	// lookup above already came up empty — never a branch worth creating,
	// unless --branch says the digits really are meant as a branch name.
	if opts.Branch == "" && allDigits(raw) {
		return nil, false, fmt.Errorf("no work environment has id %s", raw)
	}
	if repoPath == "" {
		return nil, false, fmt.Errorf("not inside a repository: pass --repo")
	}
	branch := opts.Branch
	if branch == "" {
		branch = raw
	}
	owner, _, _ := e.git().OriginGitHubRepo(repoPath)
	sp := spec{repoPath: repoPath, owner: owner, branch: branch}
	return e.finish(st, opts, sp, opts.Target)
}

// allDigits reports whether s is non-empty and consists only of the digits
// 0-9 — deliberately not strconv.Atoi's notion of "parses as an int", which
// would also accept a leading sign and silently reject a numeral too large
// to fit an int; the check here is about what the string looks like, not
// whether it happens to convert.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func ambiguous(raw string, matches []*state.Env) error {
	var ids []string
	for _, m := range matches {
		ids = append(ids, strconv.Itoa(m.ID))
	}
	return fmt.Errorf("branch %q exists in multiple environments (%s); pass --repo", raw, strings.Join(ids, ", "))
}

// lookupRegistry finds an environment for t using the registry only —
// delete and show never query GitHub or clone. For an issue, PR or
// repository-URL target, repo is ignored (the target carries its own
// repository). For a plain id, session or branch, repo behaves like
// --repo: it scopes a branch lookup and, when given explicitly, disables
// the global unique-branch fallback (an explicit --repo that names no
// matching environment is simply "not found" there, not a reason to look
// elsewhere).
func (e *Env) lookupRegistry(st *state.Store, t target.Target, repo string) (*state.Env, error) {
	switch t.Kind {
	case target.KindIssue, target.KindPR, target.KindRepo:
		return st.ByRef(t.URL()), nil
	}
	raw := t.Name
	if id, err := strconv.Atoi(raw); err == nil {
		if env := st.ByID(id); env != nil {
			return env, nil
		}
	}
	if env := st.BySession(raw); env != nil {
		return env, nil
	}

	repoPath, repoErr := e.repoFromFlag(repo)
	if repoPath != "" {
		if env := st.ByBranch(repoPath, raw); env != nil {
			return env, nil
		}
	}
	if repo != "" {
		if repoErr != nil {
			return nil, repoErr
		}
		return nil, nil
	}

	matches := st.Matching(func(x *state.Env) bool { return x.Branch == raw })
	switch len(matches) {
	case 0:
		return nil, nil
	case 1:
		return matches[0], nil
	default:
		return nil, ambiguous(raw, matches)
	}
}

// finish resolves a fully specified target: an environment already on that
// branch (registry, then a git worktree already checked out on it, found by
// path in case of drift) is reused and linked; otherwise a new one is
// created, unless opts.AttachOnly.
func (e *Env) finish(st *state.Store, opts OpenOptions, sp spec, t target.Target) (*state.Env, bool, error) {
	if env := st.ByBranch(sp.repoPath, sp.branch); env != nil {
		st.Link(env, sp.issues, sp.prs)
		return env, false, nil
	}
	existingPath, hasExisting := e.git().WorktreeForBranch(sp.repoPath, sp.branch)
	if hasExisting {
		if env := st.ByWorktree(existingPath); env != nil {
			env.Branch = sp.branch
			st.Link(env, sp.issues, sp.prs)
			return env, false, nil
		}
	}
	if opts.AttachOnly {
		return nil, false, notFound(t.String())
	}
	env, err := e.create(st, opts, sp, existingPath)
	return env, err == nil, err
}

// repoForTarget locates the repository for a GitHub target: the repository
// containing the cwd when its origin matches, then <projects_path>/<repo>,
// then (unless attachOnly) a fresh normal clone.
func (e *Env) repoForTarget(t target.Target, attachOnly bool) (string, error) {
	if root := e.git().RepoRoot(e.Cwd); root != "" {
		if o, r, ok := e.git().OriginGitHubRepo(root); ok && strings.EqualFold(o, t.Owner) && strings.EqualFold(r, t.Repo) {
			return root, nil
		}
	}
	if dir, ok := gitx.FindProjectDir(e.Cfg.ProjectsPath, t.Repo); ok {
		return dir, nil
	}
	if attachOnly {
		return "", notFound(t.String())
	}
	dest := filepath.Join(e.Cfg.ProjectsPath, t.Repo)
	if err := e.git().Clone(t.Owner+"/"+t.Repo, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// repoFromFlag resolves --repo (or, when empty, the cwd) to a repository
// path. An empty result with a nil error means neither applies — a
// legitimate outcome for a plain-name lookup that does not need a
// repository; the caller decides whether that is fatal.
func (e *Env) repoFromFlag(repo string) (string, error) {
	if repo == "" {
		return e.git().RepoRoot(e.Cwd), nil
	}
	if strings.ContainsAny(repo, "/\\") || strings.HasPrefix(repo, "~") {
		root := e.git().RepoRoot(expandHome(repo))
		if root == "" {
			return "", fmt.Errorf("%s is not a git repository", repo)
		}
		return root, nil
	}
	dir, ok := gitx.FindProjectDir(e.Cfg.ProjectsPath, repo)
	if !ok {
		return "", fmt.Errorf("repository %q not found in %s", repo, e.Cfg.ProjectsPath)
	}
	return dir, nil
}

// prBranch picks the local branch for a PR: its head branch for a same-repo
// PR (fetched into origin/<head> if needed), or pr-N materialised from
// refs/pull/N/head for a fork PR, whose head branch is not on origin.
func prBranch(pr gh.PR) (branch string, forkPR int, fetch bool) {
	if pr.IsCrossRepository {
		return naming.PRBranch(pr.Number), pr.Number, false
	}
	return pr.HeadRefName, 0, true
}

// refURL is the canonical URL for a linked reference: gh already reports it
// in the same form target.Target.URL() produces, so it is used directly
// when present; a ref missing it (never true of real gh output, only of
// bare test fixtures) falls back to constructing it from the ref's own
// repository — never the caller's owner/repo, since a link can point at a
// different repository (a fork PR closing an issue, for instance) — so the
// two always agree with what a later `we open <url>` would look up.
func refURL(kind target.Kind, ref gh.Ref) string {
	if ref.URL != "" {
		return ref.URL
	}
	return target.Target{Kind: kind, Owner: ref.Repository.Owner.Login, Repo: ref.Repository.Name, Number: ref.Number}.URL()
}
