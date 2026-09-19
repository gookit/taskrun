package formats

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gookit/kscript"
)

// legacyFixture mirrors a real Kite task file: task settings, shorthand task
// shapes, command maps, @task references, backends, dynamic variables and
// legacy template syntax.
func legacyFixture() map[string]any {
	return map[string]any{
		"__settings": map[string]any{
			"vars": map[string]any{"project": "kite"},
			"groups": map[string]any{
				"dev": map[string]any{"env_name": "dev"},
			},
			"default_group": "dev",
			"env":           map[string]any{"KS_PROJECT": "${vars.project}"},
			"env_paths":     "/opt/kite/bin",
		},
		"simple":   "go version",
		"list":     []any{"go version", "go env GOOS"},
		"strlist":  []string{"go version"},
		"quotes":   `echo "hello world" 'second arg'`,
		"refer":    "@task:simple",
		"shellcmd": "@sh: printf '%s' \"$KS_LABEL\"",
		"execcmd":  "@exec: go env GOOS",
		"bare":     "@go version",
		"typed": map[string]any{
			"type":  "sh",
			"desc":  "typed shell task",
			"run":   "printf typed",
			"deps":  []any{"simple"},
			"dir":   "sub",
			"vars":  map[string]any{"stamp": "@exec: go env GOOS", "name": "value"},
			"env":   map[string]any{"KS_LABEL": "${vars.name}"},
			"alias": []any{"t"},
		},
		"structured": map[string]any{
			"timeout": "3s",
			"run": []any{
				map[string]any{"name": "first", "run": "go version"},
				map[string]any{
					"name":       "second",
					"run":        "printf %s $1",
					"vars":       map[string]any{"inner": "x"},
					"env":        map[string]any{"KS_INNER": "1"},
					"dir":        "work",
					"if":         `inner != ""`,
					"timeout":    "1s",
					"ignore_err": true,
				},
				map[string]any{"task": "simple"},
			},
		},
		"templates": map[string]any{
			"vars": map[string]any{"target": "./..."},
			"run":  []any{"echo ${vars.target}", "echo $target", "echo $1 $@ $*", "echo $HOME", "echo ${vars.target}"},
		},
	}
}

func convertFixture(t *testing.T, opts LegacyOptions) LegacyResult {
	t.Helper()
	if opts.BaseDir == "" {
		opts.BaseDir = t.TempDir()
	}
	if opts.Scripts == nil {
		opts.Scripts = legacyFixture()
	}
	if opts.RuntimeVars == nil {
		opts.RuntimeVars = []string{"target"}
	}
	result, err := LegacyDefinition(opts)
	if err != nil {
		t.Fatalf("LegacyDefinition: %v", err)
	}
	return result
}

func TestLegacySettingsBecomeDefaults(t *testing.T) {
	result := convertFixture(t, LegacyOptions{})
	def := result.Definition
	if def.Vars["project"] != "kite" || def.Vars["env_name"] != "dev" {
		t.Fatalf("vars=%v", def.Vars)
	}
	if def.Env["KS_PROJECT"] != "${vars.project}" {
		t.Fatalf("env=%v", def.Env)
	}
	if len(def.EnvPaths) != 1 || def.EnvPaths[0] != "/opt/kite/bin" {
		t.Fatalf("env_paths=%v", def.EnvPaths)
	}
}

func TestLegacyTaskShapes(t *testing.T) {
	def := convertFixture(t, LegacyOptions{}).Definition
	if steps := def.Tasks["simple"].Steps; len(steps) != 1 || steps[0].Exec.Program != "go" {
		t.Fatalf("simple=%+v", steps)
	}
	if steps := def.Tasks["list"].Steps; len(steps) != 2 {
		t.Fatalf("list=%+v", steps)
	}
	if steps := def.Tasks["strlist"].Steps; len(steps) != 1 {
		t.Fatalf("strlist=%+v", steps)
	}
	quoted := def.Tasks["quotes"].Steps[0].Exec
	if quoted.Args[0] != "hello world" || quoted.Args[1] != "second arg" {
		t.Fatalf("quoting lost: %+v", quoted)
	}
	refer := def.Tasks["refer"].Steps[0].Task
	if refer == nil || refer.Name != "simple" || !refer.ForwardArgs {
		t.Fatalf("refer=%+v", refer)
	}
}

func TestLegacyBackendPrefixes(t *testing.T) {
	def := convertFixture(t, LegacyOptions{}).Definition
	shellStep := def.Tasks["shellcmd"].Steps[0]
	if shellStep.Shell == nil || shellStep.Shell.Name != "sh" {
		t.Fatalf("shellcmd=%+v", shellStep)
	}
	if !strings.Contains(shellStep.Shell.Script, "$KS_LABEL") {
		t.Fatalf("script=%q", shellStep.Shell.Script)
	}
	execStep := def.Tasks["execcmd"].Steps[0]
	if execStep.Exec == nil || execStep.Exec.Program != "go" || execStep.Exec.Args[0] != "env" {
		t.Fatalf("execcmd=%+v", execStep.Exec)
	}
	// A bare "@" keeps safe-run behavior but drops the legacy silent flag.
	bare := def.Tasks["bare"].Steps[0]
	if bare.Exec == nil || !bare.IgnoreError {
		t.Fatalf("bare=%+v", bare)
	}
}

