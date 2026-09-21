package formats

import "testing"

// TestSplitCommandLineContract pins the quoting rules of the exported Kite
// compatibility helper. The library itself never splits an exec argument.
func TestSplitCommandLineContract(t *testing.T) {
	cases := []struct {
		line    string
		program string
		args    []string
		bad     bool
	}{
		{line: "go run", program: "go", args: []string{"run"}},
		{line: "  go   run  ", program: "go", args: []string{"run"}},
		{line: `sh -c "echo hi"`, program: "sh", args: []string{"-c", "echo hi"}},
		{line: `pwsh -NoProfile -File`, program: "pwsh", args: []string{"-NoProfile", "-File"}},
		{line: `cmd ""`, program: "cmd", args: []string{""}},
		{line: `cmd "a\"b"`, program: "cmd", args: []string{`a"b`}},
		{line: `cmd 'single quoted'`, program: "cmd", args: []string{"single quoted"}},
		{line: `cmd a\ b`, program: "cmd", args: []string{"a b"}},
		{line: `cmd trailing\`, program: "cmd", args: []string{`trailing\`}},
		{line: "cmd\ttab", program: "cmd", args: []string{"tab"}},
		{line: `cmd "unterminated`, bad: true},
		{line: "", program: "", args: nil},
		{line: "   ", program: "", args: nil},
	}

	for _, tc := range cases {
		program, args, err := SplitCommandLine(tc.line)
		if tc.bad {
			if err == nil {
				t.Errorf("SplitCommandLine(%q) accepted an unterminated quote", tc.line)
			}
			continue
		}
		if err != nil {
			t.Errorf("SplitCommandLine(%q): %v", tc.line, err)
			continue
		}
		if program != tc.program || len(args) != len(tc.args) {
			t.Errorf("SplitCommandLine(%q) = %q %q, want %q %q", tc.line, program, args, tc.program, tc.args)
			continue
		}
		for i := range args {
			if args[i] != tc.args[i] {
				t.Errorf("SplitCommandLine(%q) arg %d = %q, want %q", tc.line, i, args[i], tc.args[i])
			}
		}
	}
}
