// Package gh fetches GitHub issue/PR metadata through the gh CLI, including
// the links between them (a PR "closes" an issue via keywords or the
// Development sidebar; GitHub reports it from both sides).
package gh

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"workenv/internal/execx"
)

type Client struct {
	R execx.Runner
}

// Ref is a linked issue or PR as gh reports it. Links can point at another
// repository, so the repository travels with the number.
type Ref struct {
	Number     int    `json:"number"`
	URL        string `json:"url"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
}

// In reports whether the reference lives in owner/repo (case-insensitively,
// like GitHub itself).
func (r Ref) In(owner, repo string) bool {
	return strings.EqualFold(r.Repository.Owner.Login, owner) && strings.EqualFold(r.Repository.Name, repo)
}

type Issue struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	LinkedPRs []Ref  `json:"closedByPullRequestsReferences"`
}

type PR struct {
	Number            int    `json:"number"`
	Title             string `json:"title"`
	HeadRefName       string `json:"headRefName"`
	IsCrossRepository bool   `json:"isCrossRepository"`
	LinkedIssues      []Ref  `json:"closingIssuesReferences"`
}

func (c Client) Issue(owner, repo string, num int) (Issue, error) {
	out, err := c.R.Output("", "gh", "issue", "view", strconv.Itoa(num),
		"-R", owner+"/"+repo, "--json", "number,title,closedByPullRequestsReferences")
	if err != nil {
		return Issue{}, err
	}
	var issue Issue
	if err := json.Unmarshal([]byte(out), &issue); err != nil {
		return Issue{}, fmt.Errorf("parsing gh issue view output: %w", err)
	}
	return issue, nil
}

func (c Client) PR(owner, repo string, num int) (PR, error) {
	out, err := c.R.Output("", "gh", "pr", "view", strconv.Itoa(num),
		"-R", owner+"/"+repo, "--json", "number,title,headRefName,isCrossRepository,closingIssuesReferences")
	if err != nil {
		return PR{}, err
	}
	var pr PR
	if err := json.Unmarshal([]byte(out), &pr); err != nil {
		return PR{}, fmt.Errorf("parsing gh pr view output: %w", err)
	}
	return pr, nil
}
