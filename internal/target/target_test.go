package target

import "testing"

func TestParseIssueURL(t *testing.T) {
	tests := []string{
		"https://github.com/acme/borscht/issues/123",
		"https://github.com/acme/borscht/issues/123/",
		"http://github.com/acme/borscht/issues/123?foo=bar",
		"github.com/acme/borscht/issues/123#issuecomment-1",
	}
	for _, in := range tests {
		got, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", in, err)
		}
		want := Target{Kind: KindIssue, Owner: "acme", Repo: "borscht", Number: 123}
		if got != want {
			t.Errorf("Parse(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestParsePRURL(t *testing.T) {
	tests := []string{
		"https://github.com/acme/borscht/pull/456",
		"https://github.com/acme/borscht/pull/456/files",
		"github.com/acme/borscht/pull/456",
	}
	for _, in := range tests {
		got, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", in, err)
		}
		want := Target{Kind: KindPR, Owner: "acme", Repo: "borscht", Number: 456}
		if got != want {
			t.Errorf("Parse(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestParsePlainName(t *testing.T) {
	got, err := Parse("feature-123")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	want := Target{Kind: KindName, Name: "feature-123"}
	if got != want {
		t.Errorf("Parse(feature-123) = %+v, want %+v", got, want)
	}
}

func TestParseRejectsBadGitHubPath(t *testing.T) {
	// A github.com URL that is neither an issue nor a PR must not silently
	// degrade into a branch name.
	if _, err := Parse("https://github.com/acme/borscht/commit/abc123"); err == nil {
		t.Error("expected error for non-issue/PR github URL, got nil")
	}
	if _, err := Parse("https://github.com/acme/borscht/issues/notanumber"); err == nil {
		t.Error("expected error for non-numeric issue, got nil")
	}
}
