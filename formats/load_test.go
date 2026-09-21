package formats

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gookit/taskrun"
)

// fullYAML is the schema documented by the design, including steps, files,
// dynamic variables, conditions and namespaced templates.
const fullYAML = `
version: 1
vars:
  target: ./...
env:
  KS_ROOT: "."
env_paths: ["/opt/definition-bin"]
tasks:
  check:
    desc: check the project
    deps: [test]
    steps:
      - exec:
          program: go
          args: [vet, "${vars.target}"]
  test:
    steps:
      - exec:
          program: go
          args: [test, "${vars.target}"]
  inspect-env:
    if: vars.enabled == true
    vars:
      enabled: true
    steps:
      - shell:
          name: sh
          script: 'printf "%s\n" "$KS_LABEL"'
        env:
          KS_LABEL: "${vars.target}"
  generate:
    steps:
      - file:
          name: generator
          args: [--check]
  stamped:
    timeout: 2s
    dynamic_vars:
      revision:
        exec:
          program: git
          args: [rev-parse, --short, HEAD]
    steps:
      - host:
          name: app.check
          args: ["${vars.revision}"]
      - name: tolerant
        ignore_error: true
        platform: [linux, darwin, windows]
        timeout: 500ms
        exec:
          program: go
          args: [version]
files:
  generator:
    path: scripts/generate.go
    interpreter:
      program: go
      prefix_args: [run]
`

func TestLoadFullSchema(t *testing.T) {
	base := t.TempDir()
	def, err := Load(".yaml", strings.NewReader(fullYAML), "full.yaml", base)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if def.Version != 1 || def.BaseDir != base {
		t.Fatalf("def=%+v", def)
	}
	if def.Vars["target"] != "./..." || def.Env["KS_ROOT"] != "." {
		t.Fatalf("vars/env=%+v %+v", def.Vars, def.Env)
	}
	if len(def.EnvPaths) != 1 || def.EnvPaths[0] != "/opt/definition-bin" {
		t.Fatalf("env_paths=%v", def.EnvPaths)
	}
	check := def.Tasks["check"]
	if check.Desc != "check the project" || len(check.Deps) != 1 || check.Deps[0] != "test" {
		t.Fatalf("check=%+v", check)
	}
	if check.Steps[0].Exec.Program != "go" || check.Steps[0].Exec.Args[1] != "${vars.target}" {
		t.Fatalf("step=%+v", check.Steps[0])
	}
	inspect := def.Tasks["inspect-env"]
	if inspect.If != "vars.enabled == true" || inspect.Vars["enabled"] != true {
		t.Fatalf("inspect-env=%+v", inspect)
	}
	if inspect.Steps[0].Env["KS_LABEL"] != "${vars.target}" {
		t.Fatalf("step env=%v", inspect.Steps[0].Env)
	}
	if def.Tasks["generate"].Steps[0].File.Name != "generator" {
		t.Fatalf("generate=%+v", def.Tasks["generate"])
	}
	stamped := def.Tasks["stamped"]
	if stamped.Timeout != 2*time.Second {
		t.Fatalf("task timeout=%v", stamped.Timeout)
	}
	if stamped.DynamicVars["revision"].Exec.Program != "git" {
		t.Fatalf("dynamic vars=%+v", stamped.DynamicVars)
	}
	if stamped.Steps[1].Timeout != 500*time.Millisecond || !stamped.Steps[1].IgnoreError {
		t.Fatalf("step=%+v", stamped.Steps[1])
	}
	file := def.Files["generator"]
	if file.Path != filepath.Join(base, "scripts/generate.go") {
		t.Fatalf("file path=%s", file.Path)
	}
	if file.Interpreter.Program != "go" || len(file.Interpreter.PrefixArgs) != 1 {
		t.Fatalf("interpreter=%+v", file.Interpreter)
	}
	if len(def.Sources) != 1 || def.Sources[0].Name != "full.yaml" || def.Sources[0].Format != FormatYAML {
		t.Fatalf("sources=%+v", def.Sources)
	}
}

