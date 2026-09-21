package taskrun

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// TestShellArgumentContract pins the interpreter invocation each supported shell
// maps to. It needs no interpreter on the test host.
func TestShellArgumentContract(t *testing.T) {
	cases := []struct {
		shell   string
		program string
		args    []string
	}{
		{"sh", "sh", []string{"-c", "echo hi"}},
		{"bash", "bash", []string{"-c", "echo hi"}},
		{"zsh", "zsh", []string{"-c", "echo hi"}},
		{"cmd", "cmd.exe", []string{"/D", "/S", "/C", "echo hi"}},
		{"pwsh", "pwsh", []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "echo hi"}},
		{"powershell", "powershell", []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "echo hi"}},
	}
	for _, tc := range cases {
		program, args, err := shellCommand(tc.shell, "echo hi", nil, nil)
		if err != nil {
			t.Fatalf("%s: %v", tc.shell, err)
		}
		if program != tc.program {
			t.Fatalf("%s program=%q want %q", tc.shell, program, tc.program)
		}
		if strings.Join(args, " ") != strings.Join(tc.args, " ") {
			t.Fatalf("%s args=%q want %q", tc.shell, args, tc.args)
		}
	}
	// A shell that accepts an explicit .exe suffix is normalized.
	if program, _, err := shellCommand("pwsh.exe", "echo hi", nil, nil); err != nil || program != "pwsh" {
		t.Fatalf("pwsh.exe program=%q err=%v", program, err)
	}
	// cmd honors ComSpec when the environment provides one.
	if program, _, err := shellCommand("cmd", "echo hi", nil, map[string]string{"ComSpec": `C:\Windows\System32\cmd.exe`}); err != nil || program != `C:\Windows\System32\cmd.exe` {
		t.Fatalf("cmd ComSpec program=%q err=%v", program, err)
	}
	// An unknown shell is rejected instead of silently using a platform default.
	if _, _, err := shellCommand("fish", "echo hi", nil, nil); err == nil {
		t.Fatal("expected an unsupported shell error")
	}
}

// requireShell skips the test when none of the named interpreters is installed.
func requireShell(t *testing.T, names ...string) string {
	t.Helper()
	for _, name := range names {
		probe := name
		if name == "cmd" {
			probe = "cmd.exe"
		}
		if _, err := exec.LookPath(probe); err == nil {
			return name
		}
	}
	t.Skipf("none of %v is installed", names)
	return ""
}

// shellEcho returns a script that prints marker with the given shell.
func shellEcho(shell, marker string) string {
	switch shell {
	case "cmd":
		return "echo " + marker
	case "pwsh", "powershell":
		return "Write-Output '" + marker + "'"
	default:
		return "printf %s '" + marker + "'"
	}
}

// TestShellSelectionIsExplicit runs every installed shell through the engine and
// checks the requested interpreter handled the script.
func TestShellSelectionIsExplicit(t *testing.T) {
	shells := []string{"sh", "bash", "pwsh", "powershell", "cmd"}
	var ran []string
	for _, shell := range shells {
		shell := requireShellOrSkip(t, shell)
		if shell == "" {
			continue
		}
		marker := "marker-" + shell
		runner := newTestRunner(t, Definition{
			Tasks: map[string]Task{"t": {Steps: []Step{{Shell: &ShellSpec{Name: shell, Script: shellEcho(shell, marker)}}}}},
		})
		result := mustRun(t, runner, Request{Task: "t", IO: IO{CaptureLimit: 1 << 12}})
		if !strings.Contains(string(result.Steps[0].Output), marker) {
			t.Fatalf("%s output=%q", shell, result.Steps[0].Output)
		}
		ran = append(ran, shell)
	}
	if len(ran) == 0 {
		t.Skip("no shell interpreter is installed")
	}
	t.Logf("verified shells: %v", ran)
}

// requireShellOrSkip returns the shell name when installed, otherwise "".
func requireShellOrSkip(t *testing.T, name string) string {
	t.Helper()
	probe := name
	if name == "cmd" {
		probe = "cmd.exe"
	}
	if _, err := exec.LookPath(probe); err != nil {
		t.Logf("shell %s is not installed on this host, skipping it", name)
		return ""
	}
	return name
}

// TestShellScriptIsRenderedOnEveryShell checks that templates are rendered
// before the source reaches the interpreter, whichever shell is selected.
func TestShellScriptIsRenderedOnEveryShell(t *testing.T) {
	shell := requireShell(t, "sh", "bash", "pwsh", "cmd")
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {
			Vars:  map[string]any{"target": "rendered"},
			Steps: []Step{{Shell: &ShellSpec{Name: shell, Script: shellEcho(shell, "${vars.target}")}}},
		}},
	})
	result := mustRun(t, runner, Request{Task: "t", IO: IO{CaptureLimit: 1 << 12}})
	if !strings.Contains(string(result.Steps[0].Output), "rendered") {
		t.Fatalf("%s output=%q", shell, result.Steps[0].Output)
	}
}

// TestStepPlatformSkip verifies step level platform gating, which is separate
// from the task level gate.
func TestStepPlatformSkip(t *testing.T) {
	other := "linux"
	if runtime.GOOS == "linux" {
		other = "windows"
	}
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{
			{Name: "skipped", Platform: []string{other}, Exec: &ExecSpec{Program: "definitely-not-a-program"}},
			{Name: "runs", Exec: &ExecSpec{Program: "go", Args: []string{"version"}}},
		}}},
	})
	result := mustRun(t, runner, Request{Task: "t", IO: IO{CaptureLimit: 1 << 12}})
	if result.Status != StatusSucceeded {
		t.Fatalf("status=%s", result.Status)
	}
	if len(result.Steps) != 2 {
		t.Fatalf("steps=%+v", result.Steps)
	}
	if result.Steps[0].Status != StatusSkipped || result.Steps[1].Status != StatusSucceeded {
		t.Fatalf("steps=%+v", result.Steps)
	}
	if result.Steps[1].Started != true {
		t.Fatalf("second step did not run: %+v", result.Steps[1])
	}
}

// TestSkippedPlatformStepIsReportedByInspect checks the plan reports a skipped
// step without executing anything.
func TestSkippedPlatformStepIsReportedByInspect(t *testing.T) {
	other := "linux"
	if runtime.GOOS == "linux" {
		other = "windows"
	}
	runner := newTestRunner(t, Definition{
		Tasks: map[string]Task{"t": {Steps: []Step{
			{Name: "skipped", Platform: []string{other}, Exec: &ExecSpec{Program: "go"}},
			{Name: "runs", Exec: &ExecSpec{Program: "go"}},
		}}},
	})
	plan, err := runner.Inspect(context.Background(), Request{Task: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Actions) != 1 {
		t.Fatalf("plan=%+v", plan.Actions)
	}
	if len(plan.Skipped) == 0 {
		t.Fatalf("skipped step was not reported: %+v", plan)
	}
}
