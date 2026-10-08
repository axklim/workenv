// Package config loads workenv settings from the XDG config directory
// ($XDG_CONFIG_HOME/workenv/config.toml, defaulting to ~/.config). Only a
// line-based TOML subset is supported — key = "value" lines and an
// [aliases] table header — which is enough for every setting we has and
// needs no TOML library.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"workenv/internal/naming"
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
	// RemoteControl also starts claude with --remote-control <session>.
	RemoteControl bool
	// Aliases maps a short name to a repository name in ProjectsPath; the
	// alias is the project name of every environment created in it.
	Aliases map[string]string
}

// AliasFor returns the alias of the repository named repo, or repo itself
// when it has none.
func (c Config) AliasFor(repo string) string {
	for alias, name := range c.Aliases {
		if name == repo {
			return alias
		}
	}
	return repo
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
	var projectsPath, worktreePathOverride, table string
	for i, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// As in TOML, a table header opens a table that runs to the next
		// header, so top-level keys must come before it.
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			table = strings.TrimSpace(line[1 : len(line)-1])
			if table != "aliases" {
				return Config{}, fmt.Errorf("config line %d: unknown table [%s]", i+1, table)
			}
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			return Config{}, fmt.Errorf("config line %d: expected key = \"value\"", i+1)
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"`)
		if table == "aliases" {
			if err := cfg.addAlias(key, val); err != nil {
				return Config{}, fmt.Errorf("config line %d: %w", i+1, err)
			}
			continue
		}
		switch key {
		case "projects_path":
			projectsPath = val
		case "worktree_path":
			worktreePathOverride = val
		case "claude_cmd":
			cfg.ClaudeCmd = val
		case "remote_we":
			cfg.RemoteWe = val
		case "remote_control":
			switch val {
			case "true":
				cfg.RemoteControl = true
			case "false":
				cfg.RemoteControl = false
			default:
				return Config{}, fmt.Errorf("config line %d: %s must be true or false, got %q", i+1, key, val)
			}
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

// addAlias records alias -> repo. The alias becomes a session prefix, so it
// keeps to TOML's bare-key characters, which are the ones session names
// keep; the value is a repository name, looked up in projects_path. One
// repository has at most one alias, so its project name is unambiguous.
func (c *Config) addAlias(alias, repo string) error {
	if alias == "" || naming.Sanitize(alias) != alias {
		return fmt.Errorf("alias %q: use letters, digits, - and _", alias)
	}
	if repo == "" || strings.ContainsAny(repo, "/\\") || strings.HasPrefix(repo, "~") {
		return fmt.Errorf("alias %s: %q must be a repository name in projects_path", alias, repo)
	}
	if _, dup := c.Aliases[alias]; dup {
		return fmt.Errorf("alias %s is defined twice", alias)
	}
	for other, name := range c.Aliases {
		if name == repo {
			return fmt.Errorf("aliases %s and %s both name %s", other, alias, repo)
		}
	}
	if c.Aliases == nil {
		c.Aliases = map[string]string{}
	}
	c.Aliases[alias] = repo
	return nil
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