func TestLegacyTypedTask(t *testing.T) {
	result := convertFixture(t, LegacyOptions{})
	task := result.Definition.Tasks["typed"]
	if task.Desc != "typed shell task" || task.Dir != "sub" || len(task.Deps) != 1 {
		t.Fatalf("task=%+v", task)
	}
	if task.Steps[0].Shell == nil || task.Steps[0].Shell.Name != "sh" {
		t.Fatalf("typed step=%+v", task.Steps[0])
	}
	if task.Vars["name"] != "value" {
		t.Fatalf("task vars=%v", task.Vars)
	}
	if _, ok := task.DynamicVars["stamp"]; !ok {
		t.Fatalf("dynamic var missing: %+v", task.DynamicVars)
	}
	if task.Env["KS_LABEL"] != "${vars.name}" {
		t.Fatalf("env=%v", task.Env)
	}
	if !hasWarning(result.Warnings, "alias") {
		t.Fatalf("alias drop was not reported: %v", result.Warnings)
	}
}

func TestLegacyStructuredCommands(t *testing.T) {
	def := convertFixture(t, LegacyOptions{}).Definition
	task := def.Tasks["structured"]
	if task.Timeout.String() != "3s" {
		t.Fatalf("timeout=%v", task.Timeout)
	}
	if len(task.Steps) != 3 {
		t.Fatalf("steps=%+v", task.Steps)
	}
	second := task.Steps[1]
	if second.Name != "second" || second.Dir != "work" || !second.IgnoreError || second.Timeout.String() != "1s" {
		t.Fatalf("second=%+v", second)
	}
	if second.Env["KS_INNER"] != "1" || second.Vars["inner"] != "x" {
		t.Fatalf("second env/vars=%+v %+v", second.Env, second.Vars)
	}
	if second.If != `inner != ""` {
		t.Fatalf("if=%q", second.If)
	}
	if second.Exec.Args[0] != "%s" || second.Exec.Args[1] != "${args.1}" {
		t.Fatalf("args=%v", second.Exec.Args)
	}
	if task.Steps[2].Task == nil || task.Steps[2].Task.Name != "simple" {
		t.Fatalf("third=%+v", task.Steps[2])
	}
}

func TestLegacyTemplateTranslation(t *testing.T) {
	def := convertFixture(t, LegacyOptions{}).Definition
	steps := def.Tasks["templates"].Steps
	cases := []struct {
		index int
		want  []string
	}{
		{0, []string{"${vars.target}"}},
		{1, []string{"${vars.target}"}},
		{2, []string{"${args.1}", "${vars.@}", "${vars.*}"}},
		{3, []string{"$HOME"}},
	}
	for _, tc := range cases {
		got := strings.Join(steps[tc.index].Exec.Args, " ")
		if got != strings.Join(tc.want, " ") {
			t.Fatalf("step %d args=%q want %q", tc.index, got, strings.Join(tc.want, " "))
		}
	}
}

func TestLegacyConvertedDefinitionValidatesAndRuns(t *testing.T) {
	base := t.TempDir()
	result := convertFixture(t, LegacyOptions{BaseDir: base})
	runner, err := kscript.New(result.Definition)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	plan, err := runner.Inspect(context.Background(), kscript.Request{Task: "structured", Args: []string{"value"}})
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if len(plan.Actions) == 0 {
		t.Fatalf("plan=%+v", plan)
	}
}

// Legacy task and command variables are top level render variables, so
// references to them must be rewritten even though they are not runtime names.
func TestLegacyTaskVariableReferencesAreTranslated(t *testing.T) {
	scripts := map[string]any{"t": map[string]any{
		"vars": map[string]any{"who": "bridged"},
		"run": []any{
			map[string]any{
				"name": "first",
				"vars": map[string]any{"inner": "value"},
				"run":  "echo ${who}-${inner}",
				"env":  map[string]any{"KS_WHO": "${who}"},
			},
		},
	}}
	result := convertFixture(t, LegacyOptions{Scripts: scripts})
	step := result.Definition.Tasks["t"].Steps[0]
	if got := strings.Join(step.Exec.Args, " "); got != "${vars.who}-${vars.inner}" {
		t.Fatalf("args=%q", got)
	}
	if step.Env["KS_WHO"] != "${vars.who}" {
		t.Fatalf("env=%v", step.Env)
	}
}

