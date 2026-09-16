package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gookit/kscript"
	"github.com/gookit/kscript/formats"
)

func main() {
	config := flag.String("config", "", "task definition file (.json, .yaml, .yml, .toml)")
	taskName := flag.String("task", "", "task name")
	dryRun := flag.Bool("dry-run", false, "inspect without executing commands")
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
	runner, err := kscript.New(def)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx := context.Background()
	result, err := runner.Run(ctx, kscript.Request{Task: *taskName, Args: flag.Args(), Dir: filepath.Dir(*config), DryRun: *dryRun, IO: kscript.IO{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr, CaptureLimit: 0}})
	if result != nil && result.Plan != nil {
		for _, action := range result.Plan.Actions {
			fmt.Printf("%s %s %s %v\n", action.Task, action.Kind, action.Program, action.Args)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
