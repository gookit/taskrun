package formats

import (
	"reflect"
	"strings"
	"testing"
)

// TestTaskCallDecodingVariants covers the task call shape: args that replace the
// inherited list, args that inherit, and forward_args.
func TestTaskCallDecodingVariants(t *testing.T) {
	path := t.TempDir() + "/def.yaml"
	body := `
version: 1
tasks:
  a:
    steps:
      - exec: {program: go}
  b:
    steps:
      - task: {name: a, args: [--flag, "two words"], forward_args: true}
  c:
    steps:
      - task: {name: a, args: []}
  d:
    steps:
      - task: {name: a}
`
	if err := writeFile(path, body); err != nil {
		t.Fatal(err)
	}
	def, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	withArgs := def.Tasks["b"].Steps[0].Task
	if withArgs == nil || withArgs.Name != "a" || !withArgs.ForwardArgs {
		t.Fatalf("b call = %+v", withArgs)
	}
	if want := []string{"--flag", "two words"}; !reflect.DeepEqual(withArgs.Args, want) {
		t.Fatalf("b args = %#v, want %#v", withArgs.Args, want)
	}

	// A present but empty args list replaces the inherited arguments.
	empty := def.Tasks["c"].Steps[0].Task
	if empty.Args == nil || len(empty.Args) != 0 {
		t.Fatalf("c args = %#v, want a non-nil empty list", empty.Args)
	}

	// An omitted args key inherits, and forward_args defaults to false.
	inherited := def.Tasks["d"].Steps[0].Task
	if inherited.Args != nil {
		t.Fatalf("d args = %#v, want nil (inherit)", inherited.Args)
	}
	if inherited.ForwardArgs {
		t.Fatal("d must not forward args by default")
	}
}

// TestTaskCallDecodingRejectsBadShapes checks the strict decoding of the call
// shape.
func TestTaskCallDecodingRejectsBadShapes(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "missing name",
			body: "version: 1\ntasks:\n  a:\n    steps:\n      - exec: {program: go}\n  b:\n    steps:\n      - task: {args: [x]}\n",
			want: "name is required",
		},
		{
			name: "unknown key",
			body: "version: 1\ntasks:\n  a:\n    steps:\n      - exec: {program: go}\n  b:\n    steps:\n      - task: {name: a, extra: 1}\n",
			want: "extra",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := t.TempDir() + "/def.yaml"
			if err := writeFile(path, tc.body); err != nil {
				t.Fatal(err)
			}
			_, err := LoadFile(path)
			if err == nil {
				t.Fatal("expected a decoding error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}
