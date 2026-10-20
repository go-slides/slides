package deck

import (
	"bufio"
	"bytes"
	"fmt"
	"html"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer"
	ghtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/go-slides/slides/interp"
)

// Frame is how a slide is wrapped.
type Frame int

const (
	// FrameSVG wraps each slide in <svg><foreignObject>, Marp Core's
	// inline SVG mode: the slide scales with the viewport, and split
	// backgrounds are separate layers.
	FrameSVG Frame = iota
	// FramePlain writes bare <section> elements (Marpit without inline
	// SVG): split and multiple backgrounds collapse to the last image.
	FramePlain
	// FrameBox renders the inline SVG layers as absolutely positioned
	// <div>s, for layout engines without <foreignObject>.
	FrameBox
)

// RenderOptions configures Render.
type RenderOptions struct {
	Frame Frame
	HTML  HTMLPolicy
	// Lang is the lang attribute when no lang directive sets one.
	Lang string
	// Width and Height are the slide size in pixels, from the theme.
	Width, Height int
	// Highlight, when set, renders a fenced code block's content; ok false
	// falls back to plain escaped code.
	Highlight func(code, lang string) (htmlOut string, ok bool)
	// GeneratedContent, when set, writes elements that stand for the
	// ::after pseudo-element Marpit paginates with, for layout engines
	// without generated content: <span data-slides-pseudo="after">.
	GeneratedContent bool
	// FitHeadings writes the markup FitScript scales for headings marked
	// <!-- fit -->. Marpit without inline SVG does not fit headings.
	FitHeadings bool
	// Math, when set, typesets a formula ($…$ inline, $$…$$ display) to
	// HTML; nil leaves formulas as the text they were written as.
	Math func(tex string, display bool) (string, error)
	// AfterContent is the theme's section::after content expression (see
	// theme.Packed.After); the element writes its value. Empty means the
	// page number alone, Marpit's scaffold.
	AfterContent string
	// SourceLines marks the slide frames and the blocks in them with the
	// source line they come from, the way VS Code's Markdown preview syncs
	// its scrolling with the editor: class="code-line" data-line="N"
	// (0-based; see Slide.Line and Deck.SourceLine). Marp for VS Code
	// marks the same elements: the frame, not the <section>, whose class
	// themes select on.
	SourceLines bool
}

// Rendered is a rendered slide.
type Rendered struct {
	Slide *Slide
	// HTML is the slide's markup, frame included.
	HTML string
}

// Render renders every slide.
func (d *Deck) Render(opts RenderOptions) []Rendered {
	if opts.Width == 0 {
		opts.Width, opts.Height = 1280, 720
	}
	r := &slideRenderer{deck: d, opts: opts}
	r.plain = goldmark.New(goldmark.WithRendererOptions(ghtml.WithHardWraps(), ghtml.WithXHTML(), ghtml.WithUnsafe()))
	r.md = goldmark.New(
		goldmark.WithExtensions(extension.NewTable(extension.WithTableCellAlignMethod(extension.TableCellAlignStyle))),
		goldmark.WithRendererOptions(
			ghtml.WithHardWraps(),
			ghtml.WithXHTML(),
			ghtml.WithUnsafe(),
			renderer.WithNodeRenderers(util.Prioritized(r, 100), util.Prioritized(strikeRenderer{}, 100)),
		),
	)
	out := make([]Rendered, len(d.Slides))
	for i, s := range d.Slides {
		out[i] = Rendered{Slide: s, HTML: r.slide(s)}
	}
	return out
}

type slideRenderer struct {
	deck *Deck
	opts RenderOptions
	md   goldmark.Markdown
	// plain is goldmark's own HTML rendering, for the nodes whose output
	// is rewritten after the fact (code spans and blocks).
	plain goldmark.Markdown
	// merged holds the texts of a run already written with the first.
	merged map[*ast.Text]bool
	// slideLine is the source line of the slide being written, for its
	// frame (SourceLines).
	slideLine int
	// nodeBuf and nodeBW receive each top-level node of a slide (see slide).
	nodeBuf bytes.Buffer
	nodeBW  *bufio.Writer
	// textBuf and textBW escape a text that may hold an emoji (renderText).
	textBuf bytes.Buffer
	textBW  *bufio.Writer
	// plainBuf and plainBW receive goldmark's own rendering (renderViaPlain).
	plainBuf bytes.Buffer
	plainBW  *bufio.Writer
}

