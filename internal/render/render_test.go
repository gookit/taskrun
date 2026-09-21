package render

import (
	"sync"
	"testing"
)

// TestReferencesAnyIsConcurrencySafe hammers the memoized pattern cache from
// many goroutines with distinct names. The cache used to be a plain map, which
// concurrent runs could write at the same time (a hard "concurrent map writes"
// panic and a race-detector failure).
func TestReferencesAnyIsConcurrencySafe(t *testing.T) {
	const workers = 16
	const rounds = 200

	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			name := string(rune('a'+worker%26)) + "-dynamic-" + string(rune('0'+worker%10))
			for i := 0; i < rounds; i++ {
				if !ReferencesAny("vars."+name+" == true", []string{name}) {
					t.Errorf("worker %d: reference %q was not detected", worker, name)
					return
				}
				if ReferencesAny("vars.other == true", []string{name}) {
					t.Errorf("worker %d: unrelated source matched %q", worker, name)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
}

// TestReferencesAnyMatchesWholeWords documents the boundary rule the deferred
// detection depends on.
func TestReferencesAnyMatchesWholeWords(t *testing.T) {
	cases := []struct {
		source string
		names  []string
		want   bool
	}{
		{source: "vars.stamp != ''", names: []string{"stamp"}, want: true},
		{source: "stamp == 'x'", names: []string{"stamp"}, want: true},
		{source: "timestamp == 'x'", names: []string{"stamp"}, want: false},
		{source: "vars.other == 1", names: []string{"stamp"}, want: false},
		{source: "vars.a == 1", names: nil, want: false},
		{source: "vars.a == 1", names: []string{""}, want: false},
	}
	for _, tc := range cases {
		if got := ReferencesAny(tc.source, tc.names); got != tc.want {
			t.Errorf("ReferencesAny(%q, %v) = %v, want %v", tc.source, tc.names, got, tc.want)
		}
	}
}
