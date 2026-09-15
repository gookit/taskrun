package kscript

import (
	"context"
	"os"
	"os/exec"
)

// ProcessEngine executes argv actions as child processes.
type ProcessEngine struct{}

func (ProcessEngine) Execute(ctx context.Context, action PreparedAction, streams IO) (ActionResult, error) {
	cmd := exec.CommandContext(ctx, action.Program, action.Args...)
	cmd.Dir = action.Dir
	if len(action.Env) > 0 {
		cmd.Env = append([]string{}, os.Environ()...)
		for key, value := range action.Env {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = streams.Stdin, streams.Stdout, streams.Stderr
	err := cmd.Run()
	result := ActionResult{}
	if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		result.ExitCode = &code
	}
	return result, err
}
