package deck

import (
	"hash/maphash"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/yuin/goldmark/v2/parser"
)

// RenderCache keeps the HTML of a deck's slides between renders, so that an
// editor re-rendering the deck on every keystroke re-renders only the slides
// that changed. Marp for VS Code renders the whole deck on every update and
// leaves the rest to VS Code's preview, which morphs the DOM; go-slides
// renders in WebAssembly, where the rendering itself is what costs.
//
// A slide is reused when everything its rendering reads is unchanged: its
// source (the Markdown after templates, so an edited include or data file is
// seen), its resolved state (the directives cascaded into it, its page number
// and the total, kind, backgrounds, heading ids, notes…), the source lines its
// blocks map to, the transition of the slide before it, and the deck-wide
// context (global directives, heading divider, link reference definitions,
// render options). Anything deck-wide that changes drops every entry.
//
// A cache holds the slides of the last render only, so it is bounded by one
// deck; it is meant to live as long as an editor's session on one document.
// It is safe for concurrent use; renders through one cache are serialised.
type RenderCache struct {
	mu   sync.Mutex
	seed maphash.Seed
	ctx  string
	// slides are the last render's slides by the hash of their key; a hit is
	// verified against the whole key, so a collision is only a miss.
	slides map[uint64]cachedSlide
	// Hits and Misses count the slides reused and rendered by the last render.
	Hits, Misses int
}

// cachedSlide is a rendered slide and the key it was rendered for: its
// source (a substring of the deck's, not a copy) and the rest (meta).
type cachedSlide struct {
	src, meta, html string
}

// referencesKey serialises link reference definitions, sorted by label.
func referencesKey(refs []parser.Reference) string {
	if len(refs) == 0 {
		return ""
	}
	parts := make([]string, len(refs))
	for i, r := range refs {
		var k keyWriter
		k.str(string(r.Label()))
		k.str(string(r.Destination()))
		k.str(string(r.Title()))
		parts[i] = k.String()
	}
	slices.Sort(parts)
	return strings.Join(parts, "")
}

// keyWriter builds an unambiguous key: every field is length-prefixed.
type keyWriter struct{ strings.Builder }

func (k *keyWriter) str(s string) {
	k.WriteString(strconv.Itoa(len(s)))
	k.WriteByte(':')
	k.WriteString(s)
}

func (k *keyWriter) num(n int) {
	k.WriteString(strconv.Itoa(n))
	k.WriteByte(';')
}

func (k *keyWriter) flag(b bool) {
	if b {
		k.WriteByte('T')
	} else {
		k.WriteByte('F')
	}
}

func (k *keyWriter) dirs(d Dirs) {
	k.num(len(d.keys))
	for _, name := range d.keys {
		k.str(name)
		k.str(d.vals[name])
	}
}

func (k *keyWriter) strs(v []string) {
	k.num(len(v))
	for _, s := range v {
		k.str(s)
	}
}

// contextKey is everything deck-wide a slide's rendering reads.
func (d *Deck) contextKey(opts RenderOptions) string {
	var k keyWriter
	k.dirs(d.Global.Dirs)
	k.num(len(d.Global.HeadingDivider))
	for _, h := range d.Global.HeadingDivider {
		k.num(h)
	}
	k.flag(d.opts.NoEmoji)
	k.str(d.refs)
	k.num(int(opts.Frame))
	k.num(int(opts.HTML))
	k.str(opts.Lang)
	k.num(opts.Width)
	k.num(opts.Height)
	k.flag(opts.Highlight != nil)
	k.flag(opts.GeneratedContent)
	k.flag(opts.FitHeadings)
	k.flag(opts.Math != nil)
	k.str(opts.AfterContent)
	k.flag(opts.SourceLines)
	return k.String()
}

// slideKey is everything a slide's rendering reads of the slide and its
// neighbours, given the context.
// src is the slide's source, a substring of all (the deck's source as a
// string), and meta the rest of the key.
func (d *Deck) slideKey(i int, all string, sourceLines bool) (src, meta string) {
	s := d.Slides[i]
	end := len(all)
	if i+1 < len(d.Slides) {
		end = d.Slides[i+1].start
	}
	start := min(s.start, end)
	src = all[start:end]
	var k keyWriter
	k.num(s.Index)
	k.str(s.Kind)
	k.dirs(s.Dirs)
	k.num(s.Page)
	k.num(s.Total)
	k.flag(s.Paginated)
	k.strs(s.Notes)
	k.num(s.Fragments)
	k.num(len(s.Backgrounds))
	for _, b := range s.Backgrounds {
		k.str(b.URL)
		k.str(b.Alt)
		k.str(b.Size)
		k.str(b.Filter)
	}
	k.str(s.BackgroundSplit)
	k.str(s.BackgroundSplitSize)
	k.str(s.BackgroundDirection)
	k.strs(s.ScopedStyles)
	k.num(len(s.Headings))
	for _, h := range s.Headings {
		k.num(h.Level)
		k.str(h.Text)
		k.str(h.ID)
		k.str(h.Raw)
	}
	k.num(s.Line)
	if i > 0 {
		prev, ok := transitionAttr(d.Slides[i-1])
		k.flag(ok)
		k.str(prev)
	}
	if sourceLines {
		// The source line of every line of the slide: an edit above it that
		// adds or removes a line moves them, and with them its data-line.
		first := d.lineIndex(start)
		last := d.lineIndex(end)
		for l := first; l <= last; l++ {
			k.num(d.sourceLineOf(l))
		}
	}
	return src, k.String()
}

// RenderCached is Render through c: the slides whose key is in the cache
// are reused, the others rendered. The result is the same as Render's, byte
// for byte. A nil c renders everything.
func (d *Deck) RenderCached(opts RenderOptions, c *RenderCache) []Rendered {
	if c == nil {
		return d.Render(opts)
	}
	if opts.Width == 0 {
		opts.Width, opts.Height = 1280, 720
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.slides == nil {
		c.seed = maphash.MakeSeed()
	}
	ctx := d.contextKey(opts)
	if ctx != c.ctx {
		c.ctx, c.slides = ctx, nil
	}
	all := string(d.source) // one copy; the slides' keys are substrings of it
	next := make(map[uint64]cachedSlide, len(d.Slides))
	out := make([]Rendered, len(d.Slides))
	var r *slideRenderer
	c.Hits, c.Misses = 0, 0
	var h maphash.Hash
	h.SetSeed(c.seed)
	for i, s := range d.Slides {
		src, meta := d.slideKey(i, all, opts.SourceLines)
		h.Reset()
		h.WriteString(meta)
		h.WriteByte(0)
		h.WriteString(src)
		sum := h.Sum64()
		e, ok := c.slides[sum]
		if ok && e.meta == meta && e.src == src {
			c.Hits++
		} else {
			if r == nil {
				r = d.newSlideRenderer(opts)
			}
			e = cachedSlide{src: src, meta: meta, html: r.slide(s)}
			c.Misses++
		}
		next[sum] = e
		out[i] = Rendered{Slide: s, HTML: e.html}
	}
	c.slides = next
	return out
}
