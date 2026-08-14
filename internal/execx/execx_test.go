package execx

import (
	"strings"
	"testing"
)

func TestRealOutput(t *testing.T) {
	out, err := Real{}.Output("", "echo", "hello")
	if err != nil {
		t.Fatalf("Output error: %v", err)
	}
	if out != "hello" {
		t.Errorf("Output = %q, want %q (trimmed)", out, "hello")
	}
}

func TestRealOutputErrorIncludesStderr(t *testing.T) {
	_, err := Real{}.Output("", "sh", "-c", "echo oops >&2; exit 1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "oops") {
		t.Errorf("error %q does not include stderr", err.Error())
	}
}

func TestFakeMatchesByPrefixAndRecords(t *testing.T) {
	f := &Fake{Responses: []FakeResponse{
		{Prefix: "git worktree list", Out: "some output"},
	}}
	out, err := f.Output("/repo", "git", "worktree", "list", "--porcelain")
	if err != nil {
		t.Fatalf("Output error: %v", err)
	}
	if out != "some output" {
		t.Errorf("Output = %q", out)
	}
	if len(f.Calls) != 1 || f.Calls[0].Dir != "/repo" {
		t.Fatalf("Calls = %+v", f.Calls)
	}
	if got := f.Joined()[0]; got != "git worktree list --porcelain" {
		t.Errorf("Joined()[0] = %q", got)
	}
}
