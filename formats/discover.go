package formats

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gookit/kscript"
)

// DiscoverMode selects how far discovery walks.
type DiscoverMode string

const (
	// Nearest stops at the first directory level that has a match. It is the
	// default when Mode is empty.
	Nearest DiscoverMode = "nearest"
	// Ancestors collects at most one file per level, from the root down to the
	// start directory, so closer files override earlier ones when merged.
	Ancestors DiscoverMode = "ancestors"
)

// DefaultExtensions is the extension order used when Extensions is empty.
var DefaultExtensions = []string{".yaml", ".yml", ".json", ".toml"}

// DiscoverOptions configures explicit discovery. There is no implicit search:
// Names and StartDir are required and nothing outside the described levels is
// read.
type DiscoverOptions struct {
	Mode DiscoverMode
	// Names are file base names without extension, for example ".kite.task".
	Names []string
	// Extensions is the preferred extension order, for example [".yaml",".json"].
	Extensions []string
	// StartDir is the absolute directory the search starts at.
	StartDir string
	// StopDir stops the walk when it reaches this absolute directory.
	StopDir string
	// MaxDepth limits how many levels are inspected. Zero means no limit beyond
	// the stop directory or the filesystem root.
	MaxDepth int
}

// Discover returns the task files selected by the options. Files are returned in
// load order: a single nearest file, or root to nearest for ancestors.
func Discover(opts DiscoverOptions) ([]string, error) {
	if len(opts.Names) == 0 {
		return nil, fmt.Errorf("discover: Names is required; discovery is never implicit")
	}
	if opts.StartDir == "" {
		return nil, fmt.Errorf("discover: StartDir is required")
	}
	if !filepath.IsAbs(opts.StartDir) {
		return nil, fmt.Errorf("discover: StartDir %q must be an absolute path", opts.StartDir)
	}
	if opts.StopDir != "" && !filepath.IsAbs(opts.StopDir) {
		return nil, fmt.Errorf("discover: StopDir %q must be an absolute path", opts.StopDir)
	}
	if opts.MaxDepth < 0 {
		return nil, fmt.Errorf("discover: MaxDepth must not be negative")
	}
	info, err := os.Stat(opts.StartDir)
	if err != nil {
		return nil, fmt.Errorf("discover: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("discover: StartDir %q is not a directory", opts.StartDir)
	}
	mode := opts.Mode
	if mode == "" {
		mode = Nearest
	}
	if mode != Nearest && mode != Ancestors {
		return nil, fmt.Errorf("discover: unknown mode %q; use nearest or ancestors", opts.Mode)
	}
	extensions := opts.Extensions
	if len(extensions) == 0 {
		extensions = DefaultExtensions
	}
	for i, extension := range extensions {
		if extension != "" && !strings.HasPrefix(extension, ".") {
			extensions[i] = "." + extension
		}
	}
	for _, name := range opts.Names {
		if name == "" {
			return nil, fmt.Errorf("discover: Names contains an empty entry")
		}
	}

	var collected []string
	level := filepath.Clean(opts.StartDir)
	stop := ""
	if opts.StopDir != "" {
		stop = filepath.Clean(opts.StopDir)
	}
	for depth := 0; ; depth++ {
		if opts.MaxDepth > 0 && depth >= opts.MaxDepth {
			break
		}
		if match := firstMatch(level, opts.Names, extensions); match != "" {
			if mode == Nearest {
				return []string{match}, nil
			}
			collected = append(collected, match)
		}
		if stop != "" && samePath(level, stop) {
			break
		}
		parent := filepath.Dir(level)
		if parent == level {
			// Reached the filesystem root, including a Windows drive root.
			break
		}
		level = parent
	}
	for i, j := 0, len(collected)-1; i < j; i, j = i+1, j-1 {
		collected[i], collected[j] = collected[j], collected[i]
	}
	return collected, nil
}

// DiscoverAndLoad discovers files and merges them in load order.
func DiscoverAndLoad(opts DiscoverOptions, override bool) (kscript.Definition, error) {
	files, err := Discover(opts)
	if err != nil {
		return kscript.Definition{}, err
	}
	if len(files) == 0 {
		return kscript.Definition{}, fmt.Errorf("discover: no task file found from %s", opts.StartDir)
	}
	return LoadFiles(files, override)
}

func firstMatch(dir string, names, extensions []string) string {
	for _, name := range names {
		for _, extension := range extensions {
			candidate := filepath.Join(dir, name+extension)
			info, err := os.Stat(candidate)
			if err == nil && !info.IsDir() {
				return candidate
			}
		}
	}
	return ""
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
