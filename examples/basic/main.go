// Command basic shows the smallest embedding of the library: build a
// definition in Go, run one task and read the structured result.
package main

import (
	"context"
	"log"
	"os"

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
		Tasks: map[string]taskrun.Task{
			"check": {
				Desc: "show the Go toolchain version",
				Steps: []taskrun.Step{
					{Name: "go-version", Exec: &taskrun.ExecSpec{Program: "go", Args: []string{"version"}}},
				},
			},
		},
	})
	if err != nil {
		log.Fatalf("new runner: %v", err)
	}

	plan, err := runner.Inspect(context.Background(), taskrun.Request{Task: "check"})
	if err != nil {
		log.Fatalf("inspect: %v", err)
	}
	for _, action := range plan.Actions {
		log.Printf("planned: %s %s %v", action.Kind, action.Program, action.Args)
	}

	result, err := runner.Run(context.Background(), taskrun.Request{
		Task: "check",
		IO:   taskrun.IO{Stdout: os.Stdout, Stderr: os.Stderr},
	})
	if err != nil {
		log.Fatalf("run: %v", err)
	}
	log.Printf("status=%s steps=%d", result.Status, len(result.Steps))
}
