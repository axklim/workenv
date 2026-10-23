package claudecfg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if p, err := Path(); err != nil || p != filepath.Join(home, ".claude.json") {
		t.Errorf("Path() = %q, %v; want ~/.claude.json", p, err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/cfg")
	if p, err := Path(); err != nil || p != "/cfg/.claude.json" {
		t.Errorf("Path() = %q, %v; want $CLAUDE_CONFIG_DIR/.claude.json", p, err)
	}
}

func decode(t *testing.T, file string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%s is not JSON: %v\n%s", file, err, raw)
	}
	return m
}

func trusted(m map[string]any, dir string) any {
	projects, _ := m["projects"].(map[string]any)
	project, _ := projects[dir].(map[string]any)
	return project[trustKey]
}

func TestTrustMissingOrEmptyFile(t *testing.T) {
	for name, content := range map[string]*string{"missing": nil, "empty": new(string)} {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), ".claude.json")
			if content != nil {
				if err := os.WriteFile(file, []byte(*content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			changed, err := Trust(file, "/p/repo")
			if err != nil || !changed {
				t.Fatalf("Trust = %v, %v; want true, nil", changed, err)
			}
			if got := trusted(decode(t, file), "/p/repo"); got != true {
				t.Errorf("hasTrustDialogAccepted = %v, want true", got)
			}
			fi, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			want := os.FileMode(0o600)
			if content != nil {
				want = 0o644
			}
			if fi.Mode().Perm() != want {
				t.Errorf("mode = %v, want %v", fi.Mode().Perm(), want)
			}
		})
	}
}

// TestTrustKeepsEveryOtherValue: claude owns the file, so everything else
// in it — other top-level keys, other projects, the project's own keys —
// must come back unchanged, and no temp file may be left behind.
func TestTrustKeepsEveryOtherValue(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, ".claude.json")
	orig := `{
  "numStartups": 12345678901234567890,
  "oauthAccount": {"emailAddress": "a<b>&c@example.com"},
  "projects": {
    "/p/other": {"hasTrustDialogAccepted": true, "allowedTools": ["Bash"]},
    "/p/repo": {"history": [{"display": "hi"}]}
  }
}`
	if err := os.WriteFile(file, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	if changed, err := Trust(file, "/p/repo"); err != nil || !changed {
		t.Fatalf("Trust = %v, %v; want true, nil", changed, err)
	}
	var want map[string]any
	if err := json.Unmarshal([]byte(orig), &want); err != nil {
		t.Fatal(err)
	}
	want["projects"].(map[string]any)["/p/repo"].(map[string]any)[trustKey] = true
	if got := decode(t, file); !reflect.DeepEqual(got, want) {
		t.Errorf("file = %v\nwant %v", got, want)
	}
	raw, _ := os.ReadFile(file)
	var big struct{ NumStartups json.Number }
	if err := json.Unmarshal(raw, &big); err != nil || big.NumStartups != "12345678901234567890" {
		t.Errorf("numStartups = %q, %v; want the original digits", big.NumStartups, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only .claude.json", len(entries))
	}
}

// TestTrustLeavesAnExistingAnswer: the key is added only when absent, and
// the file is then not rewritten at all.
func TestTrustLeavesAnExistingAnswer(t *testing.T) {
	for _, v := range []string{"true", "false"} {
		file := filepath.Join(t.TempDir(), ".claude.json")
		orig := `{"projects": {"/p/repo": {"hasTrustDialogAccepted": ` + v + `}}}`
		if err := os.WriteFile(file, []byte(orig), 0o600); err != nil {
			t.Fatal(err)
		}
		changed, err := Trust(file, "/p/repo")
		if err != nil || changed {
			t.Errorf("%s: Trust = %v, %v; want false, nil", v, changed, err)
		}
		if raw, _ := os.ReadFile(file); string(raw) != orig {
			t.Errorf("%s: file rewritten:\n%s", v, raw)
		}
	}
}

func TestTrustRefusesMalformedFile(t *testing.T) {
	for _, orig := range []string{`{"projects": `, `[]`, `{"projects": []}`, `{"projects": {"/p/repo": 1}}`} {
		file := filepath.Join(t.TempDir(), ".claude.json")
		if err := os.WriteFile(file, []byte(orig), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Trust(file, "/p/repo"); err == nil {
			t.Errorf("Trust over %s: expected an error", orig)
		}
		if raw, _ := os.ReadFile(file); string(raw) != orig {
			t.Errorf("malformed file %s was rewritten:\n%s", orig, raw)
		}
	}
}

func TestTrustNullProjects(t *testing.T) {
	file := filepath.Join(t.TempDir(), ".claude.json")
	if err := os.WriteFile(file, []byte(`{"projects": null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Trust(file, "/p/repo"); err != nil {
		t.Fatal(err)
	}
	if got := trusted(decode(t, file), "/p/repo"); got != true {
		t.Errorf("hasTrustDialogAccepted = %v, want true", got)
	}
}

// TestTrustFollowsSymlink: a ~/.claude.json linked from a dotfiles checkout
// stays a link, and the target is what changes.
func TestTrustFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "dotfiles.json")
	link := filepath.Join(dir, ".claude.json")
	if err := os.WriteFile(target, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Trust(link, "/p/repo"); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s is no longer a symlink", link)
	}
	if got := trusted(decode(t, target), "/p/repo"); got != true {
		t.Errorf("link target: hasTrustDialogAccepted = %v, want true", got)
	}
}
