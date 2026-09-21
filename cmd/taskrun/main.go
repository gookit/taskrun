// Command taskrun runs one task from a definition file. It is a small consumer
// example for the library, not part of the library API.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/gookit/taskrun"
	"github.com/gookit/taskrun/formats"
)

func main() {
	config := flag.String("config", "", "task definition file (.json, .yaml, .yml, .toml)")
	taskName := flag.String("task", "", "task name")
	dryRun := flag.Bool("dry-run", false, "validate and print the plan without running commands")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: taskrun -config <file> -task <name> [-dry-run] [args...]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *config == "" || *taskName == "" {
		flag.Usage()
		os.Exit(2)
	}

	def, err := formats.LoadFile(*config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	runner, err := taskrun.New(def)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	request := taskrun.Request{
		Task:   *taskName,
		Args:   flag.Args(),
		DryRun: *dryRun,
		IO:     taskrun.IO{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr},
	}
	if *dryRun {
		plan, err := runner.Inspect(context.Background(), request)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		for _, action := range plan.Actions {
			fmt.Printf("%s %s %s %v\n", action.Task, action.Kind, action.Program, action.Args)
		}
		for _, deferred := range plan.Deferred {
			fmt.Printf("deferred: %s\n", deferred)
		}
		for _, skipped := range plan.Skipped {
			fmt.Printf("skipped: %s\n", skipped)
		}
		return
	}
	result, err := runner.Run(context.Background(), request)
	if err != nil {
		fmt.Fprintf(os.Stderr, "status=%s err=%v\n", result.Status, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "status=%s\n", result.Status)
}