func TestLegacyNestedPathsAreTranslated(t *testing.T) {
	scripts := map[string]any{"t": map[string]any{
		"run": "echo $gvs.app ${paths.tmp} $time.datetime $unknown.thing",
	}}
	result := convertFixture(t, LegacyOptions{
		Scripts:     scripts,
		RuntimeVars: []string{"gvs", "paths", "time"},
	})
	args := result.Definition.Tasks["t"].Steps[0].Exec.Args
	want := []string{"${vars.gvs.app}", "${vars.paths.tmp}", "${vars.time.datetime}", "$unknown.thing"}
	if len(args) != len(want) {
		t.Fatalf("args=%v want=%v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args=%v want=%v", args, want)
		}
	}
}

func hasWarning(warnings []string, needle string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, needle) {
			return true
		}
	}
	return false
}

func TestLegacyFilesUseExtensionInterpreter(t *testing.T) {
	base := t.TempDir()
	result := convertFixture(t, LegacyOptions{
		BaseDir: base,
		Files: map[string]LegacyScriptFile{
			"build.go": {Path: "scripts/build.go", Ext: ".go", Bin: "go run"},
			"run.sh":   {Path: "scripts/run.sh", Ext: ".sh"},
		},
	})
	goFile := result.Definition.Files["build.go"]
	if goFile.Path != filepath.Join(base, "scripts/build.go") {
		t.Fatalf("path=%s", goFile.Path)
	}
	// "go run" is split into program and prefix arguments.
	if goFile.Interpreter.Program != "go" || len(goFile.Interpreter.PrefixArgs) != 1 {
		t.Fatalf("interpreter=%+v", goFile.Interpreter)
	}
	shFile := result.Definition.Files["run.sh"]
	if shFile.Interpreter.Program != "sh" {
		t.Fatalf("interpreter=%+v", shFile.Interpreter)
	}
}

func TestLegacyPlatformOverrideAppliesForCurrentOS(t *testing.T) {
	scripts := map[string]any{
		"cross": map[string]any{
			"run": "go version",
			runtime.GOOS: map[string]any{
				"type": "sh",
				"run":  "printf platform",
			},
		},
	}
	result := convertFixture(t, LegacyOptions{Scripts: scripts})
	step := result.Definition.Tasks["cross"].Steps[0]
	if step.Shell == nil {
		t.Fatalf("platform override was not applied: %+v", step)
	}
	if !hasWarning(result.Warnings, "platform override") {
		t.Fatalf("platform override was not reported: %v", result.Warnings)
	}
}

func TestLegacyDefaultShellAppliesToPlainCommands(t *testing.T) {
	scripts := map[string]any{"plain": "printf %s value"}
	result := convertFixture(t, LegacyOptions{Scripts: scripts, DefaultShell: "sh"})
	step := result.Definition.Tasks["plain"].Steps[0]
	if step.Shell == nil || step.Shell.Name != "sh" {
		t.Fatalf("step=%+v", step)
	}
}

func TestLegacyMapKeepsBackwardCompatibleSignature(t *testing.T) {
	def, err := LegacyMap(map[string]any{"build": []string{"go test ./...", "go vet ./..."}}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(def.Tasks["build"].Steps) != 2 {
		t.Fatalf("steps=%+v", def.Tasks["build"].Steps)
	}
}

func TestLegacyRejectsInvalidInput(t *testing.T) {
	if _, err := LegacyDefinition(LegacyOptions{Scripts: map[string]any{}, BaseDir: "."}); err == nil {
		t.Fatal("expected an absolute BaseDir requirement")
	}
	if _, err := LegacyDefinition(LegacyOptions{BaseDir: t.TempDir(), Scripts: map[string]any{"x": 42}}); err == nil {
		t.Fatal("expected an unsupported task value error")
	}
	if _, err := LegacyDefinition(LegacyOptions{BaseDir: t.TempDir(), Scripts: map[string]any{"x": map[string]any{"timeout": "5 parsecs"}}}); err == nil {
		t.Fatal("expected an invalid timeout error")
	}
	if _, err := LegacyDefinition(LegacyOptions{BaseDir: t.TempDir(), Scripts: map[string]any{"x": "@exec: echo 'unterminated"}}); err == nil {
		t.Fatal("expected an unterminated quote error")
	}
}

func TestLegacyUnknownDynamicVarTypeStaysLiteral(t *testing.T) {
	scripts := map[string]any{"t": map[string]any{
		"vars": map[string]any{"v": "@go: whatever"},
		"run":  "echo ${vars.v}",
	}}
	def := convertFixture(t, LegacyOptions{Scripts: scripts}).Definition
	task := def.Tasks["t"]
	if len(task.DynamicVars) != 0 {
		t.Fatalf("unexpected dynamic vars: %+v", task.DynamicVars)
	}
	if task.Vars["v"] != "@go: whatever" {
		t.Fatalf("vars=%v", task.Vars)
	}
}
