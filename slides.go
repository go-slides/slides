// Package slides turns a Marp slide deck — Markdown with Marpit directives,
// Go template interpolation and slide kinds — into a presentation.
//
// The work is split across packages: interp expands templates, deck parses
// the Markdown and renders slides, theme packs Marp theme CSS, and viewer
// writes the HTML page. This package wires them together.
package slides

import (
	"fmt"

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
	d, err := deck.Parse(src, cfg.Parse)
	if err != nil {
		return nil, err
	}
	set := cfg.Themes
	if set == nil {
		set = theme.NewSet()
	}
	b := &Build{Deck: d}
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
	})
	return b, nil
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
			Title:       g.Get("title"),
			Description: g.Get("description"),
			Author:      g.Get("author"),
			Keywords:    g.Get("keywords"),
			URL:         g.Get("url"),
			Image:       g.Get("image"),
			Lang:        lang,
		},
		CSS:    b.Packed.CSS,
		Slides: slides,
		Bare:   cfg.Bare,
	})
	return page, b, nil
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
