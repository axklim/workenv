package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sample() *Env {
	return &Env{
		Project:      "trade",
		Branch:       "review_claude-file",
		TmuxSession:  "trade-review_claude-file",
		WorktreePath: "/u/projects/trade.review_claude-file",
		RepoPath:     "/u/projects/trade",
		Issues:       []string{"https://github.com/axklim/trade/issues/59"},
		PRs:          []string{"https://github.com/axklim/trade/pull/61"},
		CreatedAt:    time.Date(2026, 8, 16, 14, 0, 0, 0, time.UTC),
	}
}

func TestLoadMissingFileIsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "nope", "envs.json"))
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(s.Envs) != 0 {
		t.Errorf("Envs = %+v, want empty", s.Envs)
	}
	if s.NextID != 1 {
		t.Errorf("NextID = %d, want 1", s.NextID)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "envs.json")
	s := &Store{Path: path}
	s.Add(sample())
	if err := s.Save(); err != nil {
		t.Fatalf("Save error: %v", err)
	}
	// No temp file left behind next to the registry.
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "envs.json" {
		t.Errorf("state dir = %v, want only envs.json", entries)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(got.Envs) != 1 {
		t.Fatalf("Envs = %+v, want 1", got.Envs)
	}
	want := sample()
	want.ID = 1
	e := got.Envs[0]
	if e.ID != want.ID || e.Project != want.Project || e.Branch != want.Branch ||
		e.TmuxSession != want.TmuxSession || e.WorktreePath != want.WorktreePath ||
		e.RepoPath != want.RepoPath || !e.CreatedAt.Equal(want.CreatedAt) ||
		len(e.Issues) != 1 || e.Issues[0] != want.Issues[0] ||
		len(e.PRs) != 1 || e.PRs[0] != want.PRs[0] {
		t.Errorf("round trip = %+v, want %+v", e, want)
	}
	if got.NextID != 2 {
		t.Errorf("NextID = %d, want 2", got.NextID)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(raw)
	for _, key := range []string{
		`"next_id"`, `"tmux_session"`, `"worktree_path"`, `"repo_path"`,
		"https://github.com/axklim/trade/issues/59",
	} {
		if !strings.Contains(text, key) {
			t.Errorf("file lacks %s:\n%s", key, text)
		}
	}
}

func TestSaveEmptyStoreWritesEmptyArray(t *testing.T) {
	path := filepath.Join(t.TempDir(), "envs.json")
	s := &Store{Path: path}
	if err := s.Save(); err != nil {
		t.Fatalf("Save error: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, `"envs": []`) {
		t.Errorf("file = %s, want \"envs\": [] rather than null", text)
	}
	if strings.Contains(text, "null") {
		t.Errorf("file = %s, must not contain null", text)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if len(got.Envs) != 0 {
		t.Errorf("Envs = %+v, want empty", got.Envs)
	}
}

func TestLoadRejectsCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "envs.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Error("expected error for corrupt file")
	}
}

func TestAddAssignsSequentialIDs(t *testing.T) {
	s := &Store{}
	a := s.Add(&Env{Project: "trade", Branch: "a"})
	b := s.Add(&Env{Project: "trade", Branch: "b"})
	c := s.Add(&Env{Project: "trade", Branch: "c"})

	if a.ID != 1 || b.ID != 2 || c.ID != 3 {
		t.Errorf("ids = %d, %d, %d, want 1, 2, 3", a.ID, b.ID, c.ID)
	}
	if s.NextID != 4 {
		t.Errorf("NextID = %d, want 4", s.NextID)
	}
}

func TestRemoveDoesNotFreeTheID(t *testing.T) {
	s := &Store{}
	s.Add(&Env{Project: "trade", Branch: "a"})
	second := s.Add(&Env{Project: "trade", Branch: "b"})
	if !s.Remove(second.ID) {
		t.Fatal("Remove should report success")
	}
	third := s.Add(&Env{Project: "trade", Branch: "c"})
	if third.ID != 3 {
		t.Errorf("new id = %d, want 3", third.ID)
	}
	if s.ByID(2) != nil {
		t.Errorf("ByID(2) = %+v, want nil (id 2 was removed, not reused)", s.ByID(2))
	}
	if s.Remove(second.ID) {
		t.Error("Remove of an already-removed id should report false")
	}
	if s.Remove(999) {
		t.Error("Remove of an unknown id should report false")
	}
}

// TestAddRespectsExplicitID pins the three cases the design spec's next_id
// invariant depends on: an explicit id above NextID is honoured and
// advances the counter; an explicit id below NextID is honoured without
// moving the counter backwards; and a later zero-id Add still gets the
// advanced value, not a value that collides with the explicit id.
func TestAddRespectsExplicitID(t *testing.T) {
	s := &Store{}

	high := s.Add(&Env{ID: 5, Project: "trade", Branch: "high"})
	if high.ID != 5 {
		t.Fatalf("ID = %d, want 5 (explicit id must be respected)", high.ID)
	}
	if s.NextID != 6 {
		t.Errorf("NextID = %d, want 6 (must advance past the explicit id)", s.NextID)
	}

	low := s.Add(&Env{ID: 2, Project: "trade", Branch: "low"})
	if low.ID != 2 {
		t.Fatalf("ID = %d, want 2 (explicit id must be respected)", low.ID)
	}
	if s.NextID != 6 {
		t.Errorf("NextID = %d, want 6 (must not regress for a lower explicit id)", s.NextID)
	}

	next := s.Add(&Env{Project: "trade", Branch: "next"})
	if next.ID != 6 {
		t.Errorf("ID = %d, want 6 (a zero-id Add must get the advanced NextID)", next.ID)
	}
	if s.NextID != 7 {
		t.Errorf("NextID = %d, want 7", s.NextID)
	}
}

func TestLoadRepairsNextID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "envs.json")
	raw := `{"next_id": 1, "envs": [{"id": 9, "project": "trade", "branch": "a"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if s.NextID != 10 {
		t.Errorf("NextID = %d, want 10", s.NextID)
	}
}

// TestLoadRejectsPreReleaseZeroIDs guards against a pre-release envs.json
// (written before ids existed) silently loading with every record at
// ID == 0: ls would show duplicate ids and "we delete 0" would remove every
// one of them in a single sweep. Load must refuse instead.
func TestLoadRejectsPreReleaseZeroIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "envs.json")
	raw := `{"next_id": 1, "envs": [{"project": "trade", "branch": "a"}]}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected an error for a pre-release registry (id 0)")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error should name the file %q: %v", path, err)
	}
}

