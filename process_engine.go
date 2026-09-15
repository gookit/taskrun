package kscript

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
)

// ProcessEngine executes argv actions as child processes.
type ProcessEngine struct{}

func (ProcessEngine) Execute(ctx context.Context, action PreparedAction, streams IO) (ActionResult, error) {
	args := action.Args
	program := action.Program
	if action.Kind == "shell" {
		if runtime.GOOS == "windows" {
			args = []string{"/D", "/S", "/C", action.Script}
			program = "cmd.exe"
		} else {
			args = []string{"-c", action.Script}
		}
	}
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = action.Dir
	if len(action.Env) > 0 {
		cmd.Env = append([]string{}, os.Environ()...)
		for key, value := range action.Env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = streams.Stdin, streams.Stdout, streams.Stderr
	var capture bytes.Buffer
	if streams.CaptureLimit > 0 && streams.Stdout == nil {
		cmd.Stdout = &capture
	}
	err := cmd.Run()
	result := ActionResult{}
	if streams.CaptureLimit > 0 && streams.Stdout == nil {
		result.Output = capture.Bytes()
		if int64(len(result.Output)) > streams.CaptureLimit {
			result.Output = result.Output[:streams.CaptureLimit]
			result.Truncated = true
		}
	}
	if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		result.ExitCode = &code
	}
	return result, err
}
