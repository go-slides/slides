// Package viewer writes a deck as one self-contained HTML page: the slides,
// their style sheet, and a small script to present them — keyboard and
// pointer navigation, fragmented lists, a slide per URL hash, and printing
// every slide on its own page. It plays the part of marp-cli's templates.
package viewer

import (
	"html"
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
}

// Document returns the page.
func Document(o Options) string {
	var b strings.Builder
	esc := html.EscapeString
	b.WriteString("<!DOCTYPE html>\n<html")
	if o.Meta.Lang != "" {
		b.WriteString(` lang="` + esc(o.Meta.Lang) + `"`)
	}
	b.WriteString(">\n<head>\n<meta charset=\"UTF-8\">\n")
	b.WriteString(`<meta name="viewport" content="width=device-width,height=device-height,initial-scale=1.0">` + "\n")
	b.WriteString(`<meta name="generator" content="go-slides">` + "\n")
	title := o.Meta.Title
	if title == "" {
		title = "Slides"
	}
	b.WriteString("<title>" + esc(title) + "</title>\n")
	meta := func(attr, key, value string) {
		if value != "" {
			b.WriteString(`<meta ` + attr + `="` + key + `" content="` + esc(value) + `">` + "\n")
		}
	}
	meta("name", "description", o.Meta.Description)
	meta("name", "author", o.Meta.Author)
	meta("name", "keywords", o.Meta.Keywords)
	meta("property", "og:title", o.Meta.Title)
	meta("property", "og:description", o.Meta.Description)
	meta("property", "og:url", o.Meta.URL)
	meta("property", "og:image", o.Meta.Image)
	if o.Meta.URL != "" {
		b.WriteString(`<link rel="canonical" href="` + esc(o.Meta.URL) + `">` + "\n")
	}
	b.WriteString("<style>" + o.CSS + "</style>\n")
	if o.Bare {
		b.WriteString("<style>" + bareCSS + "</style>\n")
	} else {
		b.WriteString("<style>" + presentCSS + "</style>\n")
	}
	b.WriteString("</head>\n<body>\n<div class=\"marpit\">\n")
	for _, s := range o.Slides {
		b.WriteString(s)
		b.WriteString("\n")
	}
	b.WriteString("</div>\n")
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
		b.WriteString("<script>" + presentJS + "</script>\n")
	}
	b.WriteString("</body>\n</html>\n")
	return b.String()
}

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
@media print {
  html, body { height: auto; overflow: visible; background: #fff; }
  div.marpit > svg[data-marpit-svg] { display: block; position: static; }
  [data-marpit-fragment].hidden-fragment { opacity: 1; }
  .slides-progress { display: none; }
}
`

// presentJS shows one slide at a time. Fragments reveal one by one; the
// URL hash names the slide, so a reload or a link lands on it.
const presentJS = `
(() => {
  const slides = [...document.querySelectorAll('div.marpit > svg[data-marpit-svg]')];
  if (!slides.length) return;
  const bar = document.createElement('div');
  bar.className = 'slides-progress';
  document.body.appendChild(bar);
  const frags = s => [...s.querySelectorAll('[data-marpit-fragment]')]
    .sort((a, b) => a.dataset.marpitFragment - b.dataset.marpitFragment);
  let cur = 0, step = 0;
  const show = (i, f) => {
    cur = Math.max(0, Math.min(slides.length - 1, i));
    const fs = frags(slides[cur]);
    step = f === undefined ? 0 : Math.max(0, Math.min(fs.length, f));
    slides.forEach((s, j) => s.classList.toggle('active', j === cur));
    fs.forEach((e, j) => e.classList.toggle('hidden-fragment', j >= step));
    bar.style.width = (slides.length > 1 ? cur / (slides.length - 1) * 100 : 100) + '%';
    const hash = '#' + (cur + 1);
    if (location.hash !== hash) history.replaceState(null, '', hash);
  };
  const next = () => {
    const n = frags(slides[cur]).length;
    if (step < n) show(cur, step + 1); else if (cur < slides.length - 1) show(cur + 1, 0);
  };
  const prev = () => {
    if (step > 0) show(cur, step - 1);
    else if (cur > 0) show(cur - 1, frags(slides[cur - 1]).length);
  };
  document.addEventListener('keydown', e => {
    if (e.altKey || e.ctrlKey || e.metaKey) return;
    switch (e.key) {
      case 'ArrowRight': case 'ArrowDown': case 'PageDown': case ' ': case 'Enter': next(); break;
      case 'ArrowLeft': case 'ArrowUp': case 'PageUp': case 'Backspace': prev(); break;
      case 'Home': show(0); break;
      case 'End': show(slides.length - 1, frags(slides[slides.length - 1]).length); break;
      case 'f': document.fullscreenElement ? document.exitFullscreen() : document.documentElement.requestFullscreen(); break;
      default: return;
    }
    e.preventDefault();
  });
  document.addEventListener('click', e => {
    if (e.target.closest('a')) return;
    (e.clientX < innerWidth / 3 ? prev : next)();
  });
  addEventListener('hashchange', () => show(parseInt(location.hash.slice(1), 10) - 1 || 0));
  addEventListener('beforeprint', () => slides.forEach(s => s.classList.add('active')));
  addEventListener('afterprint', () => show(cur, step));
  show((parseInt(location.hash.slice(1), 10) || 1) - 1);
})();
`
