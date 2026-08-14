package tmuxx

import "testing"

func TestParseSessions(t *testing.T) {
	raw := "we-borscht-feature-x\tborscht\tfeature-x\t/p/wt\t1\n" +
		"personal\t\t\t\t0\n" +
		"we-volt-issue-9-fix\tvolt\tissue-9-fix\t/p/wt2\t0\n"
	sessions := parseSessions(raw)
	if len(sessions) != 2 {
		t.Fatalf("got %d we sessions, want 2 (non-we filtered): %+v", len(sessions), sessions)
	}
	want := Session{Name: "we-borscht-feature-x", Project: "borscht", WeName: "feature-x", Path: "/p/wt", Attached: true}
	if sessions[0] != want {
		t.Errorf("sessions[0] = %+v, want %+v", sessions[0], want)
	}
	if sessions[1].Attached {
		t.Error("sessions[1] should be detached")
	}
}
