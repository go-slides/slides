package deck

import (
	"bufio"
	"encoding/base64"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/go-icons/twemoji"
	"github.com/yuin/goldmark/v2/ast"
	ghtml "github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/util"
)

// Emoji is Marp Core's emoji support: shortcodes (:smile:, markdown-it-emoji's
// table) and Unicode emoji become Twemoji images,
//
//	<img class="emoji" draggable="false" alt="😄" src="…" data-marp-twemoji=""/>
//
// where Marp Core's src is a CDN URL and ours is a data: URI of the SVG, so
// that a deck needs no network. Shortcodes are recognized in text only;
// Unicode emoji in text, and in code spans and code blocks too (Marp Core
// rewrites the rendered code HTML). Raw HTML, URLs, image alternative texts and
// attribute values are left alone.
//
// The graphics come from github.com/go-icons/twemoji (CC-BY 4.0: see the
// README for the attribution).

// EmojiCSS is the rule Marp Core adds to the theme for its emoji images
// (themeSetPackOptions' before): one em square, aligned with the text.
const EmojiCSS = `img[data-marp-twemoji]{background:transparent;height:1em;margin:0 .05em 0 .1em;vertical-align:-.1em;width:1em}`

// maxShortcode bounds how far past a colon a closing colon is looked for: the
// longest markdown-it-emoji name is 40 bytes.
const maxShortcode = 64

// srcs caches the data URI of a graphic by stem.
var srcs sync.Map // string -> string

// emojiSrc is the data: URI of the graphic for seq, or "" when the set has none.
func emojiSrc(seq string) string {
	stem := twemoji.Lookup(seq)
	if stem == "" {
		return ""
	}
	if v, ok := srcs.Load(stem); ok {
		return v.(string)
	}
	svg, ok := twemoji.SVGByStem(stem)
	if !ok {
		return ""
	}
	uri := "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
	srcs.Store(stem, uri)
	return uri
}

// writeEmoji writes the image for seq, spelled alt in the text.
func writeEmoji(b *strings.Builder, alt, seq string) bool {
	src := emojiSrc(seq)
	if src == "" {
		return false
	}
	b.WriteString(`<img class="emoji" draggable="false" alt="`)
	b.WriteString(alt)
	b.WriteString(`" src="`)
	b.WriteString(src)
	b.WriteString(`" data-marp-twemoji=""/>`)
	return true
}

// emojify returns s, a run of HTML text (no tags), with its emoji replaced
// by images: Unicode emoji, and :shortcodes: when shortcodes is set.
func emojify(s string, shortcodes bool) string {
	if !needsEmoji(s, shortcodes) {
		return s
	}
	var b strings.Builder
	last := 0
	for i := 0; i < len(s); {
		switch c := s[i]; {
		case c == ':' && shortcodes:
			end := min(len(s), i+1+maxShortcode)
			if j := strings.IndexByte(s[i+1:end], ':'); j >= 0 {
				if seq, ok := twemoji.Shortcode(s[i+1 : i+1+j]); ok {
					b.WriteString(s[last:i])
					last = i
					if writeEmoji(&b, seq, seq) {
						i += j + 2
						last = i
						continue
					}
				}
			}
			i++
		case c >= utf8.RuneSelf || (keycapBase(c) && i+1 < len(s) && s[i+1] >= utf8.RuneSelf):
			if n := twemoji.Cut(s[i:]); n > 0 {
				b.WriteString(s[last:i])
				last = i
				if writeEmoji(&b, s[i:i+n], s[i:i+n]) {
					i += n
					last = i
					continue
				}
			}
			_, w := utf8.DecodeRuneInString(s[i:])
			i += w
		default:
			i++
		}
	}
	b.WriteString(s[last:])
	return b.String()
}

// keycapBase reports a byte a keycap sequence can start with.
func keycapBase(c byte) bool { return c == '#' || c == '*' || (c >= '0' && c <= '9') }

// needsEmoji is a cheap test that s may hold an emoji: a non-ASCII byte, or a
// colon when shortcodes count. Most text holds neither.
func needsEmoji(s string, shortcodes bool) bool {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= utf8.RuneSelf || (shortcodes && c == ':') {
			return true
		}
	}
	return false
}

