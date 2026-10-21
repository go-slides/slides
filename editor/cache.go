package editor

import (
	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/internal/lru"
)

// renderCachesSize bounds the documents whose slides are kept: one per deck
// open in a preview, a few at once. A cache holds one deck's slides (see
// deck.RenderCache), so the whole is bounded by a few decks.
const renderCachesSize = 4

var renderCaches = lru.New[*deck.RenderCache](renderCachesSize)

// renderCacheFor returns the render cache of the document at path, made on
// first use; the least recently previewed document's is dropped past the
// bound.
func renderCacheFor(path string) *deck.RenderCache {
	k := lru.KeyOf(path)
	if c, ok := renderCaches.Get(k); ok {
		return c
	}
	c := &deck.RenderCache{}
	renderCaches.Put(k, c)
	return c
}
