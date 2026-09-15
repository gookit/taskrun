package formats

import (
	"strings"
	"testing"
)

func TestLoadRejectsUnknownField(t *testing.T) {
	_, err := Load(".json", strings.NewReader(`{"tasks":{"x":"echo"},"bad":true}`), "test.json", ".")
	if err == nil {
		t.Fatal("expected unknown field error")
	}
}

func TestLoadRejectsMissingTasks(t *testing.T) {
	_, err := Load(".json", strings.NewReader(`{"version":1}`), "test.json", ".")
	if err == nil {
		t.Fatal("expected missing tasks error")
	}
}

func TestLoadCondition(t *testing.T) {
	d, err := Load(".json", strings.NewReader(`{"version":1,"tasks":{"x":{"if":"enabled","run":"echo"}}}`), "test.json", ".")
	if err != nil || d.Tasks["x"].If != "enabled" {
		t.Fatalf("definition=%+v err=%v", d, err)
	}
}

func TestLoadDependencies(t *testing.T) {
	d, err := Load(".json", strings.NewReader(`{"version":1,"tasks":{"build":{"deps":["test"],"run":"echo"},"test":"echo"}}`), "test.json", ".")
	if err != nil || len(d.Tasks["build"].Deps) != 1 || d.Tasks["build"].Deps[0] != "test" {
		t.Fatalf("definition=%+v err=%v", d, err)
	}
}

func TestLoadRejectsInvalidDependencies(t *testing.T) {
	_, err := Load(".json", strings.NewReader(`{"version":1,"tasks":{"build":{"deps":"test"}}}`), "test.json", ".")
	if err == nil {
		t.Fatal("expected deps type error")
	}
}

func TestLoadYAMLAndTOML(t *testing.T) {
	yamlDef, err := Load(".yaml", strings.NewReader("version: 1\ntasks:\n  check:\n    run: echo\n"), "test.yaml", ".")
	if err != nil || yamlDef.Tasks["check"].Name != "check" {
		t.Fatalf("yaml=%+v err=%v", yamlDef, err)
	}
	tomlDef, err := Load(".toml", strings.NewReader("version = 1\n[tasks.check]\nrun = \"echo\"\n"), "test.toml", ".")
	if err != nil || tomlDef.Tasks["check"].Name != "check" {
		t.Fatalf("toml=%+v err=%v", tomlDef, err)
	}
}