// lineAttrs returns a block's source-line attributes when SourceLines
// asks for them, "" otherwise.
func (r *slideRenderer) lineAttrs(n ast.Node) string {
	if !r.opts.SourceLines {
		return ""
	}
	l := r.deck.nodeLine(n)
	if l < 0 {
		return ""
	}
	return ` class="code-line" data-line="` + strconv.Itoa(l) + `"`
}

// markDefaultBlocks gives the blocks goldmark writes itself (lists, block
// quotes, tables) their source-line attributes, and returns a function
// that takes them off again: the tree belongs to the deck, which may be
// rendered again without them.
func (r *slideRenderer) markDefaultBlocks(n ast.Node) func() {
	if !r.opts.SourceLines {
		return func() {}
	}
	type saved struct {
		n     ast.Node
		attrs []ast.Attribute
	}
	var undo []saved
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || c.Type() != ast.TypeBlock {
			return ast.WalkContinue, nil
		}
		switch c.Kind() {
		case ast.KindList, ast.KindBlockquote, extast.KindTable:
		default:
			return ast.WalkContinue, nil
		}
		l := r.deck.nodeLine(c)
		if l < 0 {
			return ast.WalkContinue, nil
		}
		undo = append(undo, saved{c, slices.Clone(c.Attributes())})
		c.SetAttributeString("class", []byte("code-line"))
		c.SetAttributeString("data-line", []byte(strconv.Itoa(l)))
		return ast.WalkContinue, nil
	})
	return func() {
		for _, s := range undo {
			s.n.RemoveAttributes()
			for _, a := range s.attrs {
				s.n.SetAttribute(a.Name, a.Value)
			}
		}
	}
}

// marpitDirectives are the directives Marpit writes out as data-*
// attributes and custom properties: its own and Marp Core's custom ones.
var marpitDirectives = append([]string{"headingDivider", "style", "theme", "lang", "size", "math"},
	slices.DeleteFunc(slices.Clone(localNames), func(n string) bool { return n == "transition" })...) // Marp CLI's, written out as JSON (transition.go)

var camelRe = regexp.MustCompile(`[A-Z]`)

// escapeHTML is markdown-it's escapeHtml.
var escapeHTML = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace

func kebab(s string) string {
	return camelRe.ReplaceAllStringFunc(s, func(c string) string { return "-" + strings.ToLower(c) })
}

// attr is one attribute; order matters for byte-comparable output.
type attr struct{ k, v string }

type attrs []attr

func (a *attrs) set(k, v string) {
	for i := range *a {
		if (*a)[i].k == k {
			(*a)[i].v = v
			return
		}
	}
	*a = append(*a, attr{k, v})
}

func (a attrs) get(k string) string {
	for _, x := range a {
		if x.k == k {
			return x.v
		}
	}
	return ""
}

func (a attrs) without(k string) attrs {
	var o attrs
	for _, x := range a {
		if x.k != k {
			o = append(o, x)
		}
	}
	return o
}

func (a attrs) String() string {
	var b strings.Builder
	for _, x := range a {
		b.WriteString(" " + x.k + `="` + escapeHTML(x.v) + `"`)
	}
	return b.String()
}

// styleDecls is Marpit's InlineStyle: one value per property, in the
// order properties were first set.
type styleDecls struct {
	keys []string
	vals map[string]string
}

func (s *styleDecls) set(k, v string) *styleDecls {
	if s.vals == nil {
		s.vals = map[string]string{}
	}
	if _, ok := s.vals[k]; !ok {
		s.keys = append(s.keys, k)
	}
	s.vals[k] = v
	return s
}

func (s *styleDecls) String() string {
	var b strings.Builder
	for _, k := range s.keys {
		v := s.vals[k]
		// postcss refuses a declaration that would break out of it.
		if breaksOut(v) {
			continue
		}
		b.WriteString(k + ":" + v + ";")
	}
	return b.String()
}

// breaksOut reports a ";", "{" or "}" outside quotes and parentheses: in
// url("data:image/png;base64,…") the semicolon is part of the value.
func breaksOut(v string) bool {
	depth := 0
	var quote byte
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case depth == 0 && (c == ';' || c == '{' || c == '}'):
			return true
		}
	}
	return false
}

