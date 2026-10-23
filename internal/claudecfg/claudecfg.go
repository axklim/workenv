// Package claudecfg pre-answers Claude Code's "Is this a project you
// trust?" dialog by recording the answer in claude's global config file,
// ~/.claude.json. claude rewrites that file whole, so it is read, changed
// and renamed into place in one step, and every value but the one added is
// carried over unchanged.
package claudecfg

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const trustKey = "hasTrustDialogAccepted"

// Path is claude's global config file: $CLAUDE_CONFIG_DIR/.claude.json when
// that is set, else ~/.claude.json.
func Path() (string, error) {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = home
	}
	return filepath.Join(dir, ".claude.json"), nil
}

// Trust sets projects[dir].hasTrustDialogAccepted = true in file, so claude
// started in dir skips the trust dialog. A key that is already there is
// left alone either way: an explicit false is an answer too. A missing or
// empty file starts from {}. It reports whether the file was changed.
func Trust(file, dir string) (bool, error) {
	// A symlinked file (a dotfiles checkout) is updated where it points,
	// not replaced by a regular file.
	if p, err := filepath.EvalSymlinks(file); err == nil {
		file = p
	}
	raw, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	var root map[string]json.RawMessage
	if err := unmarshalObject(raw, &root); err != nil {
		return false, fmt.Errorf("%s: %w", file, err)
	}
	var projects map[string]json.RawMessage
	if err := unmarshalObject(root["projects"], &projects); err != nil {
		return false, fmt.Errorf("%s: projects: %w", file, err)
	}
	var project map[string]json.RawMessage
	if err := unmarshalObject(projects[dir], &project); err != nil {
		return false, fmt.Errorf("%s: projects[%q]: %w", file, dir, err)
	}
	if _, ok := project[trustKey]; ok {
		return false, nil
	}
	project[trustKey] = json.RawMessage("true")
	if projects[dir], err = json.Marshal(project); err != nil {
		return false, err
	}
	if root["projects"], err = json.Marshal(projects); err != nil {
		return false, err
	}
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(root); err != nil {
		return false, err
	}
	return true, writeAtomic(file, out.Bytes())
}

// unmarshalObject decodes a JSON object into *m, treating absent, empty
// and null input as an empty object.
func unmarshalObject(raw []byte, m *map[string]json.RawMessage) error {
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, m); err != nil {
			return err
		}
	}
	if *m == nil {
		*m = map[string]json.RawMessage{}
	}
	return nil
}

// writeAtomic replaces file with data through a temp file in the same
// directory and a rename, keeping the file's permissions (0600 for a new
// one: it holds claude's credentials and history).
func writeAtomic(file string, data []byte) error {
	mode := fs.FileMode(0o600)
	if fi, err := os.Stat(file); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), filepath.Base(file)+".we-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}