// emojifyHTML replaces the Unicode emoji in the text of rendered HTML, outside
// its tags (Marp Core does this to code spans and code blocks).
func emojifyHTML(h string) string {
	if !needsEmoji(h, false) {
		return h
	}
	var b strings.Builder
	for len(h) > 0 {
		lt := strings.IndexByte(h, '<')
		if lt < 0 {
			b.WriteString(emojify(h, false))
			break
		}
		b.WriteString(emojify(h[:lt], false))
		gt := strings.IndexByte(h[lt:], '>')
		if gt < 0 {
			b.WriteString(h[lt:])
			break
		}
		b.WriteString(h[lt : lt+gt+1])
		h = h[lt+gt+1:]
	}
	return b.String()
}

// expandShortcodes returns s with its :shortcodes: spelled as the emoji they
// stand for, as Marp Core's slugs and outlines read them.
func expandShortcodes(s string) string {
	if !strings.Contains(s, ":") {
		return s
	}
	return twemoji.Replace(s, func(_, seq string) string { return seq })
}

// renderText is goldmark's text rendering, with emoji.
func (r *slideRenderer) renderText(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Text)
	if r.merged[n] {
		delete(r.merged, n)
		return ast.WalkContinue, nil
	}
	// goldmark cuts a text at every character a parser may care about (the
	// underscores of :heart_eyes:): look at the whole run, as markdown-it,
	// which joins them, does.
	last := n
	for !last.SoftLineBreak() && !last.HardLineBreak() {
		next, ok := last.NextSibling().(*ast.Text)
		if !ok || next.Segment.Start != last.Segment.Stop || next.IsRaw() != n.IsRaw() {
			break
		}
		if r.merged == nil {
			r.merged = map[*ast.Text]bool{}
		}
		r.merged[next] = true
		last = next
	}
	value := n.Segment.Value(source)
	if last != n {
		value = source[n.Segment.Start:last.Segment.Stop]
	}
	n = last
	switch {
	case n.IsRaw():
		ghtml.DefaultWriter.RawWrite(w, value)
	case !mayHoldEmoji(value):
		ghtml.DefaultWriter.Write(w, value)
		writeBreak(w, n)
	default:
		// Entities may spell an emoji (&#x1F604;): look at the text as it is
		// written.
		// One buffer and writer per render, reused for every text: a
		// text renderer runs no other renderer inside it.
		if r.textBW == nil {
			r.textBW = bufio.NewWriter(&r.textBuf)
		}
		r.textBuf.Reset()
		r.textBW.Reset(&r.textBuf)
		ghtml.DefaultWriter.Write(r.textBW, value)
		_ = r.textBW.Flush()
		_, _ = w.WriteString(emojify(r.textBuf.String(), true))
		writeBreak(w, n)
	}
	return ast.WalkContinue, nil
}

// mayHoldEmoji is needsEmoji for source text: besides non-ASCII bytes and
// colons, an ampersand may start an entity that decodes to an emoji.
func mayHoldEmoji(b []byte) bool {
	for _, c := range b {
		if c >= utf8.RuneSelf || c == ':' || c == '&' {
			return true
		}
	}
	return false
}

func writeBreak(w util.BufWriter, n *ast.Text) {
	switch {
	case n.HardLineBreak() || n.SoftLineBreak():
		// Marp Core's markdown-it has breaks: true, and the renderer is
		// configured with hard wraps.
		_, _ = w.WriteString("<br />\n")
	}
}

// renderViaPlain renders n with goldmark's own HTML renderer and gives the
// result to f.
func (r *slideRenderer) renderViaPlain(w util.BufWriter, source []byte, n ast.Node, f func(string) string) (ast.WalkStatus, error) {
	// goldmark's own renderer calls none of ours: one buffer and writer
	// per render serve every call.
	if r.plainBW == nil {
		r.plainBW = bufio.NewWriter(&r.plainBuf)
	}
	r.plainBuf.Reset()
	r.plainBW.Reset(&r.plainBuf)
	if err := r.plain.Renderer().Render(r.plainBW, source, n); err != nil {
		return ast.WalkStop, err
	}
	_, _ = w.WriteString(f(r.plainBuf.String()))
	return ast.WalkSkipChildren, nil
}

func (r *slideRenderer) renderCodeSpan(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	return r.renderViaPlain(w, source, n, emojifyHTML)
}

func (r *slideRenderer) renderCodeBlock(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	return r.renderViaPlain(w, source, n, emojifyHTML)
}
