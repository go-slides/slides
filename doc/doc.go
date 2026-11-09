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
	// Media are the video files the slides link to, by the path the deck wrote
	// (![video](clip.mp4)): the office exports embed them as the video behind
	// the poster picture, and the PDF only links to them.
	Media map[string]Media `json:"media,omitempty"`
}

// VideoLinkPrefix starts the address of the link around the poster picture of
// a video file of the deck, in the markup the exports are made from: the web
// engine keeps only http(s) links and resolves them against the page, so the
// file's path follows it ("https://go-slides.invalid/video/clip.mp4"), and the
// exports tell their own from other links by it. The PDF writes the link
// as the path alone, which a PDF reader resolves against the file's place.
const VideoLinkPrefix = "https://go-slides.invalid/video/"

// Media is a file a deck carries.
type Media struct {
	// MIME is its media type (video/mp4, video/webm).
	MIME string `json:"mime"`
	// Data is the file.
	Data []byte `json:"data"`
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
	// Transition is the transition into this slide: the directive of the
	// slide before it, as marp-cli plays it when the viewer goes on. Nil for
	// the first slide and when the slide before has none.
	Transition *Transition `json:"transition,omitempty"`
	// Fragments counts the slide's fragmented list items (`*` bullets), which
	// the slide's HTML marks data-marpit-fragment="1" … "N" in reading order.
	Fragments int `json:"fragments,omitempty"`
}

// Transition is a slide transition: marp-cli's directive, "name duration".
type Transition struct {
	// Name is the directive's word: one of marp-cli's built-in names, or the
	// deck's own.
	Name string `json:"name"`
	// Duration is in seconds; 0 when the directive gives none.
	Duration float64 `json:"duration,omitempty"`
	// Custom is set for a name that is not a built-in: the deck's own
	// keyframes, which no export can carry.
	Custom bool `json:"custom,omitempty"`
}

// Heading is one heading of a slide.
type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
}
