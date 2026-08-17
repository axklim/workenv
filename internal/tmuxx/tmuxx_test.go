package tmuxx

import (
	"testing"

	"workenv/internal/execx"
)

func TestParseSessions(t *testing.T) {
	raw := "trade-review-claude-md-file\t7\t/p/wt\t1\n" +
		"personal\t\t\t0\n" +
		"trade-pr-77\t8\t/p/wt2\t0\n"
	sessions := parseSessions(raw)
	if len(sessions) != 2 {
		t.Fatalf("got %d workenv sessions, want 2 (untagged filtered): %+v", len(sessions), sessions)
	}
	want := Session{Name: "trade-review-claude-md-file", ID: 7, Path: "/p/wt", Attached: true}
	if sessions[0] != want {
		t.Errorf("sessions[0] = %+v, want %+v", sessions[0], want)
	}
	if sessions[1].ID != 8 || sessions[1].Attached {
		t.Errorf("sessions[1] = %+v", sessions[1])
	}
}

func TestIsWorkenv(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "tmux show-options -t tagged @workenv", Out: "@workenv 1"},
		{Prefix: "tmux show-options -t untagged @workenv", Out: ""},
	}}
	tm := Tmux{R: fake}
	if !tm.IsWorkenv("tagged") {
		t.Error("tagged session should report IsWorkenv")
	}
	if tm.IsWorkenv("untagged") {
		t.Error("untagged session should not report IsWorkenv")
	}
}

func TestNewTagsSessionWithID(t *testing.T) {
	fake := &execx.Fake{}
	if err := (Tmux{R: fake}).New("trade-main", "/wt", 7); err != nil {
		t.Fatalf("New error: %v", err)
	}
	for _, want := range []string{
		"tmux new-session -d -s trade-main -c /wt",
		"tmux set-option -t trade-main @workenv 1",
		"tmux set-option -t trade-main @workenv_id 7",
	} {
		found := false
		for _, c := range fake.Joined() {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing call %q in %v", want, fake.Joined())
		}
	}
}