// sectionAttrs builds a slide's <section> attributes the way Marpit's
// marpit_directives_apply does.
func (r *slideRenderer) sectionAttrs(s *Slide, dirs Dirs) (attrs, *styleDecls) {
	var a attrs
	a.set("id", strconv.Itoa(s.Index+1))
	if len(s.ScopedStyles) > 0 {
		a.set(scopeAttr(s), "")
	}
	if s.Fragments > 0 {
		a.set("data-marpit-fragments", strconv.Itoa(s.Fragments))
	}
	style := &styleDecls{}
	all := dirs.Clone()
	all.Merge(r.deck.Global.Dirs)
	for _, k := range all.Keys() {
		v := all.Get(k)
		if v == "" || v == "false" || !contains(marpitDirectives, k) {
			continue
		}
		a.set("data-"+kebab(k), v)
		style.set("--"+kebab(k), v)
	}
	if s.Kind != "" {
		a.set("data-kind", s.Kind)
	}
	if lang := or(all.Get("lang"), r.opts.Lang); lang != "" {
		a.set("lang", lang)
	}
	if c := all.Get("class"); c != "" {
		a.set("class", c)
	}
	if c := all.Get("color"); c != "" {
		style.set("color", c)
	}
	if c := all.Get("backgroundColor"); c != "" {
		style.set("background-color", c).set("background-image", "none")
	}
	if bg := all.Get("backgroundImage"); bg != "" {
		style.set("background-image", bg).set("background-position", "center").
			set("background-repeat", "no-repeat").set("background-size", "cover")
		if v := all.Get("backgroundPosition"); v != "" {
			style.set("background-position", v)
		}
		if v := all.Get("backgroundRepeat"); v != "" {
			style.set("background-repeat", v)
		}
		if v := all.Get("backgroundSize"); v != "" {
			style.set("background-size", v)
		}
	}
	if s.Paginated {
		a.set("data-marpit-pagination", strconv.Itoa(s.Page))
	}
	return a, style
}

