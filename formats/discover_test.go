package formats

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func touch(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDiscoverRequiresExplicitOptions(t *testing.T) {
	if _, err := Discover(DiscoverOptions{StartDir: t.TempDir()}); err == nil {
		t.Fatal("expected Names to be required")
	}
	if _, err := Discover(DiscoverOptions{Names: []string{"t"}}); err == nil {
		t.Fatal("expected StartDir to be required")
	}
	if _, err := Discover(DiscoverOptions{Names: []string{"t"}, StartDir: "relative"}); err == nil {
		t.Fatal("expected an absolute StartDir requirement")
	}
	if _, err := Discover(DiscoverOptions{Names: []string{"t"}, StartDir: t.TempDir(), Mode: "everywhere"}); err == nil {
		t.Fatal("expected an unknown mode error")
	}
	if _, err := Discover(DiscoverOptions{Names: []string{"t"}, StartDir: filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("expected a missing directory error")
	}
}

func TestDiscoverNearestStopsAtFirstLevel(t *testing.T) {
	root := t.TempDir()
	near := filepath.Join(root, "a", "b")
	touch(t, filepath.Join(root, "tasks.yaml"), "version: 1\ntasks:\n  root:\n    steps:\n      - exec: {program: go}\n")
	nearFile := touch(t, filepath.Join(near, "tasks.yaml"), "version: 1\ntasks:\n  near:\n    steps:\n      - exec: {program: go}\n")

	files, err := Discover(DiscoverOptions{Mode: Nearest, Names: []string{"tasks"}, StartDir: near})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != nearFile {
		t.Fatalf("files=%v", files)
	}
}

func TestDiscoverAncestorsReturnsRootToNear(t *testing.T) {
	root := t.TempDir()
	near := filepath.Join(root, "a", "b")
	rootFile := touch(t, filepath.Join(root, "tasks.yaml"), "version: 1\ntasks:\n  root:\n    steps:\n      - exec: {program: go}\n")
	midFile := touch(t, filepath.Join(root, "a", "tasks.yaml"), "version: 1\ntasks:\n  mid:\n    steps:\n      - exec: {program: go}\n")
	nearFile := touch(t, filepath.Join(near, "tasks.yaml"), "version: 1\ntasks:\n  near:\n    steps:\n      - exec: {program: go}\n")

	files, err := Discover(DiscoverOptions{Mode: Ancestors, Names: []string{"tasks"}, StartDir: near, StopDir: root})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{rootFile, midFile, nearFile}
	if len(files) != len(want) {
		t.Fatalf("files=%v want %v", files, want)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("files=%v want %v", files, want)
		}
	}
	// One file per level, even when several candidates exist.
	touch(t, filepath.Join(root, "a", "other.yaml"), "version: 1\ntasks:\n  other:\n    steps:\n      - exec: {program: go}\n")
	files, err = Discover(DiscoverOptions{Mode: Ancestors, Names: []string{"tasks", "other"}, StartDir: filepath.Join(root, "a"), StopDir: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files=%v", files)
	}
}

func TestDiscoverMaxDepthAndExtensionOrder(t *testing.T) {
	root := t.TempDir()
	near := filepath.Join(root, "a", "b")
	touch(t, filepath.Join(root, "tasks.json"), "{}")
	jsonNear := touch(t, filepath.Join(near, "tasks.json"), "{}")
	touch(t, filepath.Join(near, "tasks.yaml"), "version: 1\n")

	files, err := Discover(DiscoverOptions{
		Mode: Ancestors, Names: []string{"tasks"},
		Extensions: []string{".json", ".yaml"},
		StartDir:   near, MaxDepth: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0] != jsonNear {
		t.Fatalf("files=%v", files)
	}
	files, err = Discover(DiscoverOptions{
		Mode: Nearest, Names: []string{"tasks"},
		Extensions: []string{"yaml"}, StartDir: near,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !strings.HasSuffix(files[0], "tasks.yaml") {
		t.Fatalf("files=%v", files)
	}
}

func TestDiscoverStopsAtFilesystemRoot(t *testing.T) {
	root := t.TempDir()
	deep := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	// Nothing exists, so the walk must terminate at the volume root.
	files, err := Discover(DiscoverOptions{Mode: Ancestors, Names: []string{"definitely-absent"}, StartDir: deep})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("files=%v", files)
	}
}

func TestDiscoverAndLoadMergesSources(t *testing.T) {
	root := t.TempDir()
	near := filepath.Join(root, "a")
	touch(t, filepath.Join(root, "tasks.yaml"), "version: 1\nvars:\n  level: root\ntasks:\n  shared:\n    steps:\n      - exec: {program: go}\n")
	touch(t, filepath.Join(near, "tasks.yaml"), "version: 1\nvars:\n  level: near\ntasks:\n  local:\n    steps:\n      - exec: {program: go}\n")

	def, err := DiscoverAndLoad(DiscoverOptions{Mode: Ancestors, Names: []string{"tasks"}, StartDir: near, StopDir: root}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := def.Tasks["shared"]; !ok {
		t.Fatalf("root task missing: %+v", def.Tasks)
	}
	if _, ok := def.Tasks["local"]; !ok {
		t.Fatalf("near task missing: %+v", def.Tasks)
	}
	if def.Vars["level"] != "near" {
		t.Fatalf("vars=%v", def.Vars)
	}
	if len(def.Sources) != 2 {
		t.Fatalf("sources=%+v", def.Sources)
	}
}

func TestDiscoverAndLoadReportsMissingFile(t *testing.T) {
	if _, err := DiscoverAndLoad(DiscoverOptions{Names: []string{"absent"}, StartDir: t.TempDir()}, false); err == nil {
		t.Fatal("expected a missing file error")
	}
}
