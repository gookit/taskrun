// Command host shows how an application exposes its own capabilities to a task
// through an explicitly registered handler, plus dynamic variables and
// conditions.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/gookit/taskrun"
)

func main() {
	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	runner, err := taskrun.New(taskrun.Definition{
		Version: 1,
		BaseDir: dir,
		Vars:    map[string]any{"announce": true},
		Tasks: map[string]taskrun.Task{
			"report": {
				Desc: "call back into the application",
				DynamicVars: map[string]taskrun.DynamicVar{
					"revision": {Exec: &taskrun.ExecSpec{Program: "go", Args: []string{"env", "GOOS"}}},
				},
				If: "vars.announce == true",
				Steps: []taskrun.Step{
					// The handler receives rendered arguments, isolated vars, env
					// and the effective directory. It must respect ctx.
					{Name: "notify", Host: &taskrun.HostSpec{
						Name: "app.notify",
						Args: []any{"platform=${vars.revision}"},
					}},
				},
			},
		},
	}, taskrun.WithHandler("app.notify", func(ctx context.Context, call taskrun.HostCall) (taskrun.ActionResult, error) {
		select {
		case <-ctx.Done():
			return taskrun.ActionResult{}, ctx.Err()
		default:
		}
		message := fmt.Sprintf("%v", call.Args[0])
		fmt.Printf("handler %s: %s (dir=%s)\n", call.Name, message, call.Dir)
		return taskrun.ActionResult{Started: true, Output: []byte(message + "\n")}, nil
	}), taskrun.WithBaseEnv(map[string]string{"PATH": os.Getenv("PATH")}))
	if err != nil {
		log.Fatalf("new runner: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := runner.Run(ctx, taskrun.Request{
		Task: "report",
		IO:   taskrun.IO{Stdout: os.Stdout, Stderr: os.Stderr},
	})
	if err != nil {
		log.Fatalf("run: %v", err)
	}
	log.Printf("status=%s", result.Status)
	for _, step := range result.Steps {
		log.Printf("step=%s status=%s output=%q", step.Name, step.Status, step.Output)
	}
}
