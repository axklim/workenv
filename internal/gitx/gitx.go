// Package gitx wraps the git operations workenv needs: locating project
// repositories, bare-cloning missing ones (with fetch-refspec setup so
// origin-tracking branches work), and managing worktrees.
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

// TopLevel returns the repository root containing dir, or "" if dir is not
// inside a work tree.
func (g Git) TopLevel(dir string) string {
	out, err := g.R.Output(dir, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return out
}

// CloneBare clones owner/repo bare into dest via gh (which honors the
// user's preferred git protocol) and sets up refs so that origin-tracking
// branches behave like in a normal clone.
func (g Git) CloneBare(ownerRepo, dest string) error {
	if err := g.R.Run("", "gh", "repo", "clone", ownerRepo, dest, "--", "--bare"); err != nil {
		return err
	}
	if _, err := g.R.Output(dest, "git", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return err
	}
	if err := g.R.Run(dest, "git", "fetch", "origin"); err != nil {
		return err
	}
	if _, err := g.R.Output(dest, "git", "remote", "set-head", "origin", "--auto"); err != nil {
		return err
	}
	return nil
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

func (g Git) RemoveWorktree(repoDir, path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	if err := g.R.Run(repoDir, "git", args...); err != nil {
		return err
	}
	return g.R.Run(repoDir, "git", "worktree", "prune")
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
