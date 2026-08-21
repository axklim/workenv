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

func TestRealOutputPassStderrReturnsTrimmedStdout(t *testing.T) {
	out, err := Real{}.OutputPassStderr("", "sh", "-c", "echo hello; echo oops >&2")
	if err != nil {
		t.Fatalf("OutputPassStderr error: %v", err)
	}
	if out != "hello" {
		t.Errorf("OutputPassStderr = %q, want %q (trimmed, stderr excluded)", out, "hello")
	}
}

func TestRealOutputPassStderrErrorIncludesCommand(t *testing.T) {
	_, err := Real{}.OutputPassStderr("", "sh", "-c", "exit 1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "sh -c exit 1") {
		t.Errorf("error %q does not include the command", err.Error())
	}
}

func TestRealOutputWithStdinReturnsTrimmedStdout(t *testing.T) {
	out, err := Real{}.OutputWithStdin("", "echo", "hello")
	if err != nil {
		t.Fatalf("OutputWithStdin error: %v", err)
	}
	if out != "hello" {
		t.Errorf("OutputWithStdin = %q, want %q (trimmed)", out, "hello")
	}
}

func TestRealOutputWithStdinErrorIncludesStderr(t *testing.T) {
	_, err := Real{}.OutputWithStdin("", "sh", "-c", "echo oops >&2; exit 1")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "oops") {
		t.Errorf("error %q does not include stderr", err.Error())
	}
}

func TestFakeRecordsOutputWithStdinMethod(t *testing.T) {
	f := &Fake{Responses: []FakeResponse{
		{Prefix: "stty -g", Out: "saved-settings"},
	}}
	out, err := f.OutputWithStdin("", "stty", "-g")
	if err != nil {
		t.Fatalf("OutputWithStdin error: %v", err)
	}
	if out != "saved-settings" {
		t.Errorf("OutputWithStdin = %q", out)
	}
	if len(f.Calls) != 1 || f.Calls[0].Method != "OutputWithStdin" {
		t.Fatalf("Calls = %+v, want one call with Method OutputWithStdin", f.Calls)
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
