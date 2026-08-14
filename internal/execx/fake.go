package execx

import "strings"

// Fake is a scripted Runner for tests. Commands are matched by prefix of
// "name arg arg ..."; unmatched commands succeed with empty output.
type Fake struct {
	Calls     []FakeCall
	Responses []FakeResponse
}

type FakeCall struct {
	Dir  string
	Argv []string
}

type FakeResponse struct {
	Prefix string
	Out    string
	Err    error
}

func (f *Fake) Output(dir, name string, args ...string) (string, error) {
	return f.record(dir, name, args)
}

func (f *Fake) Run(dir, name string, args ...string) error {
	_, err := f.record(dir, name, args)
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

func (f *Fake) record(dir, name string, args []string) (string, error) {
	argv := append([]string{name}, args...)
	f.Calls = append(f.Calls, FakeCall{Dir: dir, Argv: argv})
	joined := strings.Join(argv, " ")
	for _, r := range f.Responses {
		if strings.HasPrefix(joined, r.Prefix) {
			return r.Out, r.Err
		}
	}
	return "", nil
}
