// Package term is the terminal plumbing under `we ui`: raw mode entered and
// left with stty (kept on execx.Runner like every other external command, so
// the whole UI is drivable by the scripted fake), the terminal size, and
// keypress decoding from a byte stream.
package term

import (
	"bufio"
	"fmt"

	"workenv/internal/execx"
)

// Key is one decoded keypress.
type Key int

const (
	KeyRune Key = iota // a printable byte; ReadKey returns it alongside
	KeyUp
	KeyDown
	KeyEnter
	KeyBackspace
	KeyCtrlC
	KeyEsc
	KeyEOF
	KeyOther // decoded but unmapped (an unknown control byte or CSI sequence)
)

// ReadKey decodes one keypress. A CSI sequence is consumed through its final
// byte even when unmapped, so the key after it is not misread as part of it;
// input ending mid-sequence decodes as KeyEsc.
func ReadKey(r *bufio.Reader) (Key, byte) {
	b, err := r.ReadByte()
	if err != nil {
		return KeyEOF, 0
	}
	switch b {
	case 0x03:
		return KeyCtrlC, 0
	case '\r', '\n':
		return KeyEnter, 0
	case 0x7f, 0x08:
		return KeyBackspace, 0
	case 0x1b:
		return readEscape(r)
	}
	if b >= 0x20 {
		return KeyRune, b
	}
	return KeyOther, 0
}

// readEscape decodes what follows an ESC byte: a CSI (or SS3) sequence read
// through its final byte, or a lone escape.
func readEscape(r *bufio.Reader) (Key, byte) {
	b, err := r.ReadByte()
	if err != nil {
		return KeyEsc, 0
	}
	if b != '[' && b != 'O' {
		return KeyEsc, 0
	}
	for {
		b, err = r.ReadByte()
		if err != nil {
			return KeyEsc, 0
		}
		if b >= 0x40 && b <= 0x7e { // the sequence's final byte
			break
		}
	}
	switch b {
	case 'A':
		return KeyUp, 0
	case 'B':
		return KeyDown, 0
	}
	return KeyOther, 0
}

// Raw switches the terminal on stdin into raw mode and back.
type Raw struct {
	R     execx.Runner
	saved string
}

// Enter saves the current settings (stty -g) and applies raw -echo.
func (r *Raw) Enter() error {
	saved, err := r.R.OutputWithStdin("", "stty", "-g")
	if err != nil {
		return fmt.Errorf("stty -g (is stdin a terminal?): %w", err)
	}
	r.saved = saved
	if _, err := r.R.OutputWithStdin("", "stty", "raw", "-echo"); err != nil {
		return err
	}
	return nil
}

// Exit restores the settings Enter saved, falling back to `stty sane` when
// there are none. Errors are ignored: Exit runs on the way out, often after
// a real failure that must not be masked by a restore hiccup.
func (r *Raw) Exit() {
	if r.saved != "" {
		_, _ = r.R.OutputWithStdin("", "stty", r.saved)
		return
	}
	_, _ = r.R.OutputWithStdin("", "stty", "sane")
}

// Size reports the terminal dimensions from `stty size`, falling back to
// 24x80 when they cannot be read.
func Size(r execx.Runner) (rows, cols int) {
	out, err := r.OutputWithStdin("", "stty", "size")
	if err == nil {
		if _, err := fmt.Sscanf(out, "%d %d", &rows, &cols); err == nil && rows > 0 && cols > 0 {
			return rows, cols
		}
	}
	return 24, 80
}
