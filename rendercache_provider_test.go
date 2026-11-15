package slides

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-tex/math"

	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/emoji"
	"github.com/go-slides/slides/mathfont"
)

// A provider that is there (good) or not (not yet, or never).
type testFont struct{ good atomic.Bool }

func (f *testFont) Font() ([]byte, error) {
	if !f.good.Load() {
		return nil, errors.New("not fetched yet")
	}
	return math.DefaultFont(), nil
}

type testEmoji struct{ good atomic.Bool }

func (e *testEmoji) SVGByStem(string) (string, bool) {
	if !e.good.Load() {
		return "", false
	}
	return `<svg xmlns="http://www.w3.org/2000/svg"/>`, true
}

// the deck: a formula slide, an emoji slide, two plain ones
const providerDeck = "# Plain one\n\n---\n\nEnergy $E=mc^2$\n\n---\n\nSmile 😄 and :rocket:\n\n---\n\n# Plain two\n"

func renderThrough(t *testing.T, c *deck.RenderCache) string {
	t.Helper()
	page, _, err := HTML([]byte(providerDeck), Config{RenderCache: c})
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func fresh(t *testing.T) string {
	t.Helper()
	page, _, err := HTML([]byte(providerDeck), Config{})
	if err != nil {
		t.Fatal(err)
	}
	return page
}

func counts(t *testing.T, c *deck.RenderCache, hits, misses int) {
	t.Helper()
	if c.Hits != hits || c.Misses != misses {
		t.Errorf("hits %d misses %d, want %d and %d", c.Hits, c.Misses, hits, misses)
	}
}

// TestRenderCacheRendersFallbacksAgainWhenAProviderArrives: a slide that fell
// back for lack of the math font, or of an emoji picture, is rendered again
// once the provider is registered, and only that one; the plain slides are
// cache hits (counted by the cache, not timed).
func TestRenderCacheRendersFallbacksAgainWhenAProviderArrives(t *testing.T) {
	t.Cleanup(restoreMathFont)
	t.Cleanup(restoreEmoji)
	f, e := &testFont{}, &testEmoji{}
	mathfont.Register(f)
	emoji.Register(e)
	c := &deck.RenderCache{}

	before := renderThrough(t, c)
	counts(t, c, 0, 4)
	if !strings.Contains(before, `class="math-error"`) || !strings.Contains(before, ":rocket:") {
		t.Fatalf("no fallback before the providers: %.400s", before)
	}
	renderThrough(t, c)
	counts(t, c, 4, 0) // nothing registered, nothing re-rendered

	// the font arrives: only the formula slide is rendered again
	f.good.Store(true)
	mathfont.Register(f)
	page := renderThrough(t, c)
	counts(t, c, 3, 1)
	if strings.Contains(page, `class="math-error"`) || !strings.Contains(page, `class="slides-math"`) || !strings.Contains(page, ":rocket:") {
		t.Errorf("after the font: %.400s", page)
	}

	// the pictures arrive: only the emoji slide
	e.good.Store(true)
	emoji.Register(e)
	page = renderThrough(t, c)
	counts(t, c, 3, 1)
	if strings.Contains(page, ":rocket:") || !strings.Contains(page, `data-marp-twemoji`) {
		t.Errorf("after the pictures: %.400s", page)
	}
	if page != fresh(t) {
		t.Error("the cached page differs from a full render")
	}
	renderThrough(t, c)
	counts(t, c, 4, 0)
}

// TestRenderCacheWithAProviderThatKeepsFailing: a provider that never answers
// costs one more render of the fallback slides per Register, no loop, and the
// cache stays the size of the deck.
func TestRenderCacheWithAProviderThatKeepsFailing(t *testing.T) {
	t.Cleanup(restoreMathFont)
	t.Cleanup(restoreEmoji)
	f, e := &testFont{}, &testEmoji{} // never good
	c := &deck.RenderCache{}
	for i := 0; i < 5; i++ {
		mathfont.Register(f)
		emoji.Register(e)
		renderThrough(t, c)
		if i == 0 {
			counts(t, c, 0, 4)
		} else {
			counts(t, c, 2, 2) // the two fallbacks, once per Register
		}
		for j := 0; j < 3; j++ {
			renderThrough(t, c)
			counts(t, c, 4, 0) // and not in a loop
		}
	}
	if n := c.Len(); n != 4 {
		t.Errorf("the cache holds %d slides, the deck has 4", n)
	}
}

// TestRenderCacheRegisterWhileRendering is for go test -race: providers are
// registered while decks render through one cache; once they are good, the
// next render is typeset and drawn.
func TestRenderCacheRegisterWhileRendering(t *testing.T) {
	t.Cleanup(restoreMathFont)
	t.Cleanup(restoreEmoji)
	f, e := &testFont{}, &testEmoji{}
	mathfont.Register(f)
	emoji.Register(e)
	c := &deck.RenderCache{}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				renderThrough(t, c)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			f.good.Store(i%2 == 1)
			e.good.Store(i%2 == 1)
			mathfont.Register(f)
			emoji.Register(e)
		}
	}()
	wg.Wait()
	f.good.Store(true)
	e.good.Store(true)
	mathfont.Register(f)
	emoji.Register(e)
	if page := renderThrough(t, c); page != fresh(t) || strings.Contains(page, `class="math-error"`) || strings.Contains(page, ":rocket:") {
		t.Errorf("after the last Register: %.400s", page)
	}
}
