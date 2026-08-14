// Package gh fetches GitHub issue/PR metadata through the gh CLI.
package gh

import (
	"encoding/json"
	"fmt"
	"strconv"

	"workenv/internal/execx"
)

type Client struct {
	R execx.Runner
}

type Issue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
}

type PR struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	HeadRefName string `json:"headRefName"`
}

func (c Client) Issue(owner, repo string, num int) (Issue, error) {
	out, err := c.R.Output("", "gh", "issue", "view", strconv.Itoa(num),
		"-R", owner+"/"+repo, "--json", "number,title")
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
		"-R", owner+"/"+repo, "--json", "number,title,headRefName")
	if err != nil {
		return PR{}, err
	}
	var pr PR
	if err := json.Unmarshal([]byte(out), &pr); err != nil {
		return PR{}, fmt.Errorf("parsing gh pr view output: %w", err)
	}
	return pr, nil
}
