// Package tmuxx manages the tmux sessions backing work environments.
//
// The PDF asks whether tmux sessions support extra attributes: they do —
// user options ("@"-prefixed, settable per session). workenv marks its
// sessions with @workenv_* options, which makes `we list` stateless: the
// sessions themselves are the registry.
package tmuxx

import (
	"strconv"
	"strings"

	"workenv/internal/execx"
)

type Tmux struct {
	R execx.Runner
}

// Session is a tmux session that carries workenv markers.
type Session struct {
	Name     string
	Project  string
	WeName   string
	Path     string
	Attached bool
}

func (t Tmux) Has(name string) bool {
	// "=" forces an exact match instead of tmux's prefix matching.
	_, err := t.R.Output("", "tmux", "has-session", "-t", "="+name)
	return err == nil
}

// New creates a detached session rooted at dir and tags it with workenv
// user options so it can be told apart from regular tmux sessions.
func (t Tmux) New(name, dir, project, weName string) error {
	if _, err := t.R.Output("", "tmux", "new-session", "-d", "-s", name, "-c", dir); err != nil {
		return err
	}
	options := map[string]string{
		"@workenv":         "1",
		"@workenv_project": project,
		"@workenv_name":    weName,
		"@workenv_path":    dir,
	}
	for key, val := range options {
		// No "=" prefix here: set-option resolves a pane target in tmux 3.5,
		// which rejects the exact-match syntax. Exact names still win over
		// prefix matches, so the bare name is unambiguous.
		if _, err := t.R.Output("", "tmux", "set-option", "-t", name, key, val); err != nil {
			return err
		}
	}
	return nil
}

// RunInFirstWindow types cmd into the session's first window. send-keys is
// used instead of passing the command to new-session so the window (and
// session) survives the command exiting.
func (t Tmux) RunInFirstWindow(name, cmd string) error {
	// send-keys takes a pane target, which rejects the "=" exact-match syntax.
	_, err := t.R.Output("", "tmux", "send-keys", "-t", name, cmd, "Enter")
	return err
}

func (t Tmux) Kill(name string) error {
	_, err := t.R.Output("", "tmux", "kill-session", "-t", "="+name)
	return err
}

// List returns all workenv-tagged sessions. A stopped tmux server means no
// sessions, not an error.
func (t Tmux) List() ([]Session, error) {
	format := "#{session_name}\t#{@workenv_project}\t#{@workenv_name}\t#{@workenv_path}\t#{session_attached}"
	out, err := t.R.Output("", "tmux", "list-sessions", "-F", format)
	if err != nil {
		if strings.Contains(err.Error(), "no server running") ||
			strings.Contains(err.Error(), "No such file or directory") {
			return nil, nil
		}
		return nil, err
	}
	return parseSessions(out), nil
}

// HasClients reports whether any terminal is currently attached.
func (t Tmux) HasClients(name string) bool {
	out, err := t.R.Output("", "tmux", "list-clients", "-t", "="+name)
	return err == nil && strings.TrimSpace(out) != ""
}

// SwitchClient jumps the current tmux client to the session (used when we
// is invoked from inside tmux).
func (t Tmux) SwitchClient(name string) error {
	_, err := t.R.Output("", "tmux", "switch-client", "-t", "="+name)
	return err
}

func parseSessions(raw string) []Session {
	var sessions []Session
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 5 || fields[1] == "" {
			continue
		}
		attached, _ := strconv.Atoi(fields[4])
		sessions = append(sessions, Session{
			Name:     fields[0],
			Project:  fields[1],
			WeName:   fields[2],
			Path:     fields[3],
			Attached: attached > 0,
		})
	}
	return sessions
}
