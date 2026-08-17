// Package tmuxx manages the tmux sessions backing work environments.
//
// Sessions carry two tmux user options ("@"-prefixed, settable per
// session): @workenv=1 and @workenv_id=<id>. They are not the registry —
// the state package's JSON file is — but they let we tell its own sessions
// from personal ones, which matters for listing (only tagged sessions are
// live work environments) and adoption (an untagged session of the same
// name is never reused or killed).
package tmuxx

import (
	"strconv"
	"strings"

	"workenv/internal/execx"
)

type Tmux struct {
	R execx.Runner
}

// Session is a live tmux session tagged by we.
type Session struct {
	Name     string
	ID       int
	Path     string
	Attached bool
}

func (t Tmux) Has(name string) bool {
	// "=" forces an exact match instead of tmux's prefix matching.
	_, err := t.R.Output("", "tmux", "has-session", "-t", "="+name)
	return err == nil
}

// IsWorkenv reports whether the live session name carries the @workenv tag.
// It is what tells a we session apart from a stranger's session of the same
// name, which must never be adopted or killed.
func (t Tmux) IsWorkenv(name string) bool {
	out, err := t.R.Output("", "tmux", "show-options", "-t", name, "@workenv")
	return err == nil && strings.TrimSpace(out) != ""
}

// New creates a detached session rooted at dir and tags it with the
// environment's id.
func (t Tmux) New(session, dir string, id int) error {
	if _, err := t.R.Output("", "tmux", "new-session", "-d", "-s", session, "-c", dir); err != nil {
		return err
	}
	// No "=" prefix here: set-option resolves a pane target in tmux 3.5,
	// which rejects the exact-match syntax. Exact names still win over
	// prefix matches, so the bare name is unambiguous.
	if _, err := t.R.Output("", "tmux", "set-option", "-t", session, "@workenv", "1"); err != nil {
		return err
	}
	_, err := t.R.Output("", "tmux", "set-option", "-t", session, "@workenv_id", strconv.Itoa(id))
	return err
}

// RunInFirstWindow types cmd into the session's first window. send-keys is
// used instead of passing the command to new-session so the window (and
// session) survives the command exiting.
func (t Tmux) RunInFirstWindow(session, cmd string) error {
	// send-keys takes a pane target, which rejects the "=" exact-match syntax.
	_, err := t.R.Output("", "tmux", "send-keys", "-t", session, cmd, "Enter")
	return err
}

func (t Tmux) Kill(session string) error {
	_, err := t.R.Output("", "tmux", "kill-session", "-t", "="+session)
	return err
}

// List returns every workenv-tagged live session. A stopped tmux server
// means no sessions, not an error.
func (t Tmux) List() ([]Session, error) {
	format := "#{session_name}\t#{@workenv_id}\t#{session_path}\t#{session_attached}"
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
func (t Tmux) HasClients(session string) bool {
	out, err := t.R.Output("", "tmux", "list-clients", "-t", "="+session)
	return err == nil && strings.TrimSpace(out) != ""
}

// SwitchClient jumps the current tmux client to the session (used when we
// is invoked from inside tmux).
func (t Tmux) SwitchClient(session string) error {
	_, err := t.R.Output("", "tmux", "switch-client", "-t", "="+session)
	return err
}

func parseSessions(raw string) []Session {
	var sessions []Session
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 4 || fields[1] == "" {
			continue
		}
		id, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		attached, _ := strconv.Atoi(fields[3])
		sessions = append(sessions, Session{
			Name:     fields[0],
			ID:       id,
			Path:     fields[2],
			Attached: attached > 0,
		})
	}
	return sessions
}
