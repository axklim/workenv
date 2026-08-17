// Package state persists the registry of work environments as JSON under
// the XDG state directory ($XDG_STATE_HOME/workenv/envs.json, defaulting to
// ~/.local/state). Each environment is keyed by an integer id, assigned
// from a monotonic counter and never reused, so a stale reference fails
// instead of silently resolving to a different environment. GitHub issue
// and PR references are stored as full URLs, so a reference carries its
// own repository rather than relying on an owner/repo recorded elsewhere
// on the environment.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Env is one recorded work environment, keyed by ID.
type Env struct {
	ID           int       `json:"id"`
	Project      string    `json:"project"`
	Branch       string    `json:"branch"`
	TmuxSession  string    `json:"tmux_session"`
	WorktreePath string    `json:"worktree_path"`
	RepoPath     string    `json:"repo_path"`
	Issues       []string  `json:"issues,omitempty"`
	PRs          []string  `json:"prs,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// Store is the registry: the file it lives in plus its decoded contents.
type Store struct {
	Path   string // file location, not serialised
	NextID int    // next_id
	Envs   []*Env // envs
}

// file is the on-disk shape.
type file struct {
	NextID int    `json:"next_id"`
	Envs   []*Env `json:"envs"`
}

// DefaultPath returns the registry location following XDG notation.
func DefaultPath() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "workenv", "envs.json")
}

// Load reads the registry at path; a missing file yields an empty store
// with NextID 1. If the file's next_id is missing or lower than the
// highest recorded id, it is raised to max(id)+1, so a hand-edited file
// cannot hand out a duplicate id.
func Load(path string) (*Store, error) {
	s := &Store{Path: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			s.NextID = 1
			return s, nil
		}
		return nil, err
	}
	var f file
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	s.NextID = f.NextID
	s.Envs = f.Envs

	maxID := 0
	for _, e := range s.Envs {
		if e.ID <= 0 {
			return nil, fmt.Errorf("%s: record %q has id %d; this looks like a pre-release registry (from before ids were introduced) — delete the file and let workenv recreate it", path, e.Branch, e.ID)
		}
		if e.ID > maxID {
			maxID = e.ID
		}
	}
	if s.NextID < maxID+1 {
		s.NextID = maxID + 1
	}
	return s, nil
}

// Save writes the registry atomically (temp file in the same directory,
// then rename), so a crash can never leave a truncated file behind.
func (s *Store) Save() error {
	dir := filepath.Dir(s.Path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	envs := s.Envs
	if envs == nil {
		envs = []*Env{}
	}
	data, err := json.MarshalIndent(file{NextID: s.NextID, Envs: envs}, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".envs-*.json")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(append(data, '\n'))
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), s.Path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

func (s *Store) find(pred func(*Env) bool) *Env {
	for _, e := range s.Envs {
		if pred(e) {
			return e
		}
	}
	return nil
}

// ByID finds the environment with the given id.
func (s *Store) ByID(id int) *Env {
	return s.find(func(e *Env) bool { return e.ID == id })
}

// BySession finds the environment holding the given tmux session name.
func (s *Store) BySession(session string) *Env {
	return s.find(func(e *Env) bool { return e.TmuxSession == session })
}

// ByBranch finds the environment on branch within the repository at
// repoPath. Branch names alone are not enough: two clones of the same
// repository share a project name but are different repositories, and each
// can be on the same branch independently.
func (s *Store) ByBranch(repoPath, branch string) *Env {
	return s.find(func(e *Env) bool { return e.RepoPath == repoPath && e.Branch == branch })
}

// ByWorktree finds the environment whose worktree is at path.
func (s *Store) ByWorktree(path string) *Env {
	return s.find(func(e *Env) bool { return e.WorktreePath == path })
}

// ByRef finds the environment holding url among either its issues or PRs.
// Comparison is case-insensitive: GitHub owner/repo segments are, and
// target.Parse preserves whatever case the user typed, so two spellings of
// the same URL must resolve to the same environment rather than each
// recording its own copy of the reference.
func (s *Store) ByRef(url string) *Env {
	return s.find(func(e *Env) bool {
		return containsFold(e.Issues, url) || containsFold(e.PRs, url)
	})
}

// containsFold reports whether list contains url, ignoring case.
func containsFold(list []string, url string) bool {
	return slices.ContainsFunc(list, func(u string) bool { return strings.EqualFold(u, url) })
}

// Matching returns every environment satisfying pred.
func (s *Store) Matching(pred func(*Env) bool) []*Env {
	var out []*Env
	for _, e := range s.Envs {
		if pred(e) {
			out = append(out, e)
		}
	}
	return out
}

// Add assigns env an id when it doesn't already have one, advances NextID
// past it, appends env to the registry and returns it. An explicit non-zero
// ID is respected and still advances NextID past it, so ids are never
// reused even when assigned by the caller. It is the caller's
// responsibility to ensure an explicit ID does not already exist in the
// store; Add does not check and has no error return to report a collision.
func (s *Store) Add(env *Env) *Env {
	if env.ID == 0 {
		if s.NextID < 1 {
			s.NextID = 1
		}
		env.ID = s.NextID
	}
	if env.ID+1 > s.NextID {
		s.NextID = env.ID + 1
	}
	s.Envs = append(s.Envs, env)
	return env
}

// Remove drops the environment with the given id, reporting whether it
// existed. The id itself is never reused: NextID only ever increases.
func (s *Store) Remove(id int) bool {
	before := len(s.Envs)
	s.Envs = slices.DeleteFunc(s.Envs, func(e *Env) bool { return e.ID == id })
	return len(s.Envs) != before
}

// Link appends each of issues and prs to env's Issues/PRs, skipping any URL
// that is already recorded there or that belongs to a different
// environment. Comparison is case-insensitive (see ByRef); beyond case,
// canonicalising a URL before calling Link is still the caller's job.
func (s *Store) Link(env *Env, issues, prs []string) {
	env.Issues = linkRefs(s, env, env.Issues, issues)
	env.PRs = linkRefs(s, env, env.PRs, prs)
}

func linkRefs(s *Store, env *Env, existing []string, urls []string) []string {
	for _, u := range urls {
		if owner := s.ByRef(u); owner != nil && owner != env {
			continue
		}
		if !containsFold(existing, u) {
			existing = append(existing, u)
		}
	}
	return existing
}
