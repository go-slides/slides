package interp

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/go-slides/slides/diag"
	"github.com/go-slides/slides/vfs"
)

// expandBounded runs Expand under a memory limit and a time bound, in a
// goroutine so that a regression fails the test, not the run: before these
// limits a deck that included itself ended in "fatal error: stack overflow", and a
// bomb below would have asked for gigabytes.
func expandBounded(t *testing.T, src string, files map[string]string) (string, error, time.Duration) {
	t.Helper()
	m := map[string][]byte{}
	for k, v := range files {
		m[k] = []byte(v)
	}
	opts := Options{BaseDir: "/w", Name: "deck.md", FS: vfs.NewMap(m)}
	prev := debug.SetMemoryLimit(768 << 20)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
	type result struct {
		out string
		err error
	}
	ch := make(chan result, 1)
	start := time.Now()
	go func() {
		out, err := Expand(src, opts)
		ch <- result{out, err}
	}()
	select {
	case r := <-ch:
		return r.out, r.err, time.Since(start)
	case <-time.After(90 * time.Second):
		t.Fatal("the expansion did not finish in 90 s")
	}
	return "", nil, 0
}

// refused asserts the expansion is refused with the code, quickly, and that it
// did not keep a heap that a bomb would have grown.
func refused(t *testing.T, src string, files map[string]string, code string) error {
	t.Helper()
	runtime.GC()
	out, err, took := expandBounded(t, src, files)
	if err == nil {
		t.Fatalf("not refused: it wrote %d bytes", len(out))
	}
	if got := diag.CodeOf(err); got != code {
		t.Fatalf("code %q, want %q: %v", got, code, err)
	}
	// the bound says "does not hang", not "is fast": a small CI runner under the
	// race detector is several times slower than a laptop
	if took > 45*time.Second {
		t.Errorf("refused after %v: too slow", took)
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	if ms.HeapAlloc > 700<<20 {
		t.Errorf("heap %d MiB after the refusal", ms.HeapAlloc>>20)
	}
	return err
}

func TestIncludeCycleIsRefused(t *testing.T) {
	const self = "# x\n\n{{ include \"deck.md\" }}\n"
	err := refused(t, self, map[string]string{"/w/deck.md": self}, "include.cycle")
	if !strings.Contains(err.Error(), "/w/deck.md → /w/deck.md") {
		t.Errorf("the message does not show the cycle: %v", err)
	}

	// a → b → a, the root being a
	files := map[string]string{
		"/w/b.md": `{{ include "deck.md" }}`,
	}
	err = refused(t, `{{ include "b.md" }}`, files, "include.cycle")
	if !strings.Contains(err.Error(), "/w/deck.md → /w/b.md → /w/deck.md") {
		t.Errorf("a → b → a: %v", err)
	}

	// b ↔ c below the root: the cycle is found among the included files
	files = map[string]string{
		"/w/b.md": `{{ include "c.md" }}`,
		"/w/c.md": `{{ include "b.md" }}`,
	}
	err = refused(t, `{{ include "b.md" }}`, files, "include.cycle")
	if !strings.Contains(err.Error(), "/w/b.md → /w/c.md → /w/b.md") {
		t.Errorf("b ↔ c: %v", err)
	}

	// three files, through a relative path that is spelled differently
	files = map[string]string{
		"/w/a/x.md": `{{ include "../y.md" }}`,
		"/w/y.md":   `{{ include "a/./x.md" }}`,
	}
	refused(t, `{{ include "a/x.md" }}`, files, "include.cycle")
}

// A template with no name still cannot loop: the root is not on the stack, so
// the cycle is found when the first include comes back to the file it read.
func TestIncludeCycleWithoutRootName(t *testing.T) {
	files := map[string][]byte{"/w/deck.md": []byte(`{{ include "deck.md" }}`)}
	_, err := Expand(`{{ include "deck.md" }}`, Options{BaseDir: "/w", FS: vfs.NewMap(files)})
	if diag.CodeOf(err) != "include.cycle" {
		t.Fatalf("%v", err)
	}
}

func TestIncludeDepthBomb(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < 60; i++ {
		files[fmt.Sprintf("/w/f%d.md", i)] = fmt.Sprintf(`{{ include "f%d.md" }}`, i+1)
	}
	files["/w/f60.md"] = "end"
	err := refused(t, `{{ include "f0.md" }}`, files, "include.limit")
	if !strings.Contains(err.Error(), fmt.Sprint(MaxIncludeDepth)) {
		t.Errorf("the message does not give the bound: %v", err)
	}
}

func TestIncludeCountAndSizeBombs(t *testing.T) {
	files := map[string]string{"/w/x.md": "x", "/w/big.md": strings.Repeat("y", 1<<20)}
	refused(t, `{{ range seq 20000 }}{{ include "x.md" }}{{ end }}`, files, "include.limit")
	refused(t, `{{ range seq 20 }}{{ include "big.md" }}{{ end }}`, files, "include.limit")

	// a binary tree of includes: each file includes the next twice, 2^14 reads
	tree := map[string]string{}
	for i := 0; i < 14; i++ {
		tree[fmt.Sprintf("/w/t%d.md", i)] = fmt.Sprintf(`{{ include "t%d.md" }}{{ include "t%d.md" }}`, i+1, i+1)
	}
	tree["/w/t14.md"] = "."
	refused(t, `{{ include "t0.md" }}`, tree, "include.limit")
}

// Files that include the same fragment more than once, or through two routes,
// are not cycles.
func TestIncludeSharedFragmentsAreNotCycles(t *testing.T) {
	files := map[string]string{
		"/w/b.md": `B{{ include "d.md" }}`,
		"/w/c.md": `C{{ include "d.md" }}`,
		"/w/d.md": "D",
	}
	out, err, _ := expandBounded(t, `{{ include "b.md" }}{{ include "c.md" }}{{ include "d.md" }}{{ include "d.md" }}`, files)
	if err != nil || out != "BDCDDD" {
		t.Fatalf("%q, %v", out, err)
	}
	// an include at the depth bound is allowed
	files = map[string]string{}
	for i := 0; i < MaxIncludeDepth-1; i++ {
		files[fmt.Sprintf("/w/f%d.md", i)] = fmt.Sprintf(`{{ include "f%d.md" }}`, i+1)
	}
	files[fmt.Sprintf("/w/f%d.md", MaxIncludeDepth-1)] = "end" // MaxIncludeDepth includes open at once
	if out, err, _ := expandBounded(t, `{{ include "f0.md" }}`, files); err != nil || out != "end" {
		t.Fatalf("the deepest allowed chain: %q, %v", out, err)
	}
}

func TestTemplateBombsAreRefused(t *testing.T) {
	// the budget of seq is lowered so that refusing a nested seq costs little: a
	// refusal at the full budget takes seconds under the race detector on a
	// small runner (TestNestedRangeKeepsItsFullBudget runs one at full size)
	prev := maxIterations
	maxIterations = 1 << 16
	defer func() { maxIterations = prev }()
	for name, src := range map[string]string{
		"seq alone":            `{{ range seq 1000000000 }}x{{ end }}`,
		"nested seq":           `{{ range seq 5000 }}{{ range seq 5000 }}{{ end }}{{ end }}`,
		"deeper nested seq":    `{{ range seq 300 }}{{ range seq 300 }}{{ range seq 300 }}{{ end }}{{ end }}{{ end }}`,
		"output by repeat":     `{{ range seq 1000000 }}{{ repeat 1000 "x" }}{{ end }}`,
		"one huge repeat":      `{{ repeat 2000000000 "xx" }}`,
		"doubling replace":     `{{ $s := "xxxxxxxx" }}{{ range seq 40 }}{{ $s = replace "x" "xx" $s }}{{ end }}`,
		"printf width":         `{{ printf "%2000000000d" 1 }}`,
		"printf precision":     `{{ printf "%.2000000000f" 1.5 }}`,
		"printf star":          `{{ printf "%*d" 5 1 }}`,
		"recursive define out": `{{ define "a" }}` + strings.Repeat("x", 4096) + `{{ template "a" }}{{ end }}{{ template "a" }}`,
	} {
		t.Run(name, func(t *testing.T) {
			refused(t, src, nil, "template.limit")
		})
	}
}

// A template that calls itself through define/template is cut by text/template's
// own depth limit: an error, not a stack overflow.
func TestRecursiveTemplateIsAnError(t *testing.T) {
	refused(t, `{{ define "a" }}{{ template "a" }}{{ end }}{{ template "a" }}`, nil, "template.exec")
	refused(t, `{{ define "a" }}{{ template "b" }}{{ end }}{{ define "b" }}{{ template "a" }}{{ end }}{{ template "a" }}`, nil, "template.exec")
}

func TestLimitsDoNotRefuseOrdinaryDecks(t *testing.T) {
	out, err, _ := expandBounded(t, `{{ range seq 100 }}{{ printf "%5d|%-5s|%.2f" . "ab" 1.5 }}{{ end }}{{ repeat 3 "-" }}{{ replace "a" "bb" "banana" }}`, nil)
	if err != nil || !strings.HasSuffix(out, "---bbbnbbnbb") {
		t.Fatalf("%q, %v", out, err)
	}
	if _, err := printf("%d%%", 5); err != nil {
		t.Error(err)
	}
	if _, err := repeat(-1, "x"); err == nil {
		t.Error("a negative repeat is an error, not a panic")
	}
}

// FuzzInclude resolves includes among three files whose contents the fuzzer
// writes: whatever they say, the expansion ends, and an include problem has its
// code.
func FuzzInclude(f *testing.F) {
	f.Add(`{{ include "b.md" }}`, `{{ include "c.md" }}`, `{{ include "deck.md" }}`)
	f.Add(`{{ include "deck.md" }}`, ``, ``)
	f.Add(`{{ range seq 3 }}{{ include "b.md" }}{{ end }}`, `{{ include "../w/c.md" }}`, `x`)
	f.Add(`{{ include "b.md" }}{{ include "b.md" }}`, `{{ include "b.md" }}`, ``)
	f.Add(`{{ define "a" }}{{ template "a" }}{{ end }}{{ template "a" }}`, ``, ``)
	f.Fuzz(func(t *testing.T, deck, b, c string) {
		files := vfs.NewMap(map[string][]byte{
			"/w/deck.md": []byte(deck), "/w/b.md": []byte(b), "/w/c.md": []byte(c),
		})
		done := make(chan error, 1)
		go func() {
			_, err := Expand(deck, Options{BaseDir: "/w", Name: "deck.md", FS: files})
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil && strings.Contains(err.Error(), "include cycle") && diag.CodeOf(err) != "include.cycle" {
				t.Fatalf("an include cycle without its code: %v", err)
			}
		case <-time.After(60 * time.Second):
			t.Fatal("the expansion did not end")
		}
	})
}
