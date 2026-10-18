// Package deck parses a Marp slide deck: Markdown split into slides, with
// Marpit's directives, image syntax, fragmented lists, speaker notes and
// scoped styles, plus this tool's two additions — Go template interpolation
// (package interp) and slide kinds (see Kind).
//
// The behaviour follows the Marpit and Marp Core sources rather than their
// documentation where the two differ; the tests pin each case against output
// produced by marp-cli 4.5.1 (Marp Core 4.4.0).
package deck

import (
	"github.com/yuin/goldmark/ast"
)

// Deck is a parsed slide deck.
type Deck struct {
	// Global holds the global directives, last value written winning.
	Global Global
	// Slides are the slides in order.
	Slides []*Slide
	// Styles are the deck's global CSS additions in order: the style
	// directive first, then every unscoped <style> element.
	Styles []string
	// Front is the front matter decoded with YAML's core schema, for
	// templates and tools; directives come from Global and Slide.Dirs.
	Front map[string]any
	// Kinds are the kinds the deck could use.
	Kinds Kinds

	source []byte
	opts   Options
	info   map[ast.Node]*nodeInfo
	doc    ast.Node
	// frontText is the front matter as written, templates expanded, and
	// hasFront whether there was one.
	frontText string
	hasFront  bool
}

// Global holds the global directives.
type Global struct {
	Dirs Dirs
	// HeadingDivider lists the heading levels that start a new slide.
	HeadingDivider []int
}

// Theme returns the theme directive.
func (g Global) Theme() string { return g.Dirs.Get("theme") }

// Get returns a global directive.
func (g Global) Get(name string) string { return g.Dirs.Get(name) }

// Slide is one slide.
type Slide struct {
	// Index is the slide's position, from 0.
	Index int
	// Kind is the kind the slide was given, "" if none.
	Kind string
	// Dirs are the local and spot directives in effect on the slide.
	Dirs Dirs
	// Page is the page number as Marpit counts it: slides whose paginate
	// directive is skip or hold do not advance it.
	Page int
	// Total is the last page number of the deck.
	Total int
	// Paginated reports whether the page number is shown.
	Paginated bool
	// Notes are the speaker notes: the slide's HTML comments that are not
	// directives.
	Notes []string
	// Fragments counts the fragmented list items.
	Fragments int
	// Backgrounds are the slide's background images, in order.
	Backgrounds []Background
	// BackgroundSplit is "left" or "right" when the backgrounds take one
	// side of the slide, and BackgroundSplitSize the width they take.
	BackgroundSplit, BackgroundSplitSize string
	// BackgroundDirection is "horizontal" (the default) or "vertical".
	BackgroundDirection string
	// ScopedStyles are the slide's <style scoped> contents.
	ScopedStyles []string
	// Headings are the slide's headings, for outlines.
	Headings []Heading

	nodes []ast.Node
	// start is where the slide's content begins in the deck source.
	start int
}

// Background is a background image.
type Background struct {
	URL    string
	Alt    string
	Size   string
	Filter string
}

// Heading is a heading of a slide.
type Heading struct {
	Level int
	Text  string
	ID    string
}

// Paginate returns the paginate directive in effect: "true", "false",
// "hold" or "skip".
func (s *Slide) Paginate() string {
	if v := s.Dirs.Get("paginate"); v != "" {
		return v
	}
	return "false"
}
