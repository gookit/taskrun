// Command config loads a YAML/JSON/TOML definition file, optionally discovers
// it from the current directory upwards, and runs a task.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/gookit/kscript"
	"github.com/gookit/kscript/formats"
)

func main() {
	config := flag.String("config", "", "definition file; empty means discover ./tasks.{yaml,json,toml}")
	taskName := flag.String("task", "hello", "task name")
	flag.Parse()

	def, err := load(*config)
	if err != nil {
		log.Fatalf("load: %v", err)
	}
	runner, err := kscript.New(def)
	if err != nil {
		log.Fatalf("new runner: %v", err)
	}
	result, err := runner.Run(context.Background(), kscript.Request{
		Task: *taskName,
		IO:   kscript.IO{Stdout: os.Stdout, Stderr: os.Stderr},
	})
	if err != nil {
		log.Fatalf("run: %v", err)
	}
	log.Printf("status=%s steps=%d", result.Status, len(result.Steps))
}

func load(config string) (kscript.Definition, error) {
	if config != "" {
		return formats.LoadFile(config)
	}
	dir, err := os.Getwd()
	if err != nil {
		return kscript.Definition{}, err
	}
	// Discovery is explicit: names, start directory and mode come from the
	// caller, and only the levels described here are inspected.
	files, err := formats.Discover(formats.DiscoverOptions{
		Mode:     formats.Ancestors,
		Names:    []string{"tasks", ".kite.tasks"},
		StartDir: dir,
		MaxDepth: 4,
	})
	if err != nil {
		return kscript.Definition{}, err
	}
	if len(files) == 0 {
		fallback := filepath.Join(dir, "examples", "basic.json")
		return formats.LoadFile(fallback)
	}
	return formats.LoadFiles(files, true)
}
