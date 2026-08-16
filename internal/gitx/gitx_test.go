package gitx

import "testing"

const porcelain = `worktree /Users/u/projects/example-service
HEAD abcdefabcdefabcdefabcdefabcdefabcdefabcd
branch refs/heads/main

worktree /Users/u/projects/.we/example-service/feature-x
HEAD 1234561234561234561234561234561234561234
branch refs/heads/feature-x

worktree /Users/u/projects/.we/example-service/detached
HEAD 9999999999999999999999999999999999999999
detached
`

func TestParseWorktrees(t *testing.T) {
	wts := parseWorktrees(porcelain)
	if len(wts) != 3 {
		t.Fatalf("got %d worktrees, want 3: %+v", len(wts), wts)
	}
	if wts[0].Path != "/Users/u/projects/example-service" || wts[0].Branch != "main" {
		t.Errorf("wts[0] = %+v", wts[0])
	}
	if wts[1].Branch != "feature-x" {
		t.Errorf("wts[1] = %+v", wts[1])
	}
	if wts[2].Branch != "" {
		t.Errorf("detached worktree should have empty branch, got %+v", wts[2])
	}
}

func TestParseGitHubRemote(t *testing.T) {
	tests := []struct {
		url         string
		owner, repo string
	}{
		{"git@github.com:acme/example-service.git", "acme", "example-service"},
		{"https://github.com/acme/example-service.git", "acme", "example-service"},
		{"https://github.com/acme/example-service", "acme", "example-service"},
		{"ssh://git@github.com/acme/example-service.git", "acme", "example-service"},
	}
	for _, tt := range tests {
		owner, repo, ok := parseGitHubRemote(tt.url)
		if !ok || owner != tt.owner || repo != tt.repo {
			t.Errorf("parseGitHubRemote(%q) = %q, %q, %v", tt.url, owner, repo, ok)
		}
	}
	if _, _, ok := parseGitHubRemote("https://gitlab.com/acme/example-service.git"); ok {
		t.Error("non-github remote should not parse")
	}
}
