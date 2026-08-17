// Package gitx wraps the git operations workenv needs: locating project
// repositories from any layout (normal clone, bare container, worktree),
// cloning missing ones normally, and managing worktrees.
package gitx

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"workenv/internal/execx"
)

type Git struct {
	R execx.Runner
}

type Worktree struct {
	Path   string
	Branch string
}

// FindProjectDir looks for a project repository (normal or bare clone)
// under projectsDir, checking <name> and <name>.git.
func FindProjectDir(projectsDir, name string) (string, bool) {
	for _, candidate := range []string{
		filepath.Join(projectsDir, name),
		filepath.Join(projectsDir, name+".git"),
	} {
		if isRepoDir(candidate) {
			return candidate, true
		}
	}
	return "", false
}

func isRepoDir(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return true
	}
	// Bare repository: HEAD sits directly in the directory.
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err == nil {
		return true
	}
	return false
}

// RepoRoot returns the repository containing dir, whatever the layout: the
// parent of a ".git" (or ".bare") common dir — a normal clone or a bare
// container, seen from the checkout, a worktree or the container itself —
// or the common dir for a bare "repo.git". "" when dir is not in a repo.
func (g Git) RepoRoot(dir string) string {
	out, err := g.R.Output(dir, "git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil || out == "" {
		return ""
	}
	common := filepath.Clean(out)
	if base := filepath.Base(common); base == ".git" || base == ".bare" {
		return filepath.Dir(common)
	}
	return common
}

// CurrentBranch returns the branch checked out in worktree, "" when HEAD is
// detached or the directory is not a checkout.
func (g Git) CurrentBranch(worktree string) string {
	out, err := g.R.Output(worktree, "git", "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return out
}

// Clone clones ownerRepo into dest via gh (which honors the user's
// preferred git protocol). A normal clone already has the standard fetch
// refspec and origin/HEAD, so nothing further is needed.
func (g Git) Clone(ownerRepo, dest string) error {
	return g.R.Run("", "gh", "repo", "clone", ownerRepo, dest)
}

// ProjectName is the display name for the repository at repoPath: the
// GitHub repository name when origin points at GitHub, else repoPath's
// basename with any ".git" suffix removed.
func (g Git) ProjectName(repoPath string) string {
	if _, repo, ok := g.OriginGitHubRepo(repoPath); ok {
		return repo
	}
	return strings.TrimSuffix(filepath.Base(repoPath), ".git")
}

// DefaultBranch resolves the branch new work should start from:
// origin/HEAD when a remote exists, the repo's own HEAD otherwise.
func (g Git) DefaultBranch(repoDir string) (string, error) {
	if out, err := g.R.Output(repoDir, "git", "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimPrefix(out, "origin/"), nil
	}
	if out, err := g.R.Output(repoDir, "git", "symbolic-ref", "--short", "HEAD"); err == nil {
		return out, nil
	}
	return "", fmt.Errorf("cannot determine default branch of %s", repoDir)
}

func (g Git) ListWorktrees(repoDir string) ([]Worktree, error) {
	out, err := g.R.Output(repoDir, "git", "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	return parseWorktrees(out), nil
}

// WorktreeForBranch returns the existing worktree checked out on branch.
func (g Git) WorktreeForBranch(repoDir, branch string) (string, bool) {
	wts, err := g.ListWorktrees(repoDir)
	if err != nil {
		return "", false
	}
	for _, wt := range wts {
		if wt.Branch == branch {
			return wt.Path, true
		}
	}
	return "", false
}

// BranchExists reports whether ref (fully qualified) exists.
func (g Git) BranchExists(repoDir, ref string) bool {
	_, err := g.R.Output(repoDir, "git", "show-ref", "--verify", "--quiet", ref)
	return err == nil
}

// AddWorktree creates a worktree at path for branch, creating the branch
// from origin/<branch> or the default branch when it does not exist yet.
func (g Git) AddWorktree(repoDir, path, branch string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	switch {
	case g.BranchExists(repoDir, "refs/heads/"+branch):
		return g.R.Run(repoDir, "git", "worktree", "add", path, branch)
	case g.BranchExists(repoDir, "refs/remotes/origin/"+branch):
		return g.R.Run(repoDir, "git", "worktree", "add", "--track", "-b", branch, path, "origin/"+branch)
	default:
		base, err := g.DefaultBranch(repoDir)
		if err != nil {
			return err
		}
		start := base
		if g.BranchExists(repoDir, "refs/remotes/origin/"+base) {
			start = "origin/" + base
		}
		return g.R.Run(repoDir, "git", "worktree", "add", "-b", branch, path, start)
	}
}

// FetchPRBranch materializes a PR head as local branch, unless it exists.
func (g Git) FetchPRBranch(repoDir string, prNumber int, branch string) error {
	if g.BranchExists(repoDir, "refs/heads/"+branch) {
		return nil
	}
	return g.R.Run(repoDir, "git", "fetch", "origin",
		fmt.Sprintf("pull/%d/head:refs/heads/%s", prNumber, branch))
}

// EnsureOriginBranch makes refs/remotes/origin/<branch> available for a
// branch that exists on origin but was pushed after the last fetch. The
// explicit refspec also works in bare repos that have no fetch refspec.
func (g Git) EnsureOriginBranch(repoDir, branch string) error {
	if g.BranchExists(repoDir, "refs/heads/"+branch) || g.BranchExists(repoDir, "refs/remotes/origin/"+branch) {
		return nil
	}
	return g.R.Run(repoDir, "git", "fetch", "origin",
		fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", branch, branch))
}

// Prune forgets worktrees whose directories are gone.
func (g Git) Prune(repoDir string) error {
	return g.R.Run(repoDir, "git", "worktree", "prune")
}

func (g Git) RemoveWorktree(repoDir, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	if err := g.R.Run(repoDir, "git", args...); err != nil {
		return err
	}
	return g.Prune(repoDir)
}

func (g Git) DeleteBranch(repoDir, branch string) error {
	return g.R.Run(repoDir, "git", "branch", "-D", branch)
}

// OriginGitHubRepo reports the owner/repo of the origin remote when it
// points at github.com.
func (g Git) OriginGitHubRepo(repoDir string) (owner, repo string, ok bool) {
	out, err := g.R.Output(repoDir, "git", "remote", "get-url", "origin")
	if err != nil {
		return "", "", false
	}
	return parseGitHubRemote(out)
}

func parseWorktrees(porcelain string) []Worktree {
	var wts []Worktree
	var cur *Worktree
	flush := func() {
		if cur != nil {
			wts = append(wts, *cur)
			cur = nil
		}
	}
	for _, line := range strings.Split(porcelain, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur = &Worktree{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "branch ") && cur != nil:
			cur.Branch = strings.TrimPrefix(strings.TrimPrefix(line, "branch "), "refs/heads/")
		}
	}
	flush()
	return wts
}

var githubRemote = regexp.MustCompile(`^(?:git@github\.com:|(?:https://|ssh://git@)github\.com/)([^/]+)/([^/]+?)(?:\.git)?$`)

func parseGitHubRemote(url string) (owner, repo string, ok bool) {
	m := githubRemote.FindStringSubmatch(strings.TrimSpace(url))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}
