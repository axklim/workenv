package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	home := t.TempDir()
	cfg, err := parse("", home)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if want := filepath.Join(home, "projects"); cfg.ProjectsDir != want {
		t.Errorf("ProjectsDir = %q, want %q", cfg.ProjectsDir, want)
	}
	if want := filepath.Join(home, "projects", ".we"); cfg.WorktreesDir != want {
		t.Errorf("WorktreesDir = %q, want %q", cfg.WorktreesDir, want)
	}
	if cfg.ClaudeCmd != "claude" {
		t.Errorf("ClaudeCmd = %q, want claude", cfg.ClaudeCmd)
	}
	if cfg.RemoteWe != "we" {
		t.Errorf("RemoteWe = %q, want we", cfg.RemoteWe)
	}
}

func TestParseOverrides(t *testing.T) {
	home := "/home/u"
	raw := `
# comment
projects_dir = "~/src"
worktrees_dir = "/data/wt"
claude_cmd = "claude --dangerously-skip-permissions"
remote_we = "/usr/local/bin/we"
`
	cfg, err := parse(raw, home)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if cfg.ProjectsDir != "/home/u/src" {
		t.Errorf("ProjectsDir = %q, want /home/u/src (tilde expanded)", cfg.ProjectsDir)
	}
	if cfg.WorktreesDir != "/data/wt" {
		t.Errorf("WorktreesDir = %q, want /data/wt", cfg.WorktreesDir)
	}
	if cfg.ClaudeCmd != "claude --dangerously-skip-permissions" {
		t.Errorf("ClaudeCmd = %q", cfg.ClaudeCmd)
	}
	if cfg.RemoteWe != "/usr/local/bin/we" {
		t.Errorf("RemoteWe = %q", cfg.RemoteWe)
	}
}

func TestWorktreesDirFollowsProjectsDir(t *testing.T) {
	cfg, err := parse(`projects_dir = "/code"`, "/home/u")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if cfg.WorktreesDir != "/code/.we" {
		t.Errorf("WorktreesDir = %q, want /code/.we", cfg.WorktreesDir)
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
