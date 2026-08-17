// Package wtpath renders the location of a worktree from a Go template
// (repo_path, repo, project, owner, branch, and a sanitize filter).
package wtpath

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"workenv/internal/naming"
)

// Default is the worktree_path template used when the config does not
// specify one: a sibling of the repository directory.
const Default = "{{ .repo_path }}/../{{ .repo }}.{{ .branch | sanitize }}"

// Vars holds the values a worktree_path template may reference.
type Vars struct {
	RepoPath string
	Repo     string
	Project  string
	Owner    string
	Branch   string
}

// Render executes tmpl against v and resolves the result to a path: a
// leading ~ or ~/ expands to the home directory, a still-relative result is
// joined onto v.RepoPath, then the path is cleaned. An unknown variable or
// a template that fails to parse or execute is an error.
func Render(tmpl string, v Vars) (string, error) {
	t, err := template.New("worktree_path").
		Option("missingkey=error").
		Funcs(template.FuncMap{"sanitize": naming.Sanitize}).
		Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("worktree_path template: %w", err)
	}

	data := map[string]any{
		"repo_path": v.RepoPath,
		"repo":      v.Repo,
		"project":   v.Project,
		"owner":     v.Owner,
		"branch":    v.Branch,
	}

	var buf bytes.Buffer
	if err = t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("worktree_path template: %w", err)
	}

	return resolve(buf.String(), v.RepoPath)
}

// resolve expands a leading ~ or ~/, joins a still-relative path onto
// repoPath, and cleans the result.
func resolve(rendered, repoPath string) (string, error) {
	if rendered == "~" || strings.HasPrefix(rendered, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("worktree_path template: %w", err)
		}
		rendered = filepath.Join(home, strings.TrimPrefix(rendered, "~"))
	}
	if !filepath.IsAbs(rendered) {
		rendered = filepath.Join(repoPath, rendered)
	}
	return filepath.Clean(rendered), nil
}
