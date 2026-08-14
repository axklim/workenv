package naming

import "testing"

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
		project, name string
		want          string
	}{
		{"borscht", "feature-123", "we-borscht-feature-123"},
		{"my.repo", "fix:thing", "we-my-repo-fix-thing"},
		{"proj", "feature/login", "we-proj-feature-login"},
		{"proj", "with spaces", "we-proj-with-spaces"},
	}
	for _, tt := range tests {
		if got := SessionName(tt.project, tt.name); got != tt.want {
			t.Errorf("SessionName(%q, %q) = %q, want %q", tt.project, tt.name, got, tt.want)
		}
	}
}

func TestBranchForIssue(t *testing.T) {
	tests := []struct {
		num   int
		title string
		want  string
	}{
		{123, "Add Kafka publisher", "issue-123-add-kafka-publisher"},
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

func TestPRName(t *testing.T) {
	if got := PRName(456); got != "pr-456" {
		t.Errorf("PRName(456) = %q, want %q", got, "pr-456")
	}
}
