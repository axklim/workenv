// Package target classifies what the user asked to create a work
// environment for: a GitHub issue link, a GitHub PR link, or a plain name.
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
)

type Target struct {
	Kind   Kind
	Owner  string
	Repo   string
	Number int
	Name   string
}

var githubPath = regexp.MustCompile(`^github\.com/([^/]+)/([^/]+)/(issues|pull)/([^/?#]+)`)

// Parse recognizes GitHub issue/PR URLs; anything that is not a github.com
// URL is treated as a plain work-environment name (typically a branch name).
func Parse(s string) (Target, error) {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://")
	m := githubPath.FindStringSubmatch(trimmed)
	if m == nil {
		if strings.Contains(trimmed, "github.com/") {
			return Target{}, fmt.Errorf("unrecognized github URL (expected .../issues/N or .../pull/N): %s", s)
		}
		return Target{Kind: KindName, Name: s}, nil
	}
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
