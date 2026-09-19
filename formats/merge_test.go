package formats

import (
	"strings"
	"testing"
)

func mergeDefinitions(t *testing.T, bodies []string) ([]string, error) {
	t.Helper()
	paths := make([]string, 0, len(bodies))
	dir := t.TempDir()
	for i, body := range bodies {
		path := dir + "/def" + string(rune('0'+i)) + ".yaml"
		if err := writeFile(path, body); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func TestMergeRejectsTaskConflict(t *testing.T) {
	paths, err := mergeDefinitions(t, []string{
		"version: 1\ntasks:\n  same:\n    steps:\n      - exec: {program: go}\n",
		"version: 1\ntasks:\n  same:\n    steps:\n      - exec: {program: go}\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = LoadFiles(paths, false)
	if err == nil {
		t.Fatal("expected a conflict error")
	}
	if !strings.Contains(err.Error(), "same") || !strings.Contains(err.Error(), "defined by both") {
		t.Fatalf("unhelpful conflict error: %v", err)
	}
}

func TestMergeOverrideReplacesWholeTask(t *testing.T) {
	paths, err := mergeDefinitions(t, []string{
		"version: 1\ntasks:\n  build:\n    desc: first\n    steps:\n      - exec: {program: old}\n",
		"version: 1\ntasks:\n  build:\n    steps:\n      - exec: {program: new}\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	def, err := LoadFiles(paths, true)
	if err != nil {
		t.Fatal(err)
	}
	task := def.Tasks["build"]
	// Replacement is whole-task: the first description must not survive.
	if task.Desc != "" {
		t.Fatalf("task was merged field by field: %+v", task)
	}
	if task.Steps[0].Exec.Program != "new" {
		t.Fatalf("task=%+v", task)
	}
}

func TestMergeRejectsFileConflict(t *testing.T) {
	paths, err := mergeDefinitions(t, []string{
		"version: 1\nfiles:\n  g:\n    path: a.go\n    interpreter: {program: go}\ntasks:\n  t:\n    steps: [{exec: {program: go}}]\n",
		"version: 1\nfiles:\n  g:\n    path: b.go\n    interpreter: {program: go}\ntasks:\n  u:\n    steps: [{exec: {program: go}}]\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFiles(paths, false); err == nil {
		t.Fatal("expected a script file conflict")
	}
	if _, err := LoadFiles(paths, true); err != nil {
		t.Fatalf("override should accept the later file: %v", err)
	}
}

func TestMergeOverridesVarsAndEnvByKey(t *testing.T) {
	paths, err := mergeDefinitions(t, []string{
		"version: 1\nvars:\n  a: 1\n  b: 1\nenv:\n  A: \"1\"\ntasks:\n  t:\n    steps: [{exec: {program: go}}]\n",
		"version: 1\nvars:\n  b: 2\nenv:\n  B: \"2\"\ntasks:\n  u:\n    steps: [{exec: {program: go}}]\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	def, err := LoadFiles(paths, false)
	if err != nil {
		t.Fatal(err)
	}
	if def.Vars["a"] != int64(1) || def.Vars["b"] != int64(2) {
		t.Fatalf("vars=%v", def.Vars)
	}
	if def.Env["A"] != "1" || def.Env["B"] != "2" {
		t.Fatalf("env=%v", def.Env)
	}
}

func TestMergeRejectsEmptyInput(t *testing.T) {
	if _, err := Merge(nil, false); err == nil {
		t.Fatal("expected an error")
	}
}
