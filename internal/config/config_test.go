package config

import (
	"os"
	"path/filepath"
	"testing"

	"workenv/internal/wtpath"
)

func TestDefaults(t *testing.T) {
	home := t.TempDir()
	cfg, err := parse("", home)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if want := filepath.Join(home, "projects"); cfg.ProjectsPath != want {
		t.Errorf("ProjectsPath = %q, want %q", cfg.ProjectsPath, want)
	}
	if cfg.WorktreePath != wtpath.Default {
		t.Errorf("WorktreePath = %q, want %q (default template)", cfg.WorktreePath, wtpath.Default)
	}
	if cfg.ClaudeCmd != "claude" {
		t.Errorf("ClaudeCmd = %q, want claude", cfg.ClaudeCmd)
	}
	if cfg.RemoteWe != "we" {
		t.Errorf("RemoteWe = %q, want we", cfg.RemoteWe)
	}
	if cfg.ZedCmd != "zed" {
		t.Errorf("ZedCmd = %q, want zed", cfg.ZedCmd)
	}
}

func TestParseOverrides(t *testing.T) {
	home := "/home/u"
	raw := `
# comment
projects_path = "~/src"
worktree_path = "~/worktrees/{{ .project }}/{{ .branch | sanitize }}"
claude_cmd = "claude --dangerously-skip-permissions"
remote_we = "/usr/local/bin/we"
zed_cmd = "/usr/local/bin/zed"
`
	cfg, err := parse(raw, home)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if cfg.ProjectsPath != "/home/u/src" {
		t.Errorf("ProjectsPath = %q, want /home/u/src (tilde expanded)", cfg.ProjectsPath)
	}
	if cfg.WorktreePath != "~/worktrees/{{ .project }}/{{ .branch | sanitize }}" {
		t.Errorf("WorktreePath = %q, want verbatim template (no tilde expansion)", cfg.WorktreePath)
	}
	if cfg.ClaudeCmd != "claude --dangerously-skip-permissions" {
		t.Errorf("ClaudeCmd = %q", cfg.ClaudeCmd)
	}
	if cfg.RemoteWe != "/usr/local/bin/we" {
		t.Errorf("RemoteWe = %q", cfg.RemoteWe)
	}
	if cfg.ZedCmd != "/usr/local/bin/zed" {
		t.Errorf("ZedCmd = %q", cfg.ZedCmd)
	}
}

func TestEmptyPathsFallBackToDefaults(t *testing.T) {
	home := "/home/u"
	raw := `
projects_path = ""
worktree_path = ""
`
	cfg, err := parse(raw, home)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if want := filepath.Join(home, "projects"); cfg.ProjectsPath != want {
		t.Errorf("empty projects_path: ProjectsPath = %q, want %q (default)", cfg.ProjectsPath, want)
	}
	if cfg.WorktreePath != wtpath.Default {
		t.Errorf("empty worktree_path: WorktreePath = %q, want %q (default)", cfg.WorktreePath, wtpath.Default)
	}
}

func TestParseRejectsRetiredKeys(t *testing.T) {
	if _, err := parse(`projects_dir = "x"`, "/home/u"); err == nil {
		t.Error("expected error for retired projects_dir key, got nil")
	}
	if _, err := parse(`worktrees_dir = "x"`, "/home/u"); err == nil {
		t.Error("expected error for retired worktrees_dir key, got nil")
	}
}

func TestParseRejectsUnknownKey(t *testing.T) {
	if _, err := parse(`nonsense = "x"`, "/home/u"); err == nil {
		t.Error("expected error for unknown key, got nil")
	}
}

func TestPathUsesXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg")
	if got := Path(); got != "/xdg/workenv/config.toml" {
		t.Errorf("Path() = %q, want /xdg/workenv/config.toml", got)
	}
}

func TestPathFallsBackToDotConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".config", "workenv", "config.toml")
	if got := Path(); got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}