// TestDesignDefinitionIsAccepted verifies that the documented schema also
// passes library validation.
func TestDesignDefinitionIsAccepted(t *testing.T) {
	base := t.TempDir()
	def, err := Load(".yaml", strings.NewReader(fullYAML), "full.yaml", base)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := taskrun.New(def, taskrun.WithHandler("app.check", func(context.Context, taskrun.HostCall) (taskrun.ActionResult, error) {
		return taskrun.ActionResult{}, nil
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	plan, err := runner.Inspect(context.Background(), taskrun.Request{Task: "check"})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(plan.Actions) != 2 {
		t.Fatalf("plan=%+v", plan)
	}
}

const equivalenceYAML = `
version: 1
vars:
  target: ./...
tasks:
  check:
    if: "true"
    deps: [test]
    env:
      A: b
    steps:
      - exec:
          program: go
          args: [vet]
  test:
    steps:
      - host:
          name: noop
          args: [1, "two", true]
`

const equivalenceJSON = `{
  "version": 1,
  "vars": {"target": "./..."},
  "tasks": {
    "check": {
      "if": "true",
      "deps": ["test"],
      "env": {"A": "b"},
      "steps": [{"exec": {"program": "go", "args": ["vet"]}}]
    },
    "test": {
      "steps": [{"host": {"name": "noop", "args": [1, "two", true]}}]
    }
  }
}`

const equivalenceTOML = `
version = 1

[vars]
target = "./..."

[tasks.check]
if = "true"
deps = ["test"]

[tasks.check.env]
A = "b"

[[tasks.check.steps]]

[tasks.check.steps.exec]
program = "go"
args = ["vet"]

[[tasks.test.steps]]

[tasks.test.steps.host]
name = "noop"
args = [1, "two", true]
`

func TestFormatsProduceEqualModels(t *testing.T) {
	base := t.TempDir()
	yamlDef, err := Load(".yaml", strings.NewReader(equivalenceYAML), "eq.yaml", base)
	if err != nil {
		t.Fatalf("yaml: %v", err)
	}
	jsonDef, err := Load(".json", strings.NewReader(equivalenceJSON), "eq.json", base)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	tomlDef, err := Load(".toml", strings.NewReader(equivalenceTOML), "eq.toml", base)
	if err != nil {
		t.Fatalf("toml: %v", err)
	}
	yamlDef.Sources, jsonDef.Sources, tomlDef.Sources = nil, nil, nil
	if !reflect.DeepEqual(yamlDef, jsonDef) {
		t.Fatalf("yaml/json differ:\n%+v\n%+v", yamlDef, jsonDef)
	}
	if !reflect.DeepEqual(yamlDef, tomlDef) {
		t.Fatalf("yaml/toml differ:\n%+v\n%+v", yamlDef, tomlDef)
	}
}

func TestLoadRejectsInvalidDocuments(t *testing.T) {
	base := t.TempDir()
	cases := []struct {
		name    string
		ext     string
		body    string
		wantSub string
	}{
		{"unknown top field", ".json", `{"tasks":{"x":{"steps":[{"exec":{"program":"go"}}]}},"nope":1}`, "unknown field"},
		{"unknown task field", ".json", `{"tasks":{"x":{"steps":[{"exec":{"program":"go"}}],"stepz":1}}}`, "unknown field"},
		{"unknown step field", ".json", `{"tasks":{"x":{"steps":[{"execz":{"program":"go"}}]}}}`, "unknown field"},
		{"duplicate json key", ".json", `{"tasks":{"x":{"steps":[{"exec":{"program":"go"}}]},"x":{"steps":[]}}}`, "duplicate key"},
		{"duplicate yaml key", ".yaml", "tasks:\n  x:\n    steps:\n      - exec: {program: go}\n  x:\n    steps: []\n", ""},
		{"missing tasks", ".json", `{"version":1}`, "tasks is required"},
		{"bad version", ".json", `{"version":2,"tasks":{}}`, "unsupported schema version"},
		{"step not object", ".json", `{"tasks":{"x":{"steps":["echo"]}}}`, "step must be an object"},
		{"task not object", ".json", `{"tasks":{"x":"echo"}}`, "task must be an object"},
		{"two actions", ".json", `{"tasks":{"x":{"steps":[{"exec":{"program":"go"},"shell":{"name":"sh","script":"true"}}]}}}`, ""},
		{"missing program", ".json", `{"tasks":{"x":{"steps":[{"exec":{"args":["a"]}}]}}}`, "program is required"},
		{"shell without name", ".json", `{"tasks":{"x":{"steps":[{"shell":{"script":"true"}}]}}}`, "name is required"},
		{"host without name", ".json", `{"tasks":{"x":{"steps":[{"host":{"args":[]}}]}}}`, "name is required"},
		{"file without name", ".json", `{"tasks":{"x":{"steps":[{"file":{"args":[]}}]}}}`, "name is required"},
		{"task call without name", ".json", `{"tasks":{"x":{"steps":[{"task":{"args":[]}}]}}}`, "name is required"},
		{"deps not array", ".json", `{"tasks":{"x":{"deps":"test","steps":[]}}}`, "must be an array"},
		{"timeout not duration", ".json", `{"tasks":{"x":{"timeout":5,"steps":[{"exec":{"program":"go"}}]}}}`, "duration string"},
		{"timeout invalid", ".json", `{"tasks":{"x":{"timeout":"5 parsecs","steps":[{"exec":{"program":"go"}}]}}}`, "not a valid duration"},
		{"file escape", ".json", `{"files":{"g":{"path":"../escape.go","interpreter":{"program":"go"}}},"tasks":{"x":{"steps":[]}}}`, "escapes the source base directory"},
		{"file missing interpreter", ".json", `{"files":{"g":{"path":"g.go"}},"tasks":{"x":{"steps":[]}}}`, "interpreter is required"},
		{"bad env type", ".json", `{"env":{"A":1},"tasks":{"x":{"steps":[{"exec":{"program":"go"}}]}}}`, "must be a string"},
		{"bad dynamic action", ".json", `{"tasks":{"x":{"dynamic_vars":{"v":{}},"steps":[]}}}`, ""},
		{"empty names", ".json", `{"tasks":{"":{"steps":[]}}}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(tc.ext, strings.NewReader(tc.body), "case"+tc.ext, base)
			if err == nil {
				t.Fatalf("expected an error")
			}
			if !strings.Contains(err.Error(), "load case"+tc.ext) {
				t.Fatalf("error does not locate the source: %v", err)
			}
			if tc.wantSub != "" && !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not mention %q", err, tc.wantSub)
			}
		})
	}
}

func TestLoadErrorReportsFieldPath(t *testing.T) {
	base := t.TempDir()
	body := `{"tasks":{"build":{"steps":[{"exec":{"program":"go"},"bogus":1}]}}}`
	_, err := Load(".json", strings.NewReader(body), "path.json", base)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "tasks.build.steps[0]") {
		t.Fatalf("field path missing: %v", err)
	}
}

func TestLoadRejectsRelativeBaseDir(t *testing.T) {
	if _, err := Load(".json", strings.NewReader(`{"tasks":{}}`), "x.json", "."); err == nil {
		t.Fatal("expected an absolute baseDir requirement")
	}
}

func TestLoadRejectsUnsupportedFormat(t *testing.T) {
	if _, err := Load(".ini", strings.NewReader("x=1"), "x.ini", t.TempDir()); err == nil {
		t.Fatal("expected an unsupported format error")
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func TestLoadFileUsesAbsoluteBaseDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yaml")
	if err := writeFile(path, "version: 1\ntasks:\n  x:\n    steps:\n      - exec: {program: go}\n"); err != nil {
		t.Fatal(err)
	}
	def, err := LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if def.BaseDir != dir {
		t.Fatalf("baseDir=%s want %s", def.BaseDir, dir)
	}
}
