package target

import "testing"

func TestParseIssueURL(t *testing.T) {
	tests := []string{
		"https://github.com/acme/example-service/issues/123",
		"https://github.com/acme/example-service/issues/123/",
		"http://github.com/acme/example-service/issues/123?foo=bar",
		"github.com/acme/example-service/issues/123#issuecomment-1",
	}
	for _, in := range tests {
		got, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", in, err)
		}
		want := Target{Kind: KindIssue, Owner: "acme", Repo: "example-service", Number: 123}
		if got != want {
			t.Errorf("Parse(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestParsePRURL(t *testing.T) {
	tests := []string{
		"https://github.com/acme/example-service/pull/456",
		"https://github.com/acme/example-service/pull/456/files",
		"github.com/acme/example-service/pull/456",
	}
	for _, in := range tests {
		got, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", in, err)
		}
		want := Target{Kind: KindPR, Owner: "acme", Repo: "example-service", Number: 456}
		if got != want {
			t.Errorf("Parse(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestParseRepoURL(t *testing.T) {
	tests := []string{
		"https://github.com/acme/proj",
		"https://github.com/acme/proj.git",
		"https://github.com/acme/proj/",
		"github.com/acme/proj",
	}
	for _, in := range tests {
		got, err := Parse(in)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", in, err)
		}
		want := Target{Kind: KindRepo, Owner: "acme", Repo: "proj"}
		if got != want {
			t.Errorf("Parse(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestURLIsCanonical(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://github.com/acme/proj/issues/123?x=1", "https://github.com/acme/proj/issues/123"},
		{"http://github.com/acme/proj/issues/123/", "https://github.com/acme/proj/issues/123"},
		{"https://github.com/acme/proj/pull/456#discussion", "https://github.com/acme/proj/pull/456"},
		{"github.com/acme/proj/pull/456/files", "https://github.com/acme/proj/pull/456"},
		{"https://github.com/acme/proj.git", "https://github.com/acme/proj"},
		{"https://github.com/acme/proj/", "https://github.com/acme/proj"},
		{"github.com/acme/proj", "https://github.com/acme/proj"},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", tt.in, err)
		}
		if u := got.URL(); u != tt.want {
			t.Errorf("Parse(%q).URL() = %q, want %q", tt.in, u, tt.want)
		}
	}
	name, err := Parse("feature-123")
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if u := name.URL(); u != "" {
		t.Errorf("KindName URL() = %q, want empty", u)
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		t    Target
		want string
	}{
		{Target{Kind: KindIssue, Owner: "axklim", Repo: "trade", Number: 59}, "axklim/trade#59"},
		{Target{Kind: KindPR, Owner: "axklim", Repo: "trade", Number: 61}, "axklim/trade PR #61"},
		{Target{Kind: KindRepo, Owner: "axklim", Repo: "trade"}, "axklim/trade"},
		{Target{Kind: KindName, Name: "feature-123"}, `"feature-123"`},
	}
	for _, tt := range tests {
		if got := tt.t.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
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
	if _, err := Parse("https://github.com/acme/example-service/commit/abc123"); err == nil {
		t.Error("expected error for non-issue/PR github URL, got nil")
	}
	if _, err := Parse("https://github.com/acme/example-service/issues/notanumber"); err == nil {
		t.Error("expected error for non-numeric issue, got nil")
	}
}