// setTransition writes the transition attributes: the transition of the slide
// before, to play backwards, then the slide's own. Marp CLI's
// marp_cli_transition sets them after everything Marpit does to the section,
// so they come last, and the layers of an advanced background copy them.
func (r *slideRenderer) setTransition(a *attrs, s *Slide) {
	if s.Index > 0 {
		if prev, ok := transitionAttr(r.deck.Slides[s.Index-1]); ok {
			a.set("data-transition-back", prev)
		}
	}
	if cur, ok := transitionAttr(s); ok {
		a.set("data-transition", cur)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// scopeAttr is the attribute scoped styles select on. Marpit draws a random
// key; ours is derived from the slide so the output is reproducible.
func scopeAttr(s *Slide) string {
	return fmt.Sprintf("data-marpit-scope-s%d", s.Index+1)
}

// ScopeAttr returns the attribute a slide's scoped styles select on.
func ScopeAttr(s *Slide) string { return scopeAttr(s) }

// needsLayers reports whether a slide's backgrounds need Marpit's layers:
// several images, a split, or a filter (which only layers apply).
func needsLayers(s *Slide) bool {
	if len(s.Backgrounds) > 1 || s.BackgroundSplit != "" {
		return true
	}
	return len(s.Backgrounds) == 1 && s.Backgrounds[0].Filter != ""
}

func (r *slideRenderer) slide(s *Slide) string {
	dirs := s.Dirs.Clone()
	advanced := r.opts.Frame != FramePlain && len(s.Backgrounds) > 0
	if r.opts.Frame == FrameBox && !needsLayers(s) {
		// One unfiltered, unsplit background looks the same as a CSS
		// background, which asks less of the layout engine.
		advanced = false
	}
	if !advanced && len(s.Backgrounds) > 0 {
		// Without layers, the last background image becomes the CSS
		// background (Marpit's background_image/apply.js).
		img := s.Backgrounds[len(s.Backgrounds)-1]
		dirs.Set("backgroundImage", `url("`+img.URL+`")`)
		if img.Size != "" {
			dirs.Set("backgroundSize", img.Size)
		}
	}
	a, style := r.sectionAttrs(s, dirs)

	var content strings.Builder
	content.WriteString("\n")
	if h := dirs.Get("header"); h != "" {
		content.WriteString("<header>" + r.inline(h) + "</header>\n")
	}
	r.slideLine = s.Line
	// One buffer and one buffered writer for every top-level node of every
	// slide: goldmark wraps a plain io.Writer in a new 4 KiB bufio.Writer on
	// each call. (Nested renders, from inside a node renderer, keep their own.)
	if r.nodeBW == nil {
		r.nodeBW = bufio.NewWriter(&r.nodeBuf)
	}
	for _, n := range s.nodes {
		r.nodeBuf.Reset()
		r.nodeBW.Reset(&r.nodeBuf)
		unmark := r.markDefaultBlocks(n)
		if err := r.md.Renderer().Render(r.nodeBW, r.deck.source, n); err == nil {
			content.Write(r.nodeBuf.Bytes())
		}
		unmark()
	}
	if f := dirs.Get("footer"); f != "" {
		content.WriteString("<footer>" + r.inline(f) + "</footer>\n")
	}
	if r.opts.GeneratedContent && s.Paginated {
		content.WriteString(r.pseudoAfter(s) + "\n")
	}

	var out string
	if !advanced {
		if s.Paginated {
			// Marpit sets the total after every other attribute.
			a.set("style", style.String())
			a.set("data-marpit-pagination-total", strconv.Itoa(s.Total))
		} else if st := style.String(); st != "" {
			a.set("style", st)
		}
		if a.get("style") == "" {
			a = a.without("style")
		}
		r.setTransition(&a, s)
		section := "<section" + a.String() + ">" + content.String() + "</section>"
		if r.opts.Frame == FramePlain {
			out = section + "\n"
		} else {
			out = r.frame(section, nil, nil)
		}
	} else {
		out = r.advanced(s, a, style, dirs, content.String())
	}
	return interp.Resolve(out, s.Page, s.Total)
}

// advanced writes Marpit's advanced backgrounds: a background layer, the
// content layer, and a layer for the pseudo-elements, each a full section.
func (r *slideRenderer) advanced(s *Slide, a attrs, style *styleDecls, dirs Dirs, content string) string {
	w := strconv.Itoa(r.opts.Width)
	a.set("data-marpit-advanced-background", "content")
	contentW, contentX := w, ""
	if s.BackgroundSplit != "" {
		a.set("data-marpit-advanced-background-split", s.BackgroundSplit)
		size := or(s.BackgroundSplitSize, "50%")
		pct, _ := strconv.ParseFloat(strings.TrimSuffix(size, "%"), 64)
		contentW = strconv.FormatFloat(100-pct, 'f', -1, 64) + "%"
		if s.BackgroundSplit == "left" {
			contentX = size
		}
		style.set("--marpit-advanced-background-split", size)
	}
	// Marpit writes the content section's style before copying its
	// attributes to the other layers.
	contentAttrs := a
	if s.Paginated {
		contentAttrs.set("style", style.String())
		contentAttrs.set("data-marpit-pagination-total", strconv.Itoa(s.Total))
	} else if st := style.String(); st != "" {
		contentAttrs.set("style", st)
	}
	r.setTransition(&contentAttrs, s)

	bgAttrs := contentAttrs.without("id")
	bgAttrs.set("data-marpit-advanced-background", "background")
	var figures strings.Builder
	for _, img := range s.Backgrounds {
		fs := &styleDecls{}
		fs.set("background-image", `url("`+img.URL+`")`)
		if img.Size != "" {
			fs.set("background-size", img.Size)
		}
		if img.Filter != "" {
			fs.set("filter", img.Filter)
		}
		figures.WriteString(`<figure style="` + escapeHTML(fs.String()) + `">`)
		if img.Alt != "" {
			figures.WriteString("<figcaption>" + escapeHTML(img.Alt) + "</figcaption>")
		}
		figures.WriteString("</figure>")
	}
	direction := or(s.BackgroundDirection, "horizontal")
	bg := "<section" + bgAttrs.String() + `><div data-marpit-advanced-background-container="true" data-marpit-advanced-background-direction="` +
		direction + `">` + figures.String() + "</div></section>"

	pseudoAttrs := contentAttrs.without("id")
	ps := &styleDecls{}
	if c := dirs.Get("color"); c != "" {
		ps.set("color", c)
	}
	pseudoAttrs.set("style", ps.String())
	// Marpit writes the pseudo layer's style even when it is empty.
	pseudoAttrs.set("data-marpit-advanced-background", "pseudo")
	pseudoContent := ""
	if r.opts.GeneratedContent && s.Paginated {
		pseudoContent = r.pseudoAfter(s)
		// The content layer must not number the slide a second time.
		content = strings.Replace(content, pseudoContent+"\n", "", 1)
	}
	pseudo := "<section" + pseudoAttrs.String() + ">" + pseudoContent + "</section>"

	section := "<section" + contentAttrs.String() + ">" + content + "</section>"
	return r.frame(section, &layer{html: bg, width: w}, &layer{html: pseudo, width: w, pseudo: true}, layer{html: section, width: contentW, x: contentX})
}

type layer struct {
	html   string
	width  string
	x      string
	pseudo bool
}

// frame wraps the content section, and the background and pseudo layers
// when there are any, in the slide frame.
func (r *slideRenderer) frame(section string, bg, pseudo *layer, content ...layer) string {
	w, h := strconv.Itoa(r.opts.Width), strconv.Itoa(r.opts.Height)
	c := layer{html: section, width: w}
	if len(content) > 0 {
		c = content[0]
	}
	var layers []layer
	if bg != nil {
		layers = append(layers, *bg)
	}
	layers = append(layers, c)
	if pseudo != nil {
		layers = append(layers, *pseudo)
	}
	var b strings.Builder
	class, line := "", ""
	if r.opts.SourceLines {
		class, line = " code-line", ` data-line="`+strconv.Itoa(r.slideLine)+`"`
	}
	if r.opts.Frame == FrameBox {
		b.WriteString(`<div class="marpit-svg` + class + `" data-marpit-svg=""` + line + ` style="position:relative;width:` + w + `px;height:` + h + `px;overflow:hidden">`)
		for _, l := range layers {
			left := or(l.x, "0")
			width := l.width
			if !strings.HasSuffix(width, "%") {
				width += "px"
			}
			extra := ""
			if l.pseudo {
				extra = ` data-marpit-advanced-background="pseudo"`
			}
			b.WriteString(`<div class="marpit-fo"` + extra + ` style="position:absolute;top:0;left:` + left + `;width:` + width + `;height:` + h + `px">` + l.html + "</div>")
		}
		b.WriteString("</div>")
		return b.String()
	}
	if class != "" {
		b.WriteString(`<svg data-marpit-svg="" viewBox="0 0 ` + w + " " + h + `" class="` + class[1:] + `"` + line + `>`)
	} else {
		b.WriteString(`<svg data-marpit-svg="" viewBox="0 0 ` + w + " " + h + `">`)
	}
	for _, l := range layers {
		b.WriteString(`<foreignObject width="` + l.width + `" height="` + h + `"`)
		if l.x != "" {
			b.WriteString(` x="` + l.x + `"`)
		}
		if l.pseudo {
			b.WriteString(` data-marpit-advanced-background="pseudo"`)
		}
		b.WriteString(">" + l.html + "</foreignObject>")
	}
	b.WriteString("</svg>")
	return b.String()
}

// inline renders a header or footer: Markdown inline syntax only.
func (r *slideRenderer) inline(src string) string {
	source := []byte(src)
	doc := markdown().Parser().Parse(text.NewReader(source))
	var b bytes.Buffer
	for p := doc.FirstChild(); p != nil; p = p.NextSibling() {
		for c := p.FirstChild(); c != nil; c = c.NextSibling() {
			_ = r.md.Renderer().Render(&b, source, c)
		}
	}
	return strings.TrimSpace(b.String())
}

// RegisterFuncs overrides goldmark's HTML for what Marp changes.
func (r *slideRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindImage, r.renderImage)
	reg.Register(ast.KindHeading, r.renderHeading)
	reg.Register(ast.KindListItem, r.renderListItem)
	reg.Register(ast.KindParagraph, r.renderParagraph)
	reg.Register(ast.KindHTMLBlock, r.renderHTMLBlock)
	reg.Register(ast.KindRawHTML, r.renderRawHTML)
	reg.Register(ast.KindFencedCodeBlock, r.renderFencedCode)
	reg.Register(KindMathInline, r.renderMath)
	reg.Register(KindMathBlock, r.renderMath)
	if !r.deck.opts.NoEmoji {
		reg.Register(ast.KindText, r.renderText)
		reg.Register(ast.KindCodeSpan, r.renderCodeSpan)
		reg.Register(ast.KindCodeBlock, r.renderCodeBlock)
	}
}

// renderMath writes a formula through RenderOptions.Math. Without it, or
// when the formula does not typeset, the source stays visible as written,
// and the error is kept in a title for whoever inspects the slide.
func (r *slideRenderer) renderMath(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkSkipChildren, nil
	}
	tex, display := "", false
	switch n := node.(type) {
	case *MathInline:
		tex = n.Tex
	case *MathBlock:
		tex, display = n.Tex, true
	}
	src := "$" + tex + "$"
	if display {
		src = "$$\n" + strings.TrimSuffix(tex, "\n") + "\n$$"
	}
	if display {
		_, _ = w.WriteString("<p" + r.lineAttrs(node) + ">")
	}
	if r.opts.Math == nil {
		_, _ = w.WriteString(escapeHTML(src))
	} else if out, err := r.opts.Math(tex, display); err != nil {
		_, _ = w.WriteString(`<span class="math-error" title="` + escapeHTML(err.Error()) + `">` + escapeHTML(src) + "</span>")
	} else {
		_, _ = w.WriteString(out)
	}
	if display {
		_, _ = w.WriteString("</p>\n")
	}
	return ast.WalkSkipChildren, nil
}

