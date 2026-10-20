// Package slides turns a Marp slide deck — Markdown with Marpit directives,
// Go template interpolation and slide kinds — into a presentation.
//
// The work is split across packages: interp expands templates, deck parses
// the Markdown and renders slides, theme packs Marp theme CSS, and viewer
// writes the HTML page. This package wires them together.
package slides

import (
	"fmt"
	"strings"

	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/theme"
	"github.com/go-slides/slides/viewer"
)

// Config configures a build.
type Config struct {
	// Parse configures template expansion and kinds.
	Parse deck.Options
	// Themes holds the themes a deck can name; nil means the built-in
	// ones.
	Themes *theme.Set
	// HTML is the raw HTML policy.
	HTML deck.HTMLPolicy
	// Lang is the default lang attribute.
	Lang string
	// Bare writes the slides without the presenting script.
	Bare bool
	// NoMath leaves $…$ and $$…$$ as text (Marp Core's math: false).
	NoMath bool
	// NoEmoji leaves :shortcodes: and Unicode emoji as text (Marp Core's
	// emoji: false) instead of drawing them as Twemoji images.
	NoEmoji bool
	// Override holds the global directives the command line sets, which win
	// over the deck's own, as marp-cli's --title, --theme ... do.
	Override Override
	// WatchWS is the address of the live reload socket the HTML page opens
	// (see viewer.Options.WatchWS); empty for none.
	WatchWS string
}

// Override is marp-cli's command-line global directives. Empty ones are not
// set.
type Override struct {
	Title, Description, Author, Keywords, URL, Image, Theme string
}

// apply sets the overrides on a parsed deck. marp-cli appends them to the
// deck as a comment after everything else, so they come last and win; an
// invalid URL is ignored with a warning, and keywords that come to nothing
// are not set.
func (o Override) apply(d *deck.Deck) {
	set := func(name, v string) {
		if v != "" {
			d.Global.Dirs.Set(name, v)
		}
	}
	set("title", o.Title)
	set("description", o.Description)
	set("author", o.Author)
	if len(deck.KeywordList(o.Keywords)) > 0 {
		set("keywords", o.Keywords)
	}
	if o.URL != "" && !deck.ValidURL(o.URL) {
		d.Warnings = append(d.Warnings, "specified canonical URL is ignored since invalid URL: "+o.URL)
	} else {
		set("url", o.URL)
	}
	set("image", o.Image)
	set("theme", o.Theme)
}

// Build is a parsed and rendered deck.
type Build struct {
	Deck   *deck.Deck
	Theme  *theme.Theme
	Packed theme.Packed
	Slides []deck.Rendered
	// Warnings are problems that did not stop the build.
	Warnings []string
}

// Render parses and renders a deck for a frame.
func Render(src []byte, cfg Config, frame deck.Frame) (*Build, error) {
	popts := cfg.Parse
	popts.NoEmoji = popts.NoEmoji || cfg.NoEmoji
	d, err := deck.Parse(src, popts)
	if err != nil {
		return nil, err
	}
	set := cfg.Themes
	if set == nil {
		set = theme.NewSet()
	}
	cfg.Override.apply(d)
	b := &Build{Deck: d, Warnings: d.Warnings}
	name := d.Global.Theme()
	if name != "" && !set.Has(name) {
		b.Warnings = append(b.Warnings, fmt.Sprintf("theme %q is not known; using %q (known: %v)", name, set.Resolve("").Name, set.Names()))
	}
	b.Theme = set.Resolve(name)

	after := []string{}
	after = append(after, d.Styles...)
	for _, s := range d.Slides {
		for _, scoped := range s.ScopedStyles {
			after = append(after, theme.Scope(scoped, deck.ScopeAttr(s)))
		}
	}
	opts := theme.PackOptions{
		After:          after,
		Before:         emojiCSS(popts),
		Printable:      true,
		ContainerQuery: true,
		Size:           d.Global.Get("size"),
	}
	switch frame {
	case deck.FrameSVG:
		opts.Slide = []string{"svg", "foreignObject", "section"}
		opts.InlineSVG = true
	case deck.FrameBox:
		opts.Slide = []string{"div.marpit-svg", "div.marpit-fo", "section"}
		opts.InlineSVG = true
		opts.GeneratedContent = true
	}
	b.Packed = set.Pack(b.Theme, opts)
	b.Slides = d.Render(deck.RenderOptions{
		Frame:            frame,
		HTML:             cfg.HTML,
		Lang:             cfg.Lang,
		Width:            int(b.Packed.Width),
		Height:           int(b.Packed.Height),
		GeneratedContent: frame == deck.FrameBox,
		AfterContent:     b.Packed.After,
		Math:             mathFor(cfg),
		Highlight:        Highlight,
		FitHeadings:      true,
	})
	return b, nil
}

// emojiCSS is the rule Marp Core adds to the theme when emoji are images.
func emojiCSS(o deck.Options) []string {
	if o.NoEmoji {
		return nil
	}
	return []string{deck.EmojiCSS}
}

func mathFor(cfg Config) func(string, bool) (string, error) {
	if cfg.NoMath {
		return nil
	}
	return Math
}

// HTML builds the presentation as one self-contained HTML page.
func HTML(src []byte, cfg Config) (string, *Build, error) {
	b, err := Render(src, cfg, deck.FrameSVG)
	if err != nil {
		return "", nil, err
	}
	g := b.Deck.Global
	lang := or(g.Get("lang"), cfg.Lang)
	slides := make([]string, len(b.Slides))
	for i, s := range b.Slides {
		slides[i] = s.HTML
	}
	page := viewer.Document(viewer.Options{
		Meta: viewer.Meta{
			Title:       Title(b.Deck),
			Description: g.Get("description"),
			Author:      g.Get("author"),
			Keywords:    strings.Join(deck.KeywordList(g.Get("keywords")), ","),
			URL:         g.Get("url"),
			Image:       g.Get("image"),
			Lang:        lang,
		},
		CSS:    b.Packed.CSS,
		Slides: slides,
		Bare:    cfg.Bare,
		WatchWS: cfg.WatchWS,
	})
	return page, b, nil
}

// Title is the deck's title: the title directive, else the first heading of
// the lowest level, as marp-cli finds it.
func Title(d *deck.Deck) string {
	if d.Global.Dirs.Has("title") {
		return d.Global.Get("title")
	}
	return d.DetectTitle()
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
