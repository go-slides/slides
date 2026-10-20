// Package viewer writes a deck as one self-contained HTML page: the slides,
// their style sheet, and a small script to present them — keyboard and
// pointer navigation, fragmented lists, a slide per URL hash, and printing
// every slide on its own page. It plays the part of marp-cli's templates.
package viewer

import (
	"encoding/json"
	"html"
	"strconv"
	"strings"

	"github.com/go-slides/slides/deck"
)

// Meta is the page's metadata (marp-cli's title, description, author,
// keywords, url and image global directives).
type Meta struct {
	Title, Description, Author, Keywords, URL, Image, Lang string
}

// Options configures Document.
type Options struct {
	Meta Meta
	// CSS is the packed theme style sheet.
	CSS string
	// Slides are the rendered slides, frames included.
	Slides []string
	// Bare leaves the script out: the slides stack vertically, as in
	// marp-cli's bare template.
	Bare bool
	// WatchWS, when not empty, is the address of the live reload socket the
	// page opens (see serve.Hub): a ws:// URL, or a path on the page's host.
	WatchWS string
	// Bespoke are the options of the presenting page (marp-cli's bespoke
	// template); the zero value is a page with none of them.
	Bespoke Bespoke
	// TransitionCSS is the keyframes of the built-in transitions the deck
	// uses, written after the transition styles (see deck.TransitionCSS).
	TransitionCSS string
	// Notes are the speaker notes of each slide, one entry for each comment;
	// they are written, escaped, for the presenter view when Bespoke.Presenter
	// is set. A slide without notes has none.
	Notes [][]string
}

// Bespoke are marp-cli's --bespoke.* options.
type Bespoke struct {
	// OSC is the on-screen controller (--bespoke.osc).
	OSC bool
	// Progress is the progress bar (--bespoke.progress).
	Progress bool
	// Transition plays the transitions of the deck with the View Transition
	// API where the browser has it (--bespoke.transition). Set it only for a
	// deck that has some: the page carries the script for it.
	Transition bool
	// Presenter is the presenter view (the p key, ?view=presenter): the
	// current slide, the next step, the notes, a timer, synchronised with the
	// page through localStorage. marp-cli's bespoke template always has it.
	Presenter bool
}

// DefaultBespoke are marp-cli's defaults: the controller, no progress bar,
// transitions, and the presenter view.
func DefaultBespoke() Bespoke {
	return Bespoke{OSC: true, Progress: false, Transition: true, Presenter: true}
}

