package kscript

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestHelperProcess is not a real test. The exec boundary tests re-run the test
// binary as a child process and print the argv it received, so argument
// boundaries can be asserted without depending on a platform specific echo.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("KS_ARGV_HELPER") != "1" {
		t.Skip("helper process fixture")
	}
	payload, err := json.Marshal(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Println(string(payload))
	os.Exit(0)
}

func TestExecKeepsArgumentBoundaries(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	payload := []string{
		"a b",              // space inside one argument
		`he said "hi"`,     // quotes inside one argument
		"",                 // empty argument
		"$HOME",            // no environment expansion
		`C:\temp\new file`, // Windows style path with a space
		"--flag=1",         // flag-like argument
		"中文参数",             // non-ASCII
		"*.go",             // no glob expansion
		"one\ttab",         // tab inside one argument
	}
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{{Exec: &ExecSpec{
			Program: executable,
			Args:    append([]string{"-test.run=TestHelperProcess", "--"}, payload...),
		}}}}},
	})
	result := mustRun(t, runner, Request{
		Task: "t",
		Env:  map[string]string{"KS_ARGV_HELPER": "1"},
		IO:   IO{CaptureLimit: 1 << 16},
	})
	var got []string
	if err := json.Unmarshal(result.Steps[0].Output, &got); err != nil {
		t.Fatalf("helper output=%q err=%v", result.Steps[0].Output, err)
	}
	// The child receives the test flags first, then the payload verbatim.
	want := append([]string{"-test.run=TestHelperProcess", "--"}, payload...)
	if len(got) != len(want) {
		t.Fatalf("argv=%q want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv[%d]=%q want %q (full argv %q)", i, got[i], want[i], got)
		}
	}
}

func TestFileActionUsesRegisteredInterpreter(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a Go program")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain is unavailable")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "scripts", "hello.go")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc main() { fmt.Println(\"script\", os.Args[1:]) }\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := newTestRunner(t, Definition{
		BaseDir: dir,
		Files: map[string]ScriptFile{
			"hello": {Path: "scripts/hello.go", Interpreter: Interpreter{Program: goBin, PrefixArgs: []string{"run"}}},
		},
		Tasks: map[string]Task{"t": {Steps: []Step{{File: &FileSpec{Name: "hello", Args: []string{"--check"}}}}}},
	})
	result := mustRun(t, runner, Request{Task: "t", IO: IO{CaptureLimit: 1 << 16}})
	output := string(result.Steps[0].Output)
	if !strings.Contains(output, "script [--check]") {
		t.Fatalf("output=%q", output)
	}
}

func TestShellPrefixArgsAreApplied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix shell fixture")
	}
	dir := t.TempDir()
	runner := newTestRunner(t, Definition{
		BaseDir: dir,
		Tasks: map[string]Task{"t": {Steps: []Step{{Shell: &ShellSpec{
			Name:       "sh",
			PrefixArgs: []string{"-x"},
			Script:     "printf ok",
		}}}}},
	})
	result := mustRun(t, runner, Request{Task: "t", IO: IO{CaptureLimit: 1 << 12}})
	if string(result.Steps[0].Output) != "ok" {
		t.Fatalf("output=%q", result.Steps[0].Output)
	}
	// -x makes the shell trace to stderr, proving the prefix argument was used.
	if !strings.Contains(string(result.Steps[0].ErrorOutput), "printf") {
		t.Fatalf("prefix args were not applied: %q", result.Steps[0].ErrorOutput)
	}
}

func TestShellSourceIsNotGuessedFromExtension(t *testing.T) {
	// A file action never chooses an interpreter from the extension; only the
	// registered interpreter is used.
	runner := newTestRunner(t, Definition{
		BaseDir: t.TempDir(),
		Files: map[string]ScriptFile{
			"weird": {Path: "weird.unknown", Interpreter: Interpreter{Program: "go", PrefixArgs: []string{"version"}}},
		},
		Tasks: map[string]Task{"t": {Steps: []Step{{File: &FileSpec{Name: "weird"}}}}},
	})
	plan, err := runner.Inspect(context.Background(), Request{Task: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Actions[0].Program != "go" {
		t.Fatalf("plan=%+v", plan.Actions)
	}
}
