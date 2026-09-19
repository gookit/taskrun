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

	"github.com/gookit/kscript"
)

func main() {
	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	runner, err := kscript.New(kscript.Definition{
		Version: 1,
		BaseDir: dir,
		Vars:    map[string]any{"announce": true},
		Tasks: map[string]kscript.Task{
			"report": {
				Desc: "call back into the application",
				DynamicVars: map[string]kscript.DynamicVar{
					"revision": {Exec: &kscript.ExecSpec{Program: "go", Args: []string{"env", "GOOS"}}},
				},
				If: "vars.announce == true",
				Steps: []kscript.Step{
					// The handler receives rendered arguments, isolated vars, env
					// and the effective directory. It must respect ctx.
					{Name: "notify", Host: &kscript.HostSpec{
						Name: "app.notify",
						Args: []any{"platform=${vars.revision}"},
					}},
				},
			},
		},
	}, kscript.WithHandler("app.notify", func(ctx context.Context, call kscript.HostCall) (kscript.ActionResult, error) {
		select {
		case <-ctx.Done():
			return kscript.ActionResult{}, ctx.Err()
		default:
		}
		message := fmt.Sprintf("%v", call.Args[0])
		fmt.Printf("handler %s: %s (dir=%s)\n", call.Name, message, call.Dir)
		return kscript.ActionResult{Started: true, Output: []byte(message + "\n")}, nil
	}), kscript.WithBaseEnv(map[string]string{"PATH": os.Getenv("PATH")}))
	if err != nil {
		log.Fatalf("new runner: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := runner.Run(ctx, kscript.Request{
		Task: "report",
		IO:   kscript.IO{Stdout: os.Stdout, Stderr: os.Stderr},
	})
	if err != nil {
		log.Fatalf("run: %v", err)
	}
	log.Printf("status=%s", result.Status)
	for _, step := range result.Steps {
		log.Printf("step=%s status=%s output=%q", step.Name, step.Status, step.Output)
	}
}
