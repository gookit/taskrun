package formats

import "testing"

func TestLegacyMap(t *testing.T) {
	def, err := LegacyMap(map[string]any{"build": []string{"go test ./...", "go vet ./..."}}, ".")
	if err != nil || len(def.Tasks["build"].Steps) != 2 {
		t.Fatalf("def=%+v err=%v", def, err)
	}
}