func (r *slideRenderer) info(n ast.Node) *nodeInfo {
	if i := r.deck.info[n]; i != nil {
		return i
	}
	return &nodeInfo{}
}

func (r *slideRenderer) renderImage(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Image)
	i := r.info(n)
	if i.hidden {
		return ast.WalkSkipChildren, nil
	}
	_, _ = w.WriteString(`<img src="`)
	_, _ = w.Write(util.EscapeHTML(util.URLEscape(n.Destination, true)))
	_, _ = w.WriteString(`" alt="`)
	if i.image != nil && i.image.anyConsumed() {
		_, _ = w.WriteString(escapeHTML(i.image.alt()))
	} else {
		_, _ = w.Write(util.EscapeHTML(plainText(n, source)))
	}
	_ = w.WriteByte('"')
	if n.Title != nil {
		_, _ = w.WriteString(` title="`)
		_, _ = w.Write(util.EscapeHTML(n.Title))
		_ = w.WriteByte('"')
	}
	if i.image != nil {
		if st := i.image.inlineStyle(); st != "" {
			_, _ = w.WriteString(` style="` + escapeHTML(st) + `"`)
		}
	}
	_, _ = w.WriteString(" />")
	return ast.WalkSkipChildren, nil
}

// plainText is an image's alternative text with the markup taken out.
func plainText(n ast.Node, source []byte) []byte {
	var b bytes.Buffer
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch x := c.(type) {
		case *ast.Text:
			b.Write(x.Segment.Value(source))
			if x.SoftLineBreak() {
				b.WriteByte('\n')
			}
		case *ast.String:
			b.Write(x.Value)
		}
		return ast.WalkContinue, nil
	})
	return b.Bytes()
}

