// Package graph validates the static task graph: it rejects dependency and task
// call cycles and over-deep call chains before any action runs. It works on
// plain node names and edge lists, so it has no knowledge of the task model.
package graph

import (
	"fmt"
	"sort"
	"strings"
)

// CycleError reports a dependency or task call cycle with its full path.
type CycleError struct {
	// Path lists the cycle starting and ending with the same node.
	Path []string
}

func (e *CycleError) Error() string {
	return "dependency cycle: " + strings.Join(e.Path, " -> ")
}

// DepthError reports a call chain longer than the configured maximum.
type DepthError struct {
	// Max is the configured maximum depth.
	Max int
	// Path is the longest chain that exceeded it.
	Path []string
}

func (e *DepthError) Error() string {
	return fmt.Sprintf("task call chain exceeds the maximum depth %d: %s", e.Max, strings.Join(e.Path, " -> "))
}

// Check validates every node in nodes using edges. An edge that names an
// unknown node is ignored: reference validation is the caller's concern.
// maxDepth <= 0 skips the depth check.
func Check(nodes []string, edges map[string][]string, maxDepth int) error {
	names := append([]string(nil), nodes...)
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
			return &CycleError{Path: append(append([]string(nil), stack[index:]...), name)}
		case done:
			return nil
		}
		state[name] = visiting
		stack = append(stack, name)
		for _, next := range edges[name] {
			if !known(next, names) {
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
			return 0, path, &DepthError{Max: maxDepth, Path: path}
		}
		best, bestPath := 1, path
		for _, next := range edges[name] {
			if !known(next, names) {
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

// known reports whether name is one of the declared nodes.
func known(name string, names []string) bool {
	index := sort.SearchStrings(names, name)
	return index < len(names) && names[index] == name
}