// TestByRefIsCaseInsensitive covers the fix for a case-variant URL
// duplicating a reference: GitHub owner/repo are case-insensitive but
// target.Parse preserves what the user typed, so ByRef (and Link's
// duplicate check, which is built on it) must compare without regard to
// case or "we open https://github.com/AxKlim/trade/issues/59" would miss
// an already-recorded "axklim" spelling and append a second URL for the
// same issue.
func TestByRefIsCaseInsensitive(t *testing.T) {
	s := &Store{}
	env := s.Add(&Env{Project: "trade", Branch: "x", Issues: []string{"https://github.com/AxKlim/trade/issues/59"}})

	if s.ByRef("https://github.com/axklim/trade/issues/59") != env {
		t.Error("ByRef should match a case-variant of a recorded URL")
	}
	if s.ByRef("https://github.com/axklim/TRADE/issues/59") != env {
		t.Error("ByRef should match regardless of which segment's case differs")
	}

	s.Link(env, []string{"https://github.com/axklim/trade/issues/59"}, nil)
	if len(env.Issues) != 1 {
		t.Errorf("Link must not duplicate a case-variant of an already-recorded URL: %v", env.Issues)
	}
}

func TestLookups(t *testing.T) {
	s := &Store{}
	e := s.Add(sample())
	other := s.Add(&Env{
		Project:      "trade",
		Branch:       "review_claude-file", // same branch name, different repo
		TmuxSession:  "trade2-review_claude-file",
		WorktreePath: "/u/projects/trade2.review_claude-file",
		RepoPath:     "/u/projects/trade2",
	})

	if s.ByID(e.ID) != e || s.ByID(999) != nil {
		t.Error("ByID")
	}
	if s.BySession("trade-review_claude-file") != e || s.BySession("nope") != nil {
		t.Error("BySession")
	}
	if s.ByBranch("/u/projects/trade", "review_claude-file") != e {
		t.Error("ByBranch should find the env in the first repo")
	}
	if s.ByBranch("/u/projects/trade2", "review_claude-file") != other {
		t.Error("ByBranch should find the env in the second repo, not the first")
	}
	if s.ByBranch("/u/projects/trade", "nope") != nil {
		t.Error("ByBranch should return nil for an unknown branch")
	}
	if s.ByWorktree(e.WorktreePath) != e || s.ByWorktree("/nope") != nil {
		t.Error("ByWorktree")
	}
	if s.ByRef("https://github.com/axklim/trade/issues/59") != e {
		t.Error("ByRef should resolve an issue URL")
	}
	if s.ByRef("https://github.com/axklim/trade/pull/61") != e {
		t.Error("ByRef should resolve a PR URL")
	}
	if s.ByRef("https://github.com/axklim/trade/issues/1234") != nil {
		t.Error("ByRef should return nil for an unknown URL")
	}
}

func TestLinkSkipsRefsOwnedElsewhere(t *testing.T) {
	s := &Store{}
	const urlA = "https://github.com/axklim/trade/issues/1"
	const urlB = "https://github.com/axklim/trade/issues/2"
	env1 := s.Add(&Env{Project: "trade", Branch: "one", Issues: []string{urlA}})
	env2 := s.Add(&Env{Project: "trade", Branch: "two"})

	s.Link(env2, []string{urlA, urlB}, nil)
	if len(env2.Issues) != 1 || env2.Issues[0] != urlB {
		t.Errorf("env2.Issues = %v, want [%s] (urlA belongs to env1)", env2.Issues, urlB)
	}
	if len(env1.Issues) != 1 || env1.Issues[0] != urlA {
		t.Errorf("env1.Issues = %v, should be untouched", env1.Issues)
	}

	// Re-linking a URL the env already owns must not duplicate it.
	s.Link(env2, []string{urlB}, nil)
	if len(env2.Issues) != 1 {
		t.Errorf("relinking must be idempotent: %v", env2.Issues)
	}
}

func TestMatching(t *testing.T) {
	s := &Store{}
	s.Add(&Env{Project: "trade", Branch: "a"})
	s.Add(&Env{Project: "trade", Branch: "b"})
	s.Add(&Env{Project: "other", Branch: "c"})

	got := s.Matching(func(e *Env) bool { return e.Project == "trade" })
	if len(got) != 2 {
		t.Errorf("Matching = %d, want 2", len(got))
	}
	for _, e := range got {
		if e.Project != "trade" {
			t.Errorf("Matching returned %+v, want only project=trade", e)
		}
	}
}

func TestDefaultPathHonoursXDGStateHome(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", "/xdg")
	if got, want := DefaultPath(), "/xdg/workenv/envs.json"; got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}

	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".local", "state", "workenv", "envs.json")
	if got := DefaultPath(); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}
