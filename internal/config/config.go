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

	"workenv/internal/wtpath"
)

type Config struct {
	// ProjectsPath is where project repositories live (and get cloned to).
	ProjectsPath string
	// WorktreePath is a template for worktree placement, rendered per environment.
	// Defaults to wtpath.Default if not set.
	WorktreePath string
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
	cfg := Config{
		ClaudeCmd:    "claude",
		RemoteWe:     "we",
		WorktreePath: wtpath.Default,
	}
	var projectsPath, worktreePathOverride string
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
		case "projects_path":
			projectsPath = val
		case "worktree_path":
			worktreePathOverride = val
		case "claude_cmd":
			cfg.ClaudeCmd = val
		case "remote_we":
			cfg.RemoteWe = val
		case "projects_dir", "worktrees_dir":
			return Config{}, fmt.Errorf("config line %d: key %q is retired (use %q instead)", i+1, key, retiredKeyMap[key])
		default:
			return Config{}, fmt.Errorf("config line %d: unknown key %q", i+1, key)
		}
	}
	if projectsPath == "" {
		projectsPath = filepath.Join(home, "projects")
	}
	cfg.ProjectsPath = expandHome(projectsPath, home)
	if worktreePathOverride != "" {
		cfg.WorktreePath = worktreePathOverride
	}
	return cfg, nil
}

var retiredKeyMap = map[string]string{
	"projects_dir":  "projects_path",
	"worktrees_dir": "worktree_path",
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
