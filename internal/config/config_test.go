package config

import (
	"maps"
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
	if cfg.RemoteControl {
		t.Error("RemoteControl = true, want false by default")
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
remote_control = true
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
	if !cfg.RemoteControl {
		t.Error("RemoteControl = false, want true")
	}
}

// TestParseRemoteControlIsBoolean pins the value syntax: TOML booleans are
// bare true/false, and anything else is a config error rather than a silent
// false — a typo must not quietly turn Remote Control off.
func TestParseRemoteControlIsBoolean(t *testing.T) {
	for _, raw := range []string{`remote_control = false`, `remote_control = "false"`} {
		cfg, err := parse(raw, "/home/u")
		if err != nil {
			t.Errorf("parse(%q): %v", raw, err)
		} else if cfg.RemoteControl {
			t.Errorf("parse(%q): RemoteControl = true, want false", raw)
		}
	}
	for _, raw := range []string{`remote_control = yes`, `remote_control = 1`, `remote_control = ""`} {
		if _, err := parse(raw, "/home/u"); err == nil {
			t.Errorf("parse(%q): expected error for a non-boolean value, got nil", raw)
		}
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

func TestParseAliases(t *testing.T) {
	raw := `
projects_path = "~/src"

[aliases]
# comment
infra = "simple-dimple-infra"
[ aliases ]
w_e-2 = "workenv"
`
	cfg, err := parse(raw, "/home/u")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if cfg.ProjectsPath != "/home/u/src" {
		t.Errorf("ProjectsPath = %q, want /home/u/src", cfg.ProjectsPath)
	}
	want := map[string]string{"infra": "simple-dimple-infra", "w_e-2": "workenv"}
	if !maps.Equal(cfg.Aliases, want) {
		t.Errorf("Aliases = %v, want %v", cfg.Aliases, want)
	}
	if got := cfg.AliasFor("simple-dimple-infra"); got != "infra" {
		t.Errorf("AliasFor(simple-dimple-infra) = %q, want infra", got)
	}
	if got := cfg.AliasFor("trade"); got != "trade" {
		t.Errorf("AliasFor(trade) = %q, want trade (no alias)", got)
	}
}

// TestParseAliasesTableRunsToTheEnd pins TOML's table semantics: a key
// after [aliases] is an alias, never a top-level setting.
func TestParseAliasesTableRunsToTheEnd(t *testing.T) {
	cfg, err := parse("[aliases]\nclaude_cmd = \"x\"\n", "/home/u")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if cfg.ClaudeCmd != "claude" || cfg.Aliases["claude_cmd"] != "x" {
		t.Errorf("ClaudeCmd = %q, Aliases = %v; want the key read as an alias", cfg.ClaudeCmd, cfg.Aliases)
	}
}

func TestParseRejectsBadAliases(t *testing.T) {
	for _, raw := range []string{
		"[remotes]\nx = \"y\"",
		"[aliases]\n\"in fra\" = \"simple-dimple-infra\"",
		"[aliases]\nin.fra = \"simple-dimple-infra\"",
		"[aliases]\ninfra = \"\"",
		"[aliases]\ninfra = \"~/src/simple-dimple-infra\"",
		"[aliases]\ninfra = \"axklim/simple-dimple-infra\"",
		"[aliases]\ninfra = \"a\"\ninfra = \"b\"",
		"[aliases]\ninfra = \"simple-dimple-infra\"\nsdi = \"simple-dimple-infra\"",
	} {
		if _, err := parse(raw, "/home/u"); err == nil {
			t.Errorf("parse(%q): expected an error, got nil", raw)
		}
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