func (r *slideRenderer) renderHeading(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Heading)
	tag := "h" + strconv.Itoa(n.Level)
	i := r.info(n)
	fit := i.fit && r.opts.FitHeadings
	if !entering {
		if fit {
			_, _ = w.WriteString("</span>")
		}
		_, _ = w.WriteString("</" + tag + ">\n")
		return ast.WalkContinue, nil
	}
	_, _ = w.WriteString("<" + tag)
	if fit {
		// One line, written in the markup rather than set by FitScript:
		// go-webengine honours white-space on a block, not yet on an
		// inline span, and does not yet lay out again before a script
		// measures what it just changed.
		_, _ = w.WriteString(` data-auto-scaling="" style="white-space:nowrap"`)
	}
	if i.id != "" {
		_, _ = w.WriteString(` id="` + escapeHTML(i.id) + `"`)
	}
	_, _ = w.WriteString(r.lineAttrs(n))
	_ = w.WriteByte('>')
	if fit {
		// The line FitScript measures and scales.
		_, _ = w.WriteString(`<span data-fit-content="" style="white-space:nowrap">`)
	}
	return ast.WalkContinue, nil
}

// FitScript scales each fitting heading (# <!-- fit --> Title) so its one
// line fills the heading's width, up or down, as Marp Core's auto-scaling
// element does with an SVG viewBox: the font size is multiplied by the
// ratio of the available width to the line's natural width.
const FitScript = `(()=>{for(const c of document.querySelectorAll('[data-auto-scaling]>[data-fit-content]')){` +
	`const h=c.parentElement,s=getComputedStyle(h),` +
	`avail=h.clientWidth-parseFloat(s.paddingLeft||0)-parseFloat(s.paddingRight||0),` +
	// offsetWidth is the layout width: getBoundingClientRect would include
	// the scaling of an inline SVG frame.
	`natural=c.offsetWidth;` +
	`if(natural>0&&avail>0)h.style.fontSize=(parseFloat(s.fontSize)*avail/natural)+'px'}})();`

