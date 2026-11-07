package editor

import (
	"fmt"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/go-slides/slides/internal/benchdeck"
)

// The limits leave a large, ordinary deck alone: the 100-slide bench deck, and a
// templated one that loops over its slides and their items.
func TestLargeDecksStillRender(t *testing.T) {
	r := renderBounded(t, string(benchdeck.Large(100)), nil)
	if r.HTML == "" || len(r.Diagnostics) != 0 || r.Slides < 100 {
		t.Fatalf("bench deck: %d slides, %+v", r.Slides, r.Diagnostics)
	}
	src := front + "{{ range seq 100 }}\n# Slide {{ . }}\n\n{{ range seq 20 }}- item {{ . }}\n{{ end }}\n---\n{{ end }}"
	r = renderBounded(t, src, nil)
	if r.HTML == "" || len(r.Diagnostics) != 0 || r.Slides < 100 {
		t.Fatalf("templated deck: %d slides, %+v", r.Slides, r.Diagnostics)
	}
}

// renderBounded renders a hostile deck as the preview does (and as the
// playground's worker does for a shared link), under a memory limit and a time
// bound, in a goroutine so that a regression fails the test, not the process.
func renderBounded(t *testing.T, src string, files map[string]string) Result {
	t.Helper()
	prev := debug.SetMemoryLimit(768 << 20)
	defer debug.SetMemoryLimit(prev)
	ch := make(chan Result, 1)
	start := time.Now()
	go func() { ch <- Render(Request{Path: "/w/deck.md", Source: src, Files: files}) }()
	select {
	case r := <-ch:
		if took := time.Since(start); took > 10*time.Second {
			t.Errorf("rendered in %v: too slow", took)
		}
		return r
	case <-time.After(30 * time.Second):
		t.Fatal("the render did not finish in 30 s")
	}
	return Result{}
}

// wantRefusal checks the render is refused with a diagnostic of that code, and
// produced no preview.
func wantRefusal(t *testing.T, r Result, code string) {
	t.Helper()
	if r.HTML != "" || len(r.Diagnostics) == 0 {
		t.Fatalf("not refused: %d diagnostics, %d bytes of HTML", len(r.Diagnostics), len(r.HTML))
	}
	for _, d := range r.Diagnostics {
		if d.Code == code {
			return
		}
	}
	t.Fatalf("no diagnostic with code %q: %+v", code, r.Diagnostics)
}

const front = "---\nslides: true\n---\n\n"

// go-slides#67: a deck that includes itself overflowed the stack.
func TestHostileDecksAreRefused(t *testing.T) {
	for name, tc := range map[string]struct {
		src   string
		files map[string]string
		code  string
	}{
		"self include":   {front + "# x\n\n{{ include \"deck.md\" }}\n", nil, "include.cycle"},
		"mutual include": {front + "{{ include \"b.md\" }}\n", map[string]string{"/w/b.md": "{{ include \"deck.md\" }}"}, "include.cycle"},
		"include chain": {front + "{{ include \"f0.md\" }}\n", func() map[string]string {
			m := map[string]string{}
			for i := 0; i < 40; i++ {
				m[fmt.Sprintf("/w/f%d.md", i)] = fmt.Sprintf(`{{ include "f%d.md" }}`, i+1)
			}
			return m
		}(), "include.limit"},
		"seq bomb":        {front + "{{ range seq 1000000000 }}x{{ end }}\n", nil, "template.limit"},
		"output bomb":     {front + `{{ range seq 1000000 }}{{ repeat 1000 "x" }}{{ end }}` + "\n", nil, "template.limit"},
		"nested quotes":   {front + strings.Repeat("> ", 100000) + "x\n", nil, "deck.limit"},
		"deep indent":     {front + strings.Repeat(" ", 5000) + "- a\n", nil, "deck.limit"},
		"quotes by range": {front + `{{ range seq 3 }}` + strings.Repeat(">", 150) + " x\n{{ end }}", nil, "deck.limit"},
	} {
		t.Run(name, func(t *testing.T) { wantRefusal(t, renderBounded(t, tc.src, tc.files), tc.code) })
	}
}

// A self-including deck in a request whose Files hold the deck's own path too
// (the playground's share link), and a kinds file that extends itself.
func TestHostileKindsAreRefused(t *testing.T) {
	src := "---\nslides: true\nkinds: kinds.yaml\n---\n\n<!-- _kind: a -->\n\n# x\n"
	r := renderBounded(t, src, map[string]string{"/w/kinds.yaml": "a:\n  extends: b\nb:\n  extends: a\n"})
	wantRefusal(t, r, "kind.cycle")

	var long strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&long, "k%d:\n  extends: k%d\n", i, i+1)
	}
	long.WriteString("k200:\n  class: x\n")
	src = "---\nslides: true\nkinds: kinds.yaml\n---\n\n<!-- _kind: k0 -->\n\n# x\n"
	wantRefusal(t, renderBounded(t, src, map[string]string{"/w/kinds.yaml": long.String()}), "kind.cycle")
}

func TestOrdinaryNestingIsRendered(t *testing.T) {
	src := front + "> > > a quote\n\n- a\n  - b\n    - c\n\n{{ include \"b.md\" }}\n{{ include \"b.md\" }}\n"
	r := renderBounded(t, src, map[string]string{"/w/b.md": "included\n"})
	if r.HTML == "" || len(r.Diagnostics) != 0 {
		t.Fatalf("%+v", r.Diagnostics)
	}
}
