package naming

import (
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Add Kafka publisher", "add-kafka-publisher"},
		{"Fix: crash on empty input!!", "fix-crash-on-empty-input"},
		{"  spaces  everywhere  ", "spaces-everywhere"},
		{"ALL CAPS", "all-caps"},
		{"unicode ümläut ß", "unicode-ml-ut"},
		{"", ""},
		{"---", ""},
		{"a very long title that should definitely be truncated somewhere sane", "a-very-long-title-that-should-definitely"},
	}
	for _, tt := range tests {
		if got := Slugify(tt.in); got != tt.want {
			t.Errorf("Slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSessionName(t *testing.T) {
	tests := []struct {
		project, branch string
		want            string
	}{
		{"trade", "review_claude-file", "trade-review_claude-file"},
		{"trade", "feat/static-grid", "trade-feat-static-grid"},
		{"my.repo", "fix:thing", "my-repo-fix-thing"},
	}
	for _, tt := range tests {
		got := SessionName(tt.project, tt.branch)
		if got != tt.want {
			t.Errorf("SessionName(%q, %q) = %q, want %q", tt.project, tt.branch, got, tt.want)
		}
		if strings.HasPrefix(got, "we-") {
			t.Errorf("SessionName(%q, %q) = %q, must not start with we-", tt.project, tt.branch, got)
		}
	}
}

func TestBranchForIssueUsesTitleSlugWithoutPrefix(t *testing.T) {
	tests := []struct {
		num   int
		title string
		want  string
	}{
		{123, "Add Kafka publisher", "add-kafka-publisher"},
		{59, "Review CLAUDE.md file", "review-claude-md-file"},
		{7, "", "issue-7"},
		{42, "!!!", "issue-42"},
	}
	for _, tt := range tests {
		if got := BranchForIssue(tt.num, tt.title); got != tt.want {
			t.Errorf("BranchForIssue(%d, %q) = %q, want %q", tt.num, tt.title, got, tt.want)
		}
	}
}

func TestSanitize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"feature/login", "feature-login"},
		{"fix: crash", "fix-crash"},
		{"plain-name_ok", "plain-name_ok"},
	}
	for _, tt := range tests {
		if got := Sanitize(tt.in); got != tt.want {
			t.Errorf("Sanitize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPRBranch(t *testing.T) {
	if got := PRBranch(456); got != "pr-456" {
		t.Errorf("PRBranch(456) = %q, want %q", got, "pr-456")
	}
}
