package deck

import (
	"io"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

// HTMLPolicy says what raw HTML in the Markdown may do.
type HTMLPolicy int

const (
	// HTMLAllowlist keeps the elements and attributes Marp Core considers
	// safe and escapes the rest, as Marp Core does by default.
	HTMLAllowlist HTMLPolicy = iota
	// HTMLAll keeps all raw HTML (marp --html).
	HTMLAll
	// HTMLNone escapes all raw HTML.
	HTMLNone
)

// attrRule sanitizes one attribute value; it returns "" to drop the value.
type attrRule func(string) string

func keep(v string) string { return v }

// schemes is Marp Core's URL filter: a value with a colon must start with
// one of the schemes (an entry holding a colon is a prefix).
func schemes(allowed ...string) attrRule {
	return func(v string) string {
		if !strings.Contains(v, ":") {
			return v
		}
		t := strings.ToLower(strings.TrimSpace(v))
		scheme, _, _ := strings.Cut(t, ":")
		for _, a := range allowed {
			if scheme == a || strings.Contains(a, ":") && strings.HasPrefix(t, a) {
				return v
			}
		}
		return ""
	}
}

var (
	httpURL  = schemes("http", "https")
	imageURL = schemes("http", "https", "data:image/")
	srcset   = func(v string) string {
		for _, p := range strings.Split(v, ",") {
			if imageURL(p) == "" {
				return ""
			}
		}
		return v
	}
	dir = func(v string) string {
		if l := strings.ToLower(v); l == "rtl" || l == "ltr" || l == "auto" {
			return l
		}
		return ""
	}
)

// allowlist is Marp Core's default (src/html/allowlist.ts in marp-core 4.4).
var allowlist = func() map[string]map[string]attrRule {
	global := map[string]attrRule{"class": keep, "dir": dir, "lang": keep, "title": keep}
	with := func(extra map[string]attrRule) map[string]attrRule {
		m := map[string]attrRule{}
		for k, v := range global {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	flags := func(names ...string) map[string]attrRule {
		m := map[string]attrRule{}
		for _, n := range names {
			m[n] = keep
		}
		return m
	}
	plus := func(a, b map[string]attrRule) map[string]attrRule {
		for k, v := range b {
			a[k] = v
		}
		return a
	}
	cell := flags("width", "rowspan", "colspan", "align", "valign")
	section := flags("align", "valign")
	cols := flags("align", "valign", "span", "width")
	l := map[string]map[string]attrRule{
		"a":          with(plus(flags("name", "rel", "target"), map[string]attrRule{"href": httpURL})),
		"audio":      with(plus(flags("autoplay", "controls", "loop", "muted", "preload"), map[string]attrRule{"src": httpURL})),
		"blockquote": with(map[string]attrRule{"cite": httpURL}),
		"col":        with(cols),
		"colgroup":   with(cols),
		"del":        with(map[string]attrRule{"cite": httpURL, "datetime": keep}),
		"details":    with(flags("open")),
		"img":        with(plus(flags("align", "alt", "decoding", "height", "loading", "title", "width"), map[string]attrRule{"src": imageURL, "srcset": srcset})),
		"ins":        with(map[string]attrRule{"cite": httpURL, "datetime": keep}),
		"li":         with(flags("type", "value")),
		"ol":         with(flags("reversed", "start", "type")),
		"q":          with(map[string]attrRule{"cite": httpURL}),
		"source":     plus(flags("height", "media", "sizes", "type", "width"), map[string]attrRule{"src": imageURL, "srcset": srcset}),
		"table":      with(flags("width", "border", "align", "valign")),
		"tbody":      with(section),
		"td":         with(cell),
		"tfoot":      with(section),
		"th":         with(cell),
		"thead":      with(section),
		"time":       with(flags("datetime")),
		"tr":         with(flags("rowspan", "align", "valign")),
		"video":      with(plus(flags("autoplay", "controls", "loop", "muted", "playsinline", "preload", "height", "width"), map[string]attrRule{"poster": imageURL, "src": httpURL})),
	}
	for _, tag := range []string{
		"abbr", "address", "article", "aside", "b", "bdi", "bdo", "big", "br", "caption", "center",
		"cite", "code", "dd", "div", "dl", "dt", "em", "figcaption", "figure",
		"h1", "h2", "h3", "h4", "h5", "h6", "hr", "i", "kbd", "mark", "nav", "p", "picture", "pre",
		"rp", "rt", "ruby", "s", "section", "small", "span", "sub", "summary", "sup", "strong",
		"strike", "u", "ul", "wbr",
	} {
		l[tag] = with(nil)
	}
	return l
}()

// voidElements are written self-closed, as Marp Core does in XHTML mode.
var voidElements = []string{
	"area", "base", "br", "col", "command", "embed", "hr", "img", "input", "keygen", "link",
	"meta", "param", "source", "track", "wbr",
	"circle", "ellipse", "line", "path", "polygon", "polyline", "rect", "stop", "use",
}

var textEscaper = strings.NewReplacer("<", "&lt;", ">", "&gt;")

// sanitizeHTML filters a raw HTML fragment through the policy, like Marp
// Core's js-xss pass: allowed tags are rewritten with only their allowed
// attributes, other tags are escaped as text, and comments are dropped.
func sanitizeHTML(raw string, policy HTMLPolicy) string {
	if policy == HTMLAll {
		return raw
	}
	var out strings.Builder
	z := html.NewTokenizerFragment(strings.NewReader(raw), "div")
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if z.Err() != io.EOF {
				out.WriteString(textEscaper.Replace(string(z.Raw())))
			}
			return out.String()
		}
		rawTok := string(z.Raw())
		switch tt {
		case html.TextToken:
			out.WriteString(textEscaper.Replace(rawTok))
		case html.CommentToken, html.DoctypeToken:
			// js-xss strips comments.
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			rules, ok := allowlist[tag]
			if policy == HTMLNone || !ok {
				out.WriteString(textEscaper.Replace(rawTok))
				continue
			}
			if tt == html.EndTagToken {
				out.WriteString("</" + tag + ">")
				continue
			}
			out.WriteString("<" + tag)
			for hasAttr {
				var k, v []byte
				k, v, hasAttr = z.TagAttr()
				rule, allowed := rules[string(k)]
				if !allowed {
					continue
				}
				// js-xss writes an attribute whose value is empty, or was
				// refused, bare: <a href>, <details open>.
				out.WriteString(" " + string(k))
				if val := rule(string(v)); val != "" {
					out.WriteString(`="` + escapeHTML(val) + `"`)
				}
			}
			if slices.Contains(voidElements, tag) {
				out.WriteString(" />")
			} else {
				out.WriteString(">")
			}
		}
	}
}
