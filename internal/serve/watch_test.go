package serve

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// collector gathers what Watch reports.
type collector struct {
	mu  sync.Mutex
	got []string
	ch  chan struct{}
}

func newCollector() *collector { return &collector{ch: make(chan struct{}, 64)} }

func (c *collector) onChange(paths []string) {
	c.mu.Lock()
	c.got = append(c.got, paths...)
	c.mu.Unlock()
	c.ch <- struct{}{}
}

// next waits for a report and returns what has been gathered so far.
func (c *collector) next(t *testing.T) []string {
	t.Helper()
	select {
	case <-c.ch:
	case <-time.After(10 * time.Second):
		t.Fatal("no change reported in 10 s")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.got
	c.got = nil
	return out
}

// quiet fails if a report comes within d.
func (c *collector) quiet(t *testing.T, d time.Duration) {
	t.Helper()
	select {
	case <-c.ch:
		c.mu.Lock()
		defer c.mu.Unlock()
		t.Errorf("unexpected report %v", c.got)
	case <-time.After(d):
	}
}

func startWatch(t *testing.T, c *collector, targets ...string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	// Watch starts its watches before it reads events, but returns only at
	// the end; give it the time to arm.
	go func() { done <- Watch(ctx, targets, 30*time.Millisecond, c.onChange) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Watch: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("Watch did not stop")
		}
	})
	time.Sleep(300 * time.Millisecond)
}

func contains(paths []string, p string) bool {
	for _, q := range paths {
		if q == p {
			return true
		}
	}
	return false
}

func TestWatchDirectory(t *testing.T) {
	dir, _ := Root(t.TempDir())
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.MkdirAll(filepath.Join(dir, "node_modules", "m"), 0o755)
	c := newCollector()
	startWatch(t, c, dir)

	a := filepath.Join(dir, "a.md")
	if err := os.WriteFile(a, []byte("# a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := c.next(t); !contains(got, a) {
		t.Errorf("a new file: %v", got)
	}
	// a burst is one report
	for i := 0; i < 5; i++ {
		os.WriteFile(a, []byte("# a\n"+string(rune('0'+i))), 0o644)
	}
	if got := c.next(t); !contains(got, a) {
		t.Errorf("a changed file: %v", got)
	}
	c.quiet(t, 300*time.Millisecond)

	// a file in a subdirectory, and in a directory made later
	b := filepath.Join(dir, "sub", "b.md")
	os.WriteFile(b, []byte("# b"), 0o644)
	if got := c.next(t); !contains(got, b) {
		t.Errorf("a file in a subdirectory: %v", got)
	}
	os.MkdirAll(filepath.Join(dir, "later", "deep"), 0o755)
	c.next(t) // the directory
	d := filepath.Join(dir, "later", "deep", "d.md")
	os.WriteFile(d, []byte("# d"), 0o644)
	var got []string
	for !contains(got, d) {
		got = append(got, c.next(t)...)
	}

	// what an editor leaves, a dotfile and node_modules are not reported
	os.WriteFile(filepath.Join(dir, "a.md~"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, ".a.md.swp"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "node_modules", "m", "x.md"), []byte("x"), 0o644)
	c.quiet(t, 400*time.Millisecond)

	// removal is a change
	os.Remove(a)
	if got := c.next(t); !contains(got, a) {
		t.Errorf("a removed file: %v", got)
	}
}

func TestWatchFile(t *testing.T) {
	dir, _ := Root(t.TempDir())
	deck := filepath.Join(dir, "deck.md")
	other := filepath.Join(dir, "other.md")
	os.WriteFile(deck, []byte("# d"), 0o644)
	os.WriteFile(other, []byte("# o"), 0o644)
	c := newCollector()
	startWatch(t, c, deck)

	os.WriteFile(other, []byte("# o2"), 0o644)
	c.quiet(t, 400*time.Millisecond)

	// an editor that writes a copy and renames it over the file
	tmp := filepath.Join(dir, "deck.md.tmp")
	os.WriteFile(tmp, []byte("# d2"), 0o644)
	if err := os.Rename(tmp, deck); err != nil {
		t.Fatal(err)
	}
	if got := c.next(t); !contains(got, deck) {
		t.Errorf("a file replaced by a rename: %v", got)
	}
	// and the watch outlives it
	os.WriteFile(deck, []byte("# d3"), 0o644)
	if got := c.next(t); !contains(got, deck) {
		t.Errorf("after the rename: %v", got)
	}
}

func TestWatchErrors(t *testing.T) {
	if err := Watch(context.Background(), []string{filepath.Join(t.TempDir(), "missing")}, time.Millisecond, func([]string) {}); err == nil {
		t.Error("watching nothing should fail")
	}
}

func TestWanted(t *testing.T) {
	dirs := map[string]bool{"/w": true}
	files := map[string]bool{"/f/deck.md": true}
	for p, want := range map[string]bool{
		"/w/a.md":                true,
		"/w/sub/a.md":            true,
		"/w/node_modules/x/a.md": false,
		"/w/.git/x":              false,
		"/f/deck.md":             true,
		"/f/other.md":            false,
		"/elsewhere/a.md":        false,
	} {
		if wanted(p, files, dirs) != want {
			t.Errorf("wanted(%q) = %v", p, !want)
		}
	}
}
