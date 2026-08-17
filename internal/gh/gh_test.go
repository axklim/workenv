package gh

import (
	"testing"

	"workenv/internal/execx"
)

func TestIssue(t *testing.T) {
	f := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "gh issue view 59", Out: `{"number":59,"title":"Review CLAUDE.md file",` +
			`"closedByPullRequestsReferences":[{"number":61,"url":"https://github.com/axklim/trade/pull/61",` +
			`"repository":{"name":"trade","owner":{"login":"axklim"}}}]}`},
	}}
	issue, err := Client{R: f}.Issue("axklim", "trade", 59)
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}
	if issue.Number != 59 || issue.Title != "Review CLAUDE.md file" {
		t.Errorf("issue = %+v", issue)
	}
	if len(issue.LinkedPRs) != 1 || issue.LinkedPRs[0].Number != 61 || !issue.LinkedPRs[0].In("axklim", "trade") {
		t.Errorf("LinkedPRs = %+v", issue.LinkedPRs)
	}
	if issue.LinkedPRs[0].URL != "https://github.com/axklim/trade/pull/61" {
		t.Errorf("LinkedPRs[0].URL = %q", issue.LinkedPRs[0].URL)
	}
	if got := f.Joined()[0]; got != "gh issue view 59 -R axklim/trade --json number,title,closedByPullRequestsReferences" {
		t.Errorf("command = %q", got)
	}
}

func TestPR(t *testing.T) {
	f := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "gh pr view 61", Out: `{"number":61,"title":"docs: CLAUDE.md","headRefName":"review_claude-file",` +
			`"isCrossRepository":false,"closingIssuesReferences":[{"number":59,"url":"https://github.com/axklim/trade/issues/59",` +
			`"repository":{"name":"trade","owner":{"login":"axklim"}}}]}`},
	}}
	pr, err := Client{R: f}.PR("axklim", "trade", 61)
	if err != nil {
		t.Fatalf("PR error: %v", err)
	}
	if pr.Number != 61 || pr.HeadRefName != "review_claude-file" || pr.IsCrossRepository {
		t.Errorf("pr = %+v", pr)
	}
	if len(pr.LinkedIssues) != 1 || pr.LinkedIssues[0].Number != 59 {
		t.Errorf("LinkedIssues = %+v", pr.LinkedIssues)
	}
	if pr.LinkedIssues[0].URL != "https://github.com/axklim/trade/issues/59" {
		t.Errorf("LinkedIssues[0].URL = %q", pr.LinkedIssues[0].URL)
	}
	if got := f.Joined()[0]; got != "gh pr view 61 -R axklim/trade --json number,title,headRefName,isCrossRepository,closingIssuesReferences" {
		t.Errorf("command = %q", got)
	}
}

func TestRefInIgnoresCaseAndOtherRepos(t *testing.T) {
	var r Ref
	r.Number = 7
	r.Repository.Name = "Trade"
	r.Repository.Owner.Login = "AxKlim"
	if !r.In("axklim", "trade") {
		t.Error("In should be case-insensitive")
	}
	if r.In("axklim", "other") {
		t.Error("In must reject another repository")
	}
}
