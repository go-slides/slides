package editor

import (
	"strings"
	"testing"

	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/internal/benchdeck"
	"github.com/go-slides/slides/internal/lru"
)

// editedBenchDeck returns the 100-slide deck and the same deck with one
// character typed at the end of a paragraph line in the middle.
func editedBenchDeck() (string, string) {
	src := string(benchdeck.Large(100))
	at := len(src) / 2
	at += strings.IndexByte(src[at:], '\n')
	for strings.HasSuffix(src[:at], "---") || strings.HasSuffix(src[:at], "\n") {
		at += 1 + strings.IndexByte(src[at+1:], '\n')
	}
	return src, src[:at] + "x" + src[at:]
}

// Typing into one slide re-renders that slide only, and the preview is the
// one a fresh render gives.
func TestRenderEditReRendersOneSlide(t *testing.T) {
	src, edited := editedBenchDeck()
	path := "/w/edit-one-slide.md"
	first := Render(Request{Path: path, Source: src})
	second := Render(Request{Path: path, Source: edited})
	c := renderCacheFor(path)
	if c.Misses != 1 || c.Hits != 99 {
		t.Fatalf("after one edit: %d slides rendered, %d reused", c.Misses, c.Hits)
	}
	renderCaches.Put(lru.KeyOf(path), &deck.RenderCache{}) // a fresh cache: a full render
	fresh := Render(Request{Path: path, Source: edited})
	if second.HTML != fresh.HTML || first.HTML == second.HTML {
		t.Fatal("the incremental preview differs from a full render")
	}
}

// BenchmarkRenderEdit is the preview after an edit: the 100-slide deck with
// one character typed into a paragraph of slide 50, alternating with the
// deck before the edit, as an editor sees it keystroke after keystroke.
func BenchmarkRenderEdit(b *testing.B) {
	src, edited := editedBenchDeck()
	reqs := []Request{req(src, nil), req(edited, nil)}
	b.SetBytes(int64(len(src)))
	i := 0
	for b.Loop() {
		if res := Render(reqs[i%2]); res.Slides != 100 {
			b.Fatalf("%d slides, %+v", res.Slides, res.Diagnostics)
		}
		i++
	}
}
