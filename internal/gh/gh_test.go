package gh

import (
	"testing"

	"workenv/internal/execx"
)

func TestIssue(t *testing.T) {
	f := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "gh issue view 123", Out: `{"number":123,"title":"Add Kafka publisher"}`},
	}}
	issue, err := Client{R: f}.Issue("acme", "example-service", 123)
	if err != nil {
		t.Fatalf("Issue error: %v", err)
	}
	if issue.Number != 123 || issue.Title != "Add Kafka publisher" {
		t.Errorf("issue = %+v", issue)
	}
	if got := f.Joined()[0]; got != "gh issue view 123 -R acme/example-service --json number,title" {
		t.Errorf("command = %q", got)
	}
}

func TestPR(t *testing.T) {
	f := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "gh pr view 456", Out: `{"number":456,"title":"Fix crash","headRefName":"fix/crash"}`},
	}}
	pr, err := Client{R: f}.PR("acme", "example-service", 456)
	if err != nil {
		t.Fatalf("PR error: %v", err)
	}
	if pr.Number != 456 || pr.HeadRefName != "fix/crash" {
		t.Errorf("pr = %+v", pr)
	}
}
