// Package execx abstracts command execution behind a Runner so that flows
// touching git/tmux/gh/ssh can be tested with a fake.
package execx

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type Runner interface {
	// Output runs the command in dir (empty = inherit cwd) and returns
	// trimmed stdout; on failure the error includes stderr.
	Output(dir, name string, args ...string) (string, error)
	// OutputPassStderr is like Output — it captures and returns trimmed
	// stdout — but lets the command's stderr stream straight to the
	// user (os.Stderr) instead of being captured. Use it where the caller
	// needs to parse stdout but must not swallow the command's own
	// diagnostics when it succeeds (Output only surfaces stderr inside the
	// error it returns on failure).
	OutputPassStderr(dir, name string, args ...string) (string, error)
	// OutputWithStdin is like Output but leaves stdin attached to the
	// caller's, for commands that must talk to the terminal while their
	// stdout is parsed — stty reads the terminal from stdin and prints
	// settings (-g) or dimensions (size) to stdout.
	OutputWithStdin(dir, name string, args ...string) (string, error)
	// Run runs the command in dir streaming output to the user's terminal.
	Run(dir, name string, args ...string) error
}

type Real struct{}

func (Real) Output(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

func (Real) OutputPassStderr(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (Real) OutputWithStdin(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

func (Real) Run(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
