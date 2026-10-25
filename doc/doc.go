// Package doc is a built deck as the exports need it: each slide's HTML in
// the box frame, the theme's packed style sheet and slide size, the
// headings and notes of each slide, and the deck's metadata. It imports
// nothing: a host that renders the deck in one place (a browser's render
// worker) and exports it in another (an export worker that carries no
// Markdown parser, highlighter or theme engine) hands one over as JSON.
//
// slides.Build.Doc makes one; the export package's writers take one.
package doc

// Doc is a built deck, as the exports read it.
type Doc struct {
	// CSS is the theme's packed style sheet, and Width and Height the slide
	// size in CSS pixels (theme.Packed).
	CSS    string  `json:"css"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	// The deck's metadata, for the documents' properties: the title (the
	// title directive, else the first heading of the lowest level), the
	// author, description and language directives, and the keywords.
	Title       string   `json:"title,omitempty"`
	Author      string   `json:"author,omitempty"`
	Description string   `json:"description,omitempty"`
	Lang        string   `json:"lang,omitempty"`
	Keywords    []string `json:"keywords,omitempty"`
	// The title and keywords directives as written (the office decks'
	// properties take them so).
	TitleDirective    string `json:"titleDirective,omitempty"`
	KeywordsDirective string `json:"keywordsDirective,omitempty"`
	// Slides are the slides in order.
	Slides []Slide `json:"slides"`
}

// Slide is one slide of a Doc.
type Slide struct {
	// HTML is the slide's markup, frame included.
	HTML string `json:"html"`
	// Headings are its headings in order, for the PDF's outline and the
	// office slides' names.
	Headings []Heading `json:"headings,omitempty"`
	// Notes are its speaker notes.
	Notes []string `json:"notes,omitempty"`
}

// Heading is one heading of a slide.
type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
}