func (r *slideRenderer) renderListItem(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if entering {
		_, _ = w.WriteString("<li")
		if f := r.info(n).fragment; f > 0 {
			_, _ = w.WriteString(` data-marpit-fragment="` + strconv.Itoa(f) + `"`)
		}
		_, _ = w.WriteString(r.lineAttrs(n))
		_ = w.WriteByte('>')
		fc := n.FirstChild()
		if fc != nil {
			if _, ok := fc.(*ast.TextBlock); !ok {
				_ = w.WriteByte('\n')
			}
		}
	} else {
		_, _ = w.WriteString("</li>\n")
	}
	return ast.WalkContinue, nil
}

// renderParagraph hides a paragraph left with nothing visible once its
// comments and background images are taken out (Marpit's sweep).
func (r *slideRenderer) renderParagraph(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if r.sweepable(n, source) {
		return ast.WalkSkipChildren, nil
	}
	if entering {
		_, _ = w.WriteString("<p" + r.lineAttrs(n) + ">")
	} else {
		_, _ = w.WriteString("</p>\n")
	}
	return ast.WalkContinue, nil
}

func (r *slideRenderer) sweepable(n ast.Node, source []byte) bool {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if r.info(c).hidden {
			continue
		}
		t, ok := c.(*ast.Text)
		if !ok || strings.TrimSpace(string(t.Segment.Value(source))) != "" {
			return false
		}
	}
	return true
}

func (r *slideRenderer) renderHTMLBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering || r.info(node).hidden {
		return ast.WalkSkipChildren, nil
	}
	raw := blockText(node.(*ast.HTMLBlock), source)
	if strings.TrimSpace(raw) == "" {
		return ast.WalkSkipChildren, nil
	}
	_, _ = w.WriteString(sanitizeHTML(raw, r.opts.HTML))
	return ast.WalkSkipChildren, nil
}

func (r *slideRenderer) renderRawHTML(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering || r.info(node).hidden {
		return ast.WalkSkipChildren, nil
	}
	n := node.(*ast.RawHTML)
	_, _ = w.WriteString(sanitizeHTML(string(n.Segments.Value(source)), r.opts.HTML))
	return ast.WalkSkipChildren, nil
}

func (r *slideRenderer) renderFencedCode(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkSkipChildren, nil
	}
	n := node.(*ast.FencedCodeBlock)
	var code bytes.Buffer
	for i := 0; i < n.Lines().Len(); i++ {
		seg := n.Lines().At(i)
		code.Write(seg.Value(source))
	}
	lang := ""
	if n.Info != nil {
		info := string(n.Info.Segment.Value(source))
		lang, _, _ = strings.Cut(strings.TrimSpace(info), " ")
		lang = html.UnescapeString(lang)
	}
	var out strings.Builder
	// VS Code's preview reads a fenced block's line on its <code>.
	out.WriteString("<pre><code")
	la := r.lineAttrs(n)
	switch {
	case lang != "" && la != "":
		out.WriteString(` class="language-` + escapeHTML(lang) + ` code-line"` + la[len(` class="code-line"`):])
	case lang != "":
		out.WriteString(` class="language-` + escapeHTML(lang) + `"`)
	default:
		out.WriteString(la)
	}
	out.WriteByte('>')
	highlighted := false
	if r.opts.Highlight != nil {
		if h, ok := r.opts.Highlight(code.String(), lang); ok {
			out.WriteString(h)
			highlighted = true
		}
	}
	if !highlighted {
		out.Write(util.EscapeHTML(code.Bytes()))
	}
	out.WriteString("</code></pre>\n")
	if r.deck.opts.NoEmoji {
		_, _ = w.WriteString(out.String())
	} else {
		_, _ = w.WriteString(emojifyHTML(out.String()))
	}
	return ast.WalkSkipChildren, nil
}
