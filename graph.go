package kscript

import (
	"fmt"
	"sort"
	"strings"
)

// graphError reports a dependency or task-call cycle with its full path.
type graphError struct {
	path []string
}

func (e *graphError) Error() string {
	return "dependency cycle: " + strings.Join(e.path, " -> ")
}

// Is reports the error as both a cycle and a definition error.
func (e *graphError) Is(target error) bool {
	return target == ErrDependencyCycle || target == ErrInvalidDefinition
}

// taskEdges returns the static edges of a task: dependencies in declaration
// order, followed by task calls in step order. Both kinds participate in cycle
// detection even when a condition would skip them.
func taskEdges(task Task) []string {
	edges := append([]string(nil), task.Deps...)
	for _, step := range task.Steps {
		if step.Task != nil {
			edges = append(edges, step.Task.Name)
		}
	}
	return edges
}

// checkGraph rejects cycles and over-deep call chains before any action runs.
// The whole definition is analyzed, not only the tasks a caller may run.
func checkGraph(def Definition, maxDepth int) error {
	names := make([]string, 0, len(def.Tasks))
	for name := range def.Tasks {
		names = append(names, name)
	}
	sort.Strings(names)

	const (
		visiting = 1
		done     = 2
	)
	state := make(map[string]int, len(names))
	var stack []string
	var visit func(name string) error
	visit = func(name string) error {
		switch state[name] {
		case visiting:
			index := 0
			for i, item := range stack {
				if item == name {
					index = i
					break
				}
			}
			path := append(append([]string(nil), stack[index:]...), name)
			return &RunError{Kind: ErrKindInvalidDefinition, Err: &graphError{path: path}}
		case done:
			return nil
		}
		state[name] = visiting
		stack = append(stack, name)
		for _, next := range taskEdges(def.Tasks[name]) {
			if _, ok := def.Tasks[next]; !ok {
				continue
			}
			if err := visit(next); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = done
		return nil
	}
	for _, name := range names {
		if err := visit(name); err != nil {
			return err
		}
	}
	if maxDepth <= 0 {
		return nil
	}
	// Cycles are gone, so the longest chain can be measured with memoization.
	longest := make(map[string]int, len(names))
	var measure func(name string, path []string) (int, []string, error)
	measure = func(name string, path []string) (int, []string, error) {
		if length, ok := longest[name]; ok {
			return length, append(path, name), nil
		}
		path = append(path, name)
		if len(path) > maxDepth {
			return 0, path, invalidDef("task call chain exceeds the maximum depth %d: %s", maxDepth, strings.Join(path, " -> "))
		}
		best, bestPath := 1, path
		for _, next := range taskEdges(def.Tasks[name]) {
			if _, ok := def.Tasks[next]; !ok {
				continue
			}
			length, nextPath, err := measure(next, path)
			if err != nil {
				return 0, nil, err
			}
			if length+1 > best {
				best, bestPath = length+1, nextPath
			}
		}
		longest[name] = best
		return best, bestPath, nil
	}
	for _, name := range names {
		if _, _, err := measure(name, nil); err != nil {
			return err
		}
	}
	return nil
}

var _ = fmt.Sprintf
