// Package target classifies what the user asked to create a work
// environment for: a GitHub issue link, a GitHub PR link, a bare repository
// link, or a plain name.
package target

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type Kind int

const (
	KindName Kind = iota
	KindIssue
	KindPR
	KindRepo
)

type Target struct {
	Kind   Kind
	Owner  string
	Repo   string
	Number int
	Name   string
}

var githubIssuePath = regexp.MustCompile(`^github\.com/([^/]+)/([^/]+)/(issues|pull)/([^/?#]+)`)
var githubRepoPath = regexp.MustCompile(`^github\.com/([^/]+)/([^/]+)$`)

// Parse recognizes GitHub issue/PR/repository URLs; anything that is not a
// github.com URL is treated as a plain work-environment name (typically a
// branch name).
func Parse(s string) (Target, error) {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	if m := githubIssuePath.FindStringSubmatch(trimmed); m != nil {
		num, err := strconv.Atoi(m[4])
		if err != nil {
			return Target{}, fmt.Errorf("invalid issue/PR number in %s", s)
		}
		kind := KindIssue
		if m[3] == "pull" {
			kind = KindPR
		}
		return Target{Kind: kind, Owner: m[1], Repo: m[2], Number: num}, nil
	}
	if owner, repo, ok := parseRepoPath(trimmed); ok {
		return Target{Kind: KindRepo, Owner: owner, Repo: repo}, nil
	}
	if strings.Contains(trimmed, "github.com/") {
		return Target{}, fmt.Errorf("unrecognized github URL (expected .../issues/N, .../pull/N, or a repository URL): %s", s)
	}
	return Target{Kind: KindName, Name: s}, nil
}

// parseRepoPath recognizes a bare "github.com/<owner>/<repo>" path, after
// stripping an optional query/fragment, trailing slash and ".git" suffix.
func parseRepoPath(trimmed string) (owner, repo string, ok bool) {
	path := trimmed
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	path = strings.TrimSuffix(path, "/")
	path = strings.TrimSuffix(path, ".git")
	m := githubRepoPath.FindStringSubmatch(path)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// URL renders the canonical GitHub URL for t: always https, never a
// trailing slash, never a ".git" suffix. "" for KindName, which has no URL.
func (t Target) URL() string {
	switch t.Kind {
	case KindIssue:
		return fmt.Sprintf("https://github.com/%s/%s/issues/%d", t.Owner, t.Repo, t.Number)
	case KindPR:
		return fmt.Sprintf("https://github.com/%s/%s/pull/%d", t.Owner, t.Repo, t.Number)
	case KindRepo:
		return fmt.Sprintf("https://github.com/%s/%s", t.Owner, t.Repo)
	default:
		return ""
	}
}

// String renders t for humans: "owner/repo#N" for an issue, "owner/repo PR
// #N" for a PR, "owner/repo" for a bare repository, or the quoted name.
func (t Target) String() string {
	switch t.Kind {
	case KindIssue:
		return fmt.Sprintf("%s/%s#%d", t.Owner, t.Repo, t.Number)
	case KindPR:
		return fmt.Sprintf("%s/%s PR #%d", t.Owner, t.Repo, t.Number)
	case KindRepo:
		return fmt.Sprintf("%s/%s", t.Owner, t.Repo)
	default:
		return fmt.Sprintf("%q", t.Name)
	}
}
