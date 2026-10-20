package serve

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
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

// settle drops the reports that come until none has come for d: one step of
// a test (a file created, then written) can be two reports when the runner is
// slow enough that its events are more than the debounce apart, and a report
// left over would otherwise be read as the next step's.
func (c *collector) settle(d time.Duration) {
	for {
		select {
		case <-c.ch:
		case <-time.After(d):
			c.mu.Lock()
			c.got = nil
			c.mu.Unlock()
			return
		}
	}
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

// testDebounce is long enough that the writes of one burst, a few file
// system calls apart, fall within it even under -race on a busy runner; at
// 30 ms a burst was sometimes reported twice in CI.
const testDebounce = 150 * time.Millisecond

func startWatch(t *testing.T, c *collector, targets ...string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	// Watch starts its watches before it reads events, but returns only at
	// the end; give it the time to arm.
	go func() { done <- Watch(ctx, targets, testDebounce, c.onChange) }()
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
	c.settle(3 * testDebounce)
	// A changed file is reported. That a burst is ONE report is a matter of
	// time, which a runner under -race does not keep: main's CI split a burst
	// at 30 ms and again at 150 ms. TestCoalesce asserts it on a fake clock.
	for i := 0; i < 5; i++ {
		os.WriteFile(a, []byte("# a\n"+string(rune('0'+i))), 0o644)
	}
	if got := c.next(t); !contains(got, a) {
		t.Errorf("a changed file: %v", got)
	}
	c.settle(3 * testDebounce)

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

	c.settle(3 * testDebounce)

	// what an editor leaves, a dotfile and node_modules are not reported
	os.WriteFile(filepath.Join(dir, "a.md~"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, ".a.md.swp"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "node_modules", "m", "x.md"), []byte("x"), 0o644)
	c.quiet(t, 3*testDebounce)

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

// TestCoalesce asserts the debounce on synctest's fake clock, where time only
// moves when every goroutine waits: a burst is one report whatever the
// machine's load, a pause longer than the debounce makes two, and nothing is
// reported after in is closed.
func TestCoalesce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const debounce = 100 * time.Millisecond
		var mu sync.Mutex
		var all [][]string
		reported := func() [][]string {
			mu.Lock()
			defer mu.Unlock()
			return slices.Clone(all)
		}
		in := make(chan string)
		closeIn := sync.OnceFunc(func() { close(in) })
		done := make(chan struct{})
		go func() {
			defer close(done)
			coalesce(in, debounce, func(p []string) {
				mu.Lock()
				all = append(all, p)
				mu.Unlock()
			})
		}()
		// a failed assertion still ends coalesce, so the bubble can exit
		defer func() {
			closeIn()
			<-done
		}()

		// a burst: five events 40 ms apart, two paths, one of them twice. It
		// lasts 160 ms, longer than the debounce, so a window that the first
		// event opens and the next ones do not push back would split it.
		for i, p := range []string{"/d/a.md", "/d/b.md", "/d/a.md", "/d/b.md", "/d/a.md"} {
			if i > 0 {
				time.Sleep(40 * time.Millisecond)
			}
			in <- p
		}
		time.Sleep(debounce - time.Millisecond)
		synctest.Wait()
		if r := reported(); len(r) != 0 {
			t.Fatalf("reported before the debounce had passed: %v", r)
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		if r := reported(); len(r) != 1 || !slices.Equal(r[0], []string{"/d/a.md", "/d/b.md"}) {
			t.Fatalf("a burst: got %v, want one report [/d/a.md /d/b.md]", r)
		}

		// two events further apart than the debounce are two reports
		in <- "/d/a.md"
		time.Sleep(debounce + time.Millisecond)
		in <- "/d/b.md"
		time.Sleep(debounce + time.Millisecond)
		synctest.Wait()
		if r := reported(); len(r) != 3 || !slices.Equal(r[1], []string{"/d/a.md"}) || !slices.Equal(r[2], []string{"/d/b.md"}) {
			t.Fatalf("two bursts: got %v", r)
		}

		// what is pending when in closes is dropped, and coalesce returns
		in <- "/d/c.md"
		closeIn()
		<-done
		if r := reported(); len(r) != 3 {
			t.Fatalf("reported after in closed: %v", r[3:])
		}
	})
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