// noteEscaper escapes what marp-cli escapes in a note: the three characters
// that make markup. A note is the deck author's text, shown to the
// presenter; it is never markup.
var noteEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// Document returns the page.
func Document(o Options) string {
	var b strings.Builder
	esc := html.EscapeString
	b.WriteString("<!DOCTYPE html>\n<html")
	if o.Meta.Lang != "" {
		b.WriteString(` lang="` + esc(o.Meta.Lang) + `"`)
	}
	b.WriteString(">\n<head>\n")
	// The order and the conditions are those of marp-cli's bespoke and bare
	// templates (lib/marp-cli-*.js, the pug-compiled head): no title, no
	// <title>; og:image:alt only with both; the keywords only from the
	// second one on (marp-cli tests keywords.length > 1).
	meta := func(attr, key, value string) {
		b.WriteString(`<meta ` + attr + `="` + key + `" content="` + esc(value) + `">` + "\n")
	}
	m := o.Meta
	if m.Title != "" {
		b.WriteString("<title>" + esc(m.Title) + "</title>\n")
		meta("property", "og:title", m.Title)
		if m.Image != "" {
			meta("property", "og:image:alt", m.Title)
		}
	}
	if m.Author != "" {
		meta("name", "author", m.Author)
		meta("property", "article:author", m.Author)
	}
	if m.Description != "" {
		meta("name", "description", m.Description)
		meta("property", "og:description", m.Description)
	}
	if strings.Contains(m.Keywords, ",") {
		meta("name", "keywords", m.Keywords)
	}
	if m.URL != "" {
		b.WriteString(`<link rel="canonical" href="` + esc(m.URL) + `">` + "\n")
		meta("property", "og:url", m.URL)
	}
	if m.Image != "" {
		meta("property", "og:image", m.Image)
	}
	b.WriteString("<meta charset=\"UTF-8\">\n")
	b.WriteString(`<meta name="viewport" content="width=device-width,height=device-height,initial-scale=1.0">` + "\n")
	b.WriteString(`<meta name="apple-mobile-web-app-capable" content="yes">` + "\n")
	b.WriteString(`<meta http-equiv="X-UA-Compatible" content="ie=edge">` + "\n")
	b.WriteString(`<meta property="og:type" content="website">` + "\n")
	card := "summary"
	if m.Title != "" && m.Image != "" {
		card = "summary_large_image"
	}
	meta("name", "twitter:card", card)
	b.WriteString(`<meta name="generator" content="go-slides">` + "\n")
	b.WriteString("<style>" + o.CSS + "</style>\n")
	if o.Bare {
		b.WriteString("<style>" + bareCSS + "</style>\n")
	} else {
		b.WriteString("<style>" + compact(presentCSS))
		if o.Bespoke.Presenter {
			b.WriteString(compact(presenterCSS))
		}
		b.WriteString("</style>\n")
		if o.Bespoke.Transition {
			b.WriteString("<style>" + compact(transitionCSS) + o.TransitionCSS + "</style>\n")
		}
	}
	b.WriteString("</head>\n<body>\n<div class=\"marpit\">\n")
	for _, s := range o.Slides {
		b.WriteString(s)
		b.WriteString("\n")
	}
	b.WriteString("</div>\n")
	if o.Bespoke.Presenter && !o.Bare {
		// Hidden by the style sheet: the presenter view takes them.
		for i, notes := range o.Notes {
			if len(notes) == 0 {
				continue
			}
			b.WriteString(`<div class="slides-note" data-index="` + strconv.Itoa(i) + `" tabindex="0">`)
			for _, n := range notes {
				b.WriteString("<p>" + noteEscaper.Replace(n) + "</p>")
			}
			b.WriteString("</div>\n")
		}
	}
	// Fitting headings are measured with every slide laid out: the
	// presenting styles hide all but one, so they are lifted for the time
	// of the measure.
	for _, s := range o.Slides {
		if strings.Contains(s, "data-fit-content") {
			b.WriteString("<script>document.body.classList.add('slides-fitting');" + deck.FitScript +
				"document.body.classList.remove('slides-fitting');</script>\n")
			break
		}
	}
	if !o.Bare {
		opts, _ := json.Marshal(struct {
			OSC       bool `json:"osc"`
			Progress  bool `json:"progress"`
			Presenter bool `json:"presenter"`
		}{o.Bespoke.OSC, o.Bespoke.Progress, o.Bespoke.Presenter})
		b.WriteString("<script>" + strings.Replace(compact(presentJS), "/*OPTS*/", string(opts), 1) + "</script>\n")
		if o.Bespoke.Transition {
			b.WriteString("<script>" + compact(transitionJS) + "</script>\n")
		}
	}
	if o.WatchWS != "" {
		// json.Marshal escapes <, > and &, so the address cannot end the script.
		ws, _ := json.Marshal(o.WatchWS)
		b.WriteString("<script>window.__slidesWatchWS=" + string(ws) + ";" + watchJS + "</script>\n")
	}
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

// watchJS reloads the page when its deck changes, over the socket marp-cli's
// watch script uses: "ready" when it is open, "reload" on a change, and a try
// again every 5 s when it was lost, reloading when it is back. An address
// that starts with a slash is on the page's own host.
const watchJS = `!function(){var u=window.__slidesWatchWS;if(!u)return;
if(u.charAt(0)==="/")u=(location.protocol==="https:"?"wss:":"ws:")+"//"+location.host+u;
var connect=function(reload){var ws=new WebSocket(u);
ws.addEventListener("open",function(){console.info("[slides] Observing the change of file...");if(reload)location.reload()});
ws.addEventListener("close",function(){console.warn("[slides] WebSocket for file watcher was disconnected. Try re-connecting in 5000ms...");setTimeout(function(){connect(true)},5000)});
ws.addEventListener("message",function(e){if(e.data==="reload")location.reload()});return ws};
connect(false)}();`

const bareCSS = `
html, body { margin: 0; background: #555; }
div.marpit > svg[data-marpit-svg] { display: block; width: 100%; height: auto; margin: 0 auto 1em; max-width: 100vw; }
@media print { div.marpit > svg[data-marpit-svg] { margin: 0; } }
`

const presentCSS = `
html, body { margin: 0; height: 100%; background: #000; overflow: hidden; }
div.marpit > svg[data-marpit-svg] { display: none; position: absolute; inset: 0; width: 100vw; height: 100vh; }
div.marpit > svg[data-marpit-svg].active, .slides-fitting div.marpit > svg[data-marpit-svg] { display: block; }
[data-marpit-fragment] { transition: opacity .2s; }
[data-marpit-fragment].hidden-fragment { opacity: 0; }
.slides-progress { position: fixed; left: 0; bottom: 0; height: 3px; background: rgba(128,128,128,.6); transition: width .2s; }
.slides-osc { position: fixed; left: 50%; bottom: 12px; transform: translateX(-50%); display: flex; align-items: center; gap: 2px;
  padding: 2px 6px; border-radius: 10px; background: rgba(24,24,24,.82); color: #fff; font: 13px/1 system-ui, -apple-system, sans-serif;
  opacity: 0; pointer-events: none; transition: opacity .25s; z-index: 10; -webkit-user-select: none; user-select: none; }
.slides-osc.active, .slides-osc:focus-within { opacity: 1; pointer-events: auto; }
.slides-osc button { appearance: none; background: none; border: 0; color: inherit; font: inherit; font-size: 20px; line-height: 1;
  width: 32px; height: 32px; cursor: pointer; border-radius: 6px; }
.slides-osc button:hover, .slides-osc button:focus-visible { background: rgba(255,255,255,.18); }
.slides-osc-page { min-width: 4.5em; text-align: center; font-variant-numeric: tabular-nums; }
@media print {
  html, body { height: auto; overflow: visible; background: #fff; }
  div.marpit > svg[data-marpit-svg] { display: block; position: static; }
  [data-marpit-fragment].hidden-fragment { opacity: 1; }
  .slides-progress, .slides-osc { display: none; }
}
`
