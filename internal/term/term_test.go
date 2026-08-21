package term

import (
	"bufio"
	"strings"
	"testing"

	"workenv/internal/execx"
)

func TestReadKeyDecodesBytes(t *testing.T) {
	cases := []struct {
		in   string
		key  Key
		b    byte
		name string
	}{
		{"j", KeyRune, 'j', "printable rune"},
		{"\r", KeyEnter, 0, "carriage return"},
		{"\x1b[A", KeyUp, 0, "arrow up"},
		{"\x1b[B", KeyDown, 0, "arrow down"},
		{"\x7f", KeyBackspace, 0, "DEL backspace"},
		{"\x08", KeyBackspace, 0, "BS backspace"},
		{"\x03", KeyCtrlC, 0, "ctrl-c"},
		{"", KeyEOF, 0, "end of input"},
		{"\x1b", KeyEsc, 0, "lone escape"},
		{"\x1b[C", KeyOther, 0, "unmapped escape sequence"},
	}
	for _, c := range cases {
		key, b := ReadKey(bufio.NewReader(strings.NewReader(c.in)))
		if key != c.key || b != c.b {
			t.Errorf("%s: ReadKey(%q) = (%v, %q), want (%v, %q)", c.name, c.in, key, b, c.key, c.b)
		}
	}
}

// TestReadKeyConsumesWholeEscapeSequence proves an unmapped CSI sequence is
// swallowed to its final byte, so the key after it is not misread as part of
// the sequence.
func TestReadKeyConsumesWholeEscapeSequence(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("\x1b[Cq"))
	if key, _ := ReadKey(r); key != KeyOther {
		t.Fatalf("first ReadKey = %v, want KeyOther", key)
	}
	key, b := ReadKey(r)
	if key != KeyRune || b != 'q' {
		t.Errorf("second ReadKey = (%v, %q), want (KeyRune, 'q')", key, b)
	}
}

func TestRawEnterAppliesRawAndExitRestoresSaved(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "stty -g", Out: "SAVED"},
	}}
	raw := &Raw{R: fake}
	if err := raw.Enter(); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	raw.Exit()
	want := []string{"stty -g", "stty raw -echo", "stty SAVED"}
	got := fake.Joined()
	if len(got) != len(want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, got[i], want[i])
		}
		if fake.Calls[i].Method != "OutputWithStdin" {
			t.Errorf("call %d Method = %q, want OutputWithStdin (stty needs the terminal on stdin)", i, fake.Calls[i].Method)
		}
	}
}

func TestRawExitFallsBackToSane(t *testing.T) {
	fake := &execx.Fake{} // unscripted stty -g yields no saved settings
	raw := &Raw{R: fake}
	if err := raw.Enter(); err != nil {
		t.Fatalf("Enter: %v", err)
	}
	raw.Exit()
	if got := fake.Joined()[len(fake.Calls)-1]; got != "stty sane" {
		t.Errorf("last call = %q, want %q", got, "stty sane")
	}
}

func TestSizeParsesSttySize(t *testing.T) {
	fake := &execx.Fake{Responses: []execx.FakeResponse{
		{Prefix: "stty size", Out: "40 120"},
	}}
	rows, cols := Size(fake)
	if rows != 40 || cols != 120 {
		t.Errorf("Size = (%d, %d), want (40, 120)", rows, cols)
	}
}

func TestSizeFallsBackTo24x80(t *testing.T) {
	rows, cols := Size(&execx.Fake{})
	if rows != 24 || cols != 80 {
		t.Errorf("Size = (%d, %d), want the 24x80 fallback", rows, cols)
	}
}
