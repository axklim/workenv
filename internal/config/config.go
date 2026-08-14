// Package config loads workenv settings from the XDG config directory
// ($XDG_CONFIG_HOME/workenv/config.toml, defaulting to ~/.config). Only a
// flat key = "value" TOML subset is supported, which keeps workenv
// dependency-free.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	// ProjectsDir is where project repositories live (and get cloned to).
	ProjectsDir string
	// WorktreesDir is the root under which worktrees are created, laid out
	// as <WorktreesDir>/<project>/<name>. The deterministic layout is what
	// makes the program stateless.
	WorktreesDir string
	// ClaudeCmd is the command started in the first tmux window.
	ClaudeCmd string
	// RemoteWe is the path of the we binary on remote hosts.
	RemoteWe string
}

// Path returns the config file location following XDG notation.
func Path() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "workenv", "config.toml")
}

// Load reads the config file if present; a missing file yields defaults.
func Load() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	raw, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return parse("", home)
		}
		return Config{}, err
	}
	return parse(string(raw), home)
}

func parse(raw, home string) (Config, error) {
	cfg := Config{ClaudeCmd: "claude", RemoteWe: "we"}
	var projectsDir, worktreesDir string
	for i, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return Config{}, fmt.Errorf("config line %d: expected key = \"value\"", i+1)
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"`)
		switch key {
		case "projects_dir":
			projectsDir = val
		case "worktrees_dir":
			worktreesDir = val
		case "claude_cmd":
			cfg.ClaudeCmd = val
		case "remote_we":
			cfg.RemoteWe = val
		default:
			return Config{}, fmt.Errorf("config line %d: unknown key %q", i+1, key)
		}
	}
	if projectsDir == "" {
		projectsDir = filepath.Join(home, "projects")
	}
	cfg.ProjectsDir = expandHome(projectsDir, home)
	if worktreesDir == "" {
		worktreesDir = filepath.Join(cfg.ProjectsDir, ".we")
	}
	cfg.WorktreesDir = expandHome(worktreesDir, home)
	return cfg, nil
}

func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}
