package kscript

import (
	"context"
	"testing"
)

func TestConditionSkipAndRun(t *testing.T) {
	r, err := New(Definition{BaseDir: ".", Tasks: map[string]Task{
		"skip": {Name: "skip", If: "enabled", Steps: []Step{{Exec: &ExecSpec{Program: "definitely-not-run"}}}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := r.Run(context.Background(), Request{Task: "skip", Vars: map[string]any{"enabled": false}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusSkipped {
		t.Fatalf("status=%s", result.Status)
	}
}

func TestConditionMustBeBool(t *testing.T) {
	_, err := evalCondition("name", map[string]any{"name": "text"})
	if err == nil {
		t.Fatal("expected bool error")
	}
}
