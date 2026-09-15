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
