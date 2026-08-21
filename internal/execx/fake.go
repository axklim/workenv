package execx

import "strings"

// Fake is a scripted Runner for tests. Commands are matched by prefix of
// "name arg arg ..."; unmatched commands succeed with empty output.
type Fake struct {
	Calls     []FakeCall
	Responses []FakeResponse
}

type FakeCall struct {
	// Method is which Runner method recorded the call ("Output", "Run" or
	// "OutputPassStderr"), so a test can assert the caller used the one it
	// meant to, not just the argv.
	Method string
	Dir    string
	Argv   []string
}

type FakeResponse struct {
	Prefix string
	Out    string
	Err    error
}

func (f *Fake) Output(dir, name string, args ...string) (string, error) {
	return f.record("Output", dir, name, args)
}

// OutputPassStderr behaves exactly like Output for a Fake — there is no
// real stderr stream to pass through in a test — but records the call
// under its own Method so a test can tell the two apart.
func (f *Fake) OutputPassStderr(dir, name string, args ...string) (string, error) {
	return f.record("OutputPassStderr", dir, name, args)
}

// OutputWithStdin behaves exactly like Output for a Fake — there is no
// terminal to attach in a test — but records the call under its own Method.
func (f *Fake) OutputWithStdin(dir, name string, args ...string) (string, error) {
	return f.record("OutputWithStdin", dir, name, args)
}

func (f *Fake) Run(dir, name string, args ...string) error {
	_, err := f.record("Run", dir, name, args)
	return err
}

// Joined renders each recorded call as a single "name arg arg" string.
func (f *Fake) Joined() []string {
	out := make([]string, len(f.Calls))
	for i, c := range f.Calls {
		out[i] = strings.Join(c.Argv, " ")
	}
	return out
}

func (f *Fake) record(method, dir, name string, args []string) (string, error) {
	argv := append([]string{name}, args...)
	f.Calls = append(f.Calls, FakeCall{Method: method, Dir: dir, Argv: argv})
	joined := strings.Join(argv, " ")
	for _, r := range f.Responses {
		if strings.HasPrefix(joined, r.Prefix) {
			return r.Out, r.Err
		}
	}
	return "", nil
}
