// Package editor is what an editor extension needs from go-slides, as plain
// data: a preview of the deck, its problems positioned on lines, and the
// completions and hovers for directives, kinds and template functions.
//
// It is the logic of the VS Code extension (go-slides/vscode), which runs it
// compiled to WebAssembly and only translates its results into the editor's
// types. Every entry point takes a Request: the deck and the workspace files
// the editor has read. A build reads nothing else (see vfs.Map); the files it
// wanted and did not get come back in Missing, for the editor to read and ask
// again.
package editor

import (
	"html"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/project"
	"github.com/go-slides/slides/vfs"
)

// Request is a deck and what the editor knows around it.
type Request struct {
	// Path is the deck's path; relative paths in the deck resolve from its
	// directory.
	Path string `json:"path"`
	// Source is the deck's text, as the editor holds it (unsaved edits
	// included).
	Source string `json:"source"`
	// Files are the workspace files the editor has read, by path.
	Files map[string]string `json:"files,omitempty"`
	// Dirs are directories the editor has listed, with the names in them;
	// a subdirectory's name ends with "/". A listed file the editor has not
	// read comes back in Missing if the build reads it.
	Dirs map[string][]string `json:"dirs,omitempty"`
	// HTML allows all raw HTML. The editor sets it only when the user asked
	// for it and trusts the workspace; a configuration file's html: true is
	// not enough, since the workspace wrote it.
	HTML bool `json:"html,omitempty"`
	// NoConfig ignores slides.yaml and slides.hcl.
	NoConfig bool `json:"noConfig,omitempty"`
}

// fs is the request's file system: its files, the deck itself included.
func (r Request) fs() *vfs.Map {
	files := make(map[string][]byte, len(r.Files)+1)
	for k, v := range r.Files {
		files[k] = []byte(v)
	}
	files[r.Path] = []byte(r.Source)
	m := vfs.NewMap(files)
	for d, names := range r.Dirs {
		m.Dir(d)
		for _, n := range names {
			if sub, ok := strings.CutSuffix(n, "/"); ok {
				m.Dir(filepath.Join(d, sub))
			} else if _, held := files[filepath.Join(d, n)]; !held {
				m.Listed(filepath.Join(d, n))
			}
		}
	}
	return m
}

// config assembles the build configuration over the request's files.
func (r Request) config(m *vfs.Map) (slides.Config, error) {
	cfg, err := project.Load(r.Path, project.Options{FS: m, NoConfig: r.NoConfig})
	// The preview syncs its scrolling with the editor by source line.
	cfg.Parse.LineMap = true
	cfg.SourceLines = true
	if r.HTML {
		cfg.HTML = deck.HTMLAll
	} else {
		cfg.HTML = deck.HTMLAllowlist
	}
	return cfg, err
}

// Result is a preview.
type Result struct {
	// HTML is the preview's markup: the theme's style element and the
	// slides, each an inline SVG that scales to the preview's width.
	HTML string `json:"html"`
	// Slides counts the slides.
	Slides int `json:"slides"`
	// Diagnostics are the deck's problems; an error leaves HTML empty.
	Diagnostics []Diagnostic `json:"diagnostics"`
	// Missing are the files the build wanted and the request did not hold.
	Missing []string `json:"missing"`
}

// PreviewCSS lays the slides out in the editor's preview: one under the
// other, full width, with a gap and a shadow.
const PreviewCSS = `#go-slides-preview{margin:0 auto}` +
	`#go-slides-preview>svg[data-marpit-svg]{display:block;width:100%;height:auto;margin:0 0 1.5em;` +
	`box-shadow:0 1px 4px rgba(0,0,0,.25)}`

// Render builds the deck's preview.
func Render(r Request) Result {
	m := r.fs()
	res := Result{Diagnostics: []Diagnostic{}}
	cfg, err := r.config(m)
	if err != nil {
		res.Diagnostics = Diagnose(err, r.Path, r.Source)
		res.Missing = m.Missing()
		return res
	}
	b, err := slides.Render([]byte(r.Source), cfg, deck.FrameSVG)
	res.Missing = m.Missing()
	if err != nil {
		res.Diagnostics = Diagnose(err, r.Path, r.Source)
		return res
	}
	for _, w := range b.Warnings {
		res.Diagnostics = append(res.Diagnostics, warning(w, r.Path, r.Source))
	}
	var sb strings.Builder
	sb.WriteString(`<style id="go-slides-style">`)
	// The theme is CSS the deck chose; a </style> inside it would end the
	// element early and let the rest be read as markup.
	sb.WriteString(strings.ReplaceAll(b.Packed.CSS, "</", `<\/`))
	sb.WriteString(PreviewCSS)
	sb.WriteString(`</style><div id="go-slides-preview" class="marpit" data-slides="`)
	sb.WriteString(strconv.Itoa(len(b.Slides)))
	sb.WriteString(`" data-theme="`)
	sb.WriteString(html.EscapeString(b.Theme.Name))
	sb.WriteString(`">`)
	for _, s := range b.Slides {
		sb.WriteString(s.HTML)
	}
	sb.WriteString(`</div>`)
	res.HTML = sb.String()
	res.Slides = len(b.Slides)
	return res
}

// Detect reports whether a Markdown document is a deck to preview: its
// front matter sets slides: true, or marp: true when marp is set. The
// editor leaves marp: true to Marp for VS Code when that extension is
// installed, so that the two do not both render the same document. An HCL
// front matter (---hcl) says slides = true or marp = true.
func Detect(src string, marp bool) bool {
	fm, isHCL, ok := frontMatter(src)
	if !ok {
		return false
	}
	sep := ":"
	if isHCL {
		sep = "="
	}
	for line := range strings.SplitSeq(fm, "\n") {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue // blank, or nested under another key
		}
		key, val, found := strings.Cut(line, sep)
		if !found {
			continue
		}
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if i := strings.Index(val, " #"); i >= 0 {
			val = strings.TrimSpace(val[:i]) // a trailing comment
		}
		if val != "true" {
			continue
		}
		if key == "slides" || marp && key == "marp" {
			return true
		}
	}
	return false
}

// frontMatter returns the text of a leading front matter, the way
// markdown-it-front-matter (and deck.Parse) delimit it: a first line of at
// least three dashes, closed by a line of at least as many.
func frontMatter(src string) (text string, isHCL, ok bool) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	first, rest, found := strings.Cut(src, "\n")
	n := len(first) - len(strings.TrimLeft(first, "-"))
	if n < 3 || !found {
		return "", false, false
	}
	isHCL = strings.TrimRight(first, " \t") == "---hcl"
	var b strings.Builder
	for rest != "" {
		line, after, _ := strings.Cut(rest, "\n")
		t := strings.TrimLeft(line, " ")
		d := len(t) - len(strings.TrimLeft(t, "-"))
		if len(line)-len(t) <= 3 && d >= n && strings.Trim(t[d:], " ") == "" {
			return b.String(), isHCL, true
		}
		b.WriteString(line + "\n")
		rest = after
	}
	return b.String(), isHCL, true
}
