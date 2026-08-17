package wtpath

import (
	"path/filepath"
	"testing"
)

func TestPlacement(t *testing.T) {
	base := Vars{RepoPath: "/Users/u/projects/trade", Repo: "trade", Project: "trade", Owner: "axklim", Branch: "feat/x"}

	tests := []struct {
		name string
		tmpl string
		vars Vars
		want func(home string) string
	}{
		{
			name: "TestDefaultTemplateRendersSibling",
			tmpl: Default,
			vars: base,
			want: func(home string) string { return "/Users/u/projects/trade.feat-x" },
		},
		{
			name: "TestCentralisedRoot",
			tmpl: "~/worktrees/{{ .project }}/{{ .branch | sanitize }}",
			vars: base,
			want: func(home string) string { return filepath.Join(home, "worktrees/trade/feat-x") },
		},
		{
			name: "TestRelativeResolvesAgainstRepoPath",
			tmpl: ".worktrees/{{ .branch | sanitize }}",
			vars: base,
			want: func(home string) string { return "/Users/u/projects/trade/.worktrees/feat-x" },
		},
		{
			name: "TestConditional",
			tmpl: `{{ .repo_path }}/../{{ if eq .repo ".git" }}x{{ else }}{{ .repo }}{{ end }}`,
			vars: base,
			want: func(home string) string { return "/Users/u/projects/trade" },
		},
		{
			name: "TestOwnerAndUnsanitizedBranch",
			tmpl: "~/src/{{ .owner }}/{{ .repo }}/{{ .branch }}",
			vars: base,
			want: func(home string) string { return filepath.Join(home, "src/axklim/trade/feat/x") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set HOME even for cases that don't use ~: harmless, and keeps
			// every subtest independent of the developer's real home.
			home := t.TempDir()
			t.Setenv("HOME", home)

			got, err := Render(tt.tmpl, tt.vars)
			if err != nil {
				t.Fatalf("Render(%q) error: %v", tt.tmpl, err)
			}
			if want := tt.want(home); got != want {
				t.Errorf("Render(%q) = %q, want %q", tt.tmpl, got, want)
			}
		})
	}
}

func TestUnknownVariableIsAnError(t *testing.T) {
	if _, err := Render("{{ .nope }}/x", Vars{RepoPath: "/r", Repo: "r"}); err == nil {
		t.Error("expected an error for an unknown variable")
	}
}

func TestMalformedTemplateIsAnError(t *testing.T) {
	if _, err := Render("{{ .repo_path", Vars{RepoPath: "/r"}); err == nil {
		t.Error("expected a parse error")
	}
}
