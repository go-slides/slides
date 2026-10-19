package theme

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/go-slides/slides/internal/css"
)

// scaffoldCSS is Marpit's scaffold theme (theme/scaffold.js), the base every
// packed theme starts from.
const scaffoldCSS = `
section {
  width: 1280px;
  height: 720px;
  box-sizing: border-box;
  overflow: hidden;
  position: relative;
  scroll-snap-align: center center;
  -webkit-text-size-adjust: 100%;
  text-size-adjust: 100%;
}
section::after {
  bottom: 0;
  content: attr(data-marpit-pagination);
  padding: inherit;
  pointer-events: none;
  position: absolute;
  right: 0;
}
section:not([data-marpit-pagination])::after {
  display: none;
}
/* Normalization */
:where(h1) {
  font-size: 2em;
  margin-block: 0.67em;
}
video::-webkit-media-controls {
  will-change: transform;
}
`

var scaffold = func() *Theme {
	t, err := parse(scaffoldCSS)
	if err != nil {
		panic(err)
	}
	return t
}()

// advancedBackgroundCSS is Marpit's postcss/advanced_background.js.
const advancedBackgroundCSS = `
section[data-marpit-advanced-background="background"] {
  columns: initial !important;
  display: block !important;
  padding: 0 !important;
}
section[data-marpit-advanced-background="background"]::before,
section[data-marpit-advanced-background="background"]::after,
section[data-marpit-advanced-background="content"]::before,
section[data-marpit-advanced-background="content"]::after {
  display: none !important;
}
section[data-marpit-advanced-background="background"] > div[data-marpit-advanced-background-container] {
  all: initial;
  display: flex;
  flex-direction: row;
  height: 100%;
  overflow: hidden;
  width: 100%;
}
section[data-marpit-advanced-background="background"] > div[data-marpit-advanced-background-container][data-marpit-advanced-background-direction="vertical"] {
  flex-direction: column;
}
section[data-marpit-advanced-background="background"][data-marpit-advanced-background-split] > div[data-marpit-advanced-background-container] {
  width: var(--marpit-advanced-background-split, 50%);
}
section[data-marpit-advanced-background="background"][data-marpit-advanced-background-split="right"] > div[data-marpit-advanced-background-container] {
  margin-left: calc(100% - var(--marpit-advanced-background-split, 50%));
}
section[data-marpit-advanced-background="background"] > div[data-marpit-advanced-background-container] > figure {
  all: initial;
  background-position: center;
  background-repeat: no-repeat;
  background-size: cover;
  flex: auto;
  margin: 0;
}
section[data-marpit-advanced-background="background"] > div[data-marpit-advanced-background-container] > figure > figcaption {
  position: absolute;
  border: 0;
  clip: rect(0, 0, 0, 0);
  height: 1px;
  margin: -1px;
  overflow: hidden;
  padding: 0;
  white-space: nowrap;
  width: 1px;
}
section[data-marpit-advanced-background="content"],
section[data-marpit-advanced-background="pseudo"] {
  background: transparent !important;
}
section[data-marpit-advanced-background="pseudo"],
:marpit-container > svg[data-marpit-svg] > foreignObject[data-marpit-advanced-background="pseudo"] {
  pointer-events: none !important;
}
section[data-marpit-advanced-background-split] {
  width: 100%;
  height: 100%;
}
`

const printContainerCSS = `
html, body {
  background-color: #fff;
  margin: 0;
  page-break-inside: avoid;
  break-inside: avoid-page;
}
`

// PackOptions configures Pack.
type PackOptions struct {
	// Before is CSS put ahead of the theme (Marpit's pack option of that
	// name): what the renderer needs whatever the theme, such as the rule
	// for emoji images.
	Before []string
	// After is CSS appended to the theme: the deck's style directive and
	// global <style> elements, then its scoped styles (see Scope).
	After []string
	// Container is the selector of the element holding the slides;
	// "" means "div.marpit".
	Container string
	// Slide lists the elements from the container down to the section;
	// nil means just "section".
	Slide []string
	// InlineSVG adds the advanced background rules.
	InlineSVG bool
	// Printable adds @page and the print rules.
	Printable bool
	// ContainerQuery makes the slide a size container (Marp Core).
	ContainerQuery bool
	// Size names a @size preset of the theme (the size directive).
	Size string
	// GeneratedContent lowers section::after to an element selector, for
	// layout engines that do not generate content (see Packed.After).
	GeneratedContent bool
}

// Packed is a packed style sheet.
type Packed struct {
	CSS string
	// Width and Height are the slide size in CSS pixels.
	Width, Height float64
	// After is the content expression of the theme's section::after rule
	// when GeneratedContent lowered it, for the renderer to evaluate.
	After string
}

// Pack builds the style sheet of a deck using theme t (Marpit's
// ThemeSet.pack, with Marp Core's size directive).
func (s *Set) Pack(t *Theme, opts PackOptions) Packed {
	if t == nil {
		t = s.Resolve("")
	}
	if opts.Container == "" {
		opts.Container = "div.marpit"
	}
	if opts.Slide == nil {
		opts.Slide = []string{"section"}
	}
	w, h := s.Size(t)
	themeCSS := t.CSS
	if opts.Size != "" {
		if pw, ph, ok := s.SizePreset(t, opts.Size); ok {
			w, h = pw, ph
			themeCSS += "\nsection{width:" + pw + ";height:" + ph + ";}"
		}
	}

	sheet := css.Parse(themeCSS)
	for i := len(opts.Before) - 1; i >= 0; i-- {
		sheet.Nodes = append(css.Parse(opts.Before[i]).Nodes, sheet.Nodes...)
	}
	for _, extra := range opts.After {
		x := css.Parse(extra)
		x.Nodes = css.Flatten(x.Nodes)
		sheet.Nodes = append(sheet.Nodes, s.suppressThemeImports(x.Nodes)...)
	}
	if opts.ContainerQuery {
		sheet.Nodes = append(css.Parse("section:marpit-container-query { container-type: size; }").Nodes, sheet.Nodes...)
	}
	sheet.Nodes = hoist(sheet.Nodes)
	sheet.Nodes = s.replaceImports(sheet.Nodes, []string{t.Name})
	if opts.Printable {
		print := css.Parse(`@page { size: ` + w + ` ` + h + `; margin: 0; }
@media marpit-print {
  section { page-break-before: always; break-before: page; }
  section, section * {
    -webkit-print-color-adjust: exact !important;
    animation-delay: 0s !important;
    animation-duration: 0s !important;
    color-adjust: exact !important;
    print-color-adjust: exact !important;
    transition: none !important;
  }
  :marpit-container > svg[data-marpit-svg] { display: block; height: 100vh; width: 100vw; }
}`)
		sheet.Nodes = append(print.Nodes, sheet.Nodes...)
	}
	if t != scaffold {
		sheet.Nodes = append(css.Parse(scaffold.CSS).Nodes, sheet.Nodes...)
	}
	if opts.InlineSVG {
		sheet.Nodes = append(sheet.Nodes, css.Parse(advancedBackgroundCSS).Nodes...)
	}
	paginationOnly(sheet.Nodes)
	eachRule(sheet.Nodes, func(r *css.Rule) {
		r.Selector = mapSelectors(r.Selector, func(sel string) string {
			return rootReplaceRe.ReplaceAllString(sel, "${1}section"+rootPseudo)
		})
	})
	sheet.Nodes = injectRootFontSize(sheet.Nodes)
	prepend(sheet.Nodes)
	slide := strings.Join(opts.Slide, " > ")
	eachRule(sheet.Nodes, func(r *css.Rule) {
		r.Selector = mapSelectors(r.Selector, func(sel string) string {
			sel = replacePseudo(sel, ":marpit-container", opts.Container)
			sel = replacePseudo(sel, ":marpit-slide", slide)
			return strings.TrimLeft(strings.TrimPrefix(strings.TrimSpace(sel), ">"), " ")
		})
		r.Selector = rootSpecificity.ReplaceAllString(r.Selector, `:where(section):not([\20 root])`)
		r.Selector = containerQueryPseudo.ReplaceAllString(r.Selector, ":where(section)")
	})
	if opts.Printable {
		css.Walk(sheet.Nodes, func(n, _ css.Node) bool {
			if a, ok := n.(*css.AtRule); ok && a.Name == "media" && a.Params == "marpit-print" {
				a.Params = "print"
				a.Nodes = append(css.Parse(printContainerCSS).Nodes, a.Nodes...)
			}
			return true
		})
	}
	remToRoot(sheet.Nodes)
	sheet.Nodes = hoist(sheet.Nodes)

	p := Packed{Width: pixels(w), Height: pixels(h)}
	if opts.GeneratedContent {
		p.After = lowerAfter(sheet.Nodes, opts.Container+" > "+slide)
	}
	p.CSS = sheet.String()
	return p
}

var (
	rootSpecificity      = regexp.MustCompile(`(?:section)?:marpit-root\b`)
	containerQueryPseudo = regexp.MustCompile(`\bsection:marpit-container-query\b`)
)

// replacePseudo replaces a pseudo-class name not followed by a name
// character: JavaScript's /:name(?![\w-])/g, which Go's regexp cannot
// express with a lookahead.
func replacePseudo(sel, name, repl string) string {
	var b strings.Builder
	for {
		i := strings.Index(sel, name)
		if i < 0 {
			b.WriteString(sel)
			return b.String()
		}
		end := i + len(name)
		if end < len(sel) && isNameChar(sel[end]) {
			b.WriteString(sel[:end])
		} else {
			b.WriteString(sel[:i] + repl)
		}
		sel = sel[end:]
	}
}

func isNameChar(c byte) bool {
	return c == '-' || c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// eachRule calls fn for every style rule outside @keyframes.
func eachRule(nodes []css.Node, fn func(*css.Rule)) {
	css.Walk(nodes, func(n, _ css.Node) bool {
		switch x := n.(type) {
		case *css.AtRule:
			return !strings.HasSuffix(x.Name, "keyframes")
		case *css.Rule:
			fn(x)
		}
		return true
	})
}

// hoist moves @charset and @import to the top (Marpit's import/hoisting.js).
func hoist(nodes []css.Node) []css.Node {
	var charset *css.AtRule
	var imports, rest []css.Node
	for _, n := range nodes {
		if a, ok := n.(*css.AtRule); ok && !a.Block {
			switch a.Name {
			case "charset":
				if charset == nil {
					charset = a
				}
				continue
			case "import":
				imports = append(imports, a)
				continue
			}
		}
		rest = append(rest, n)
	}
	var out []css.Node
	if charset != nil {
		out = append(out, charset)
	}
	return append(append(out, imports...), rest...)
}

// replaceImports inlines @import and @import-theme of known themes
// (Marpit's import/replace.js): @import in place, @import-theme at the top.
func (s *Set) replaceImports(nodes []css.Node, seen []string) []css.Node {
	var out, prepends []css.Node
	for _, n := range nodes {
		a, ok := n.(*css.AtRule)
		if !ok || a.Block || a.Name != "import" && a.Name != "import-theme" {
			out = append(out, n)
			continue
		}
		name, quotedOK := quoted(a.Params)
		imp := s.themes[name]
		if !quotedOK || imp == nil || containsString(seen, name) {
			out = append(out, n)
			continue
		}
		inner := s.replaceImports(css.Parse(imp.CSS).Nodes, append(seen, name))
		if a.Name == "import" {
			out = append(out, inner...)
		} else {
			prepends = append(inner, prepends...)
		}
	}
	return append(prepends, out...)
}

// suppressThemeImports comments out theme imports in a deck's own CSS
// (Marpit's import/suppress.js): a deck picks its theme with the theme
// directive, not from a style element.
func (s *Set) suppressThemeImports(nodes []css.Node) []css.Node {
	for i, n := range nodes {
		if a, ok := n.(*css.AtRule); ok && !a.Block && (a.Name == "import" || a.Name == "import-theme") {
			if name, ok := quoted(a.Params); ok && s.Has(name) {
				nodes[i] = &css.Comment{Text: " @" + a.Name + " " + a.Params + "; "}
			}
		}
	}
	return nodes
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// afterSelector is Marpit's /^section(?![\w-])[^\s>+~]*::?after$/.
var afterSelector = regexp.MustCompile(`^section(?:[^\w\s>+~-][^\s>+~]*)?::?after$`)

// paginationOnly comments out the content of section::after rules unless it
// shows the page number: Marpit reserves the slide's ::after for pagination
// (postcss/pagination.js).
func paginationOnly(nodes []css.Node) {
	eachRule(nodes, func(r *css.Rule) {
		match := false
		for _, sel := range css.SplitSelectors(r.Selector) {
			if afterSelector.MatchString(attrBrackets.ReplaceAllString(sel, "")) {
				match = true
			}
		}
		if !match {
			return
		}
		for i, n := range r.Nodes {
			if d, ok := n.(*css.Decl); ok && d.Prop == "content" && !strings.Contains(d.Value, "attr(data-marpit-pagination)") {
				r.Nodes[i] = &css.Comment{Text: " content: " + d.Value + "; "}
			}
		}
	})
}

var (
	attrBrackets   = regexp.MustCompile(`\[.*?\]`)
	selectorSplit  = regexp.MustCompile(`(\s+|\s*[>~+]\s*)`)
	targetDelim    = regexp.MustCompile(`[.:#\[]`)
	remSkip        = regexp.MustCompile(`("[^"]*"|'[^']*'|(?:attr|url|var)\([^)]*\))`)
	remValue       = regexp.MustCompile(`(\d*\.?\d+)rem\b`)
	rootFontSizeCP = "--marpit-root-font-size"
)

// injectRootFontSize mirrors every font-size given to the slide into a
// custom property, which rem units are rewritten against (Marpit's
// root/font_size.js), so that rem means the slide's font size.
func injectRootFontSize(nodes []css.Node) []css.Node {
	var out []css.Node
	for _, n := range nodes {
		switch x := n.(type) {
		case *css.AtRule:
			if x.Block && !strings.HasSuffix(x.Name, "keyframes") {
				x.Nodes = injectRootFontSize(x.Nodes)
			}
			out = append(out, x)
		case *css.Rule:
			out = append(out, x)
			var sels []string
			for _, sel := range css.SplitSelectors(x.Selector) {
				if inj, ok := rootFontSizeSelector(sel); ok {
					sels = append(sels, inj)
				}
			}
			if len(sels) == 0 {
				continue
			}
			var decls []css.Node
			for _, d := range x.Nodes {
				if decl, ok := d.(*css.Decl); ok && decl.Prop == "font-size" {
					c := *decl
					c.Prop = rootFontSizeCP
					decls = append(decls, &c)
				}
			}
			if len(decls) > 0 {
				out = append(out, &css.Rule{Selector: css.JoinSelectors(dedupe(sels)), Nodes: decls})
			}
		default:
			out = append(out, n)
		}
	}
	return out
}

func rootFontSizeSelector(sel string) (string, bool) {
	parts := splitKeep(sel, selectorSplit)
	target := parts[len(parts)-1]
	parents := strings.Join(parts[:len(parts)-1], "")
	base, rest := target, ""
	if loc := targetDelim.FindStringIndex(target); loc != nil {
		base, rest = target[:loc[0]], target[loc[0]:]
	}
	if base != "section" && !strings.HasSuffix(base, "*") && base != "" {
		return "", false
	}
	if base == "section" {
		return parents + "section" + rest, true
	}
	return parents + ":marpit-container > :marpit-slide section" + rest, true
}

// splitKeep splits s at re, keeping the separators, as JavaScript's split
// with a capturing group does.
func splitKeep(s string, re *regexp.Regexp) []string {
	var out []string
	last := 0
	for _, loc := range re.FindAllStringIndex(s, -1) {
		out = append(out, s[last:loc[0]], s[loc[0]:loc[1]])
		last = loc[1]
	}
	return append(out, s[last:])
}

func dedupe(list []string) []string {
	var out []string
	for _, s := range list {
		if !containsString(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// prepend scopes every rule to the slide (Marpit's
// pseudo_selector/prepend.js).
func prepend(nodes []css.Node) {
	eachRule(nodes, func(r *css.Rule) {
		r.Selector = mapSelectors(r.Selector, func(sel string) string {
			switch {
			case sectionStart.MatchString(sel):
				return ":marpit-container > :marpit-slide" + sel[len("section"):]
			case strings.HasPrefix(sel, ":marpit-container"):
				return sel
			}
			return ":marpit-container > :marpit-slide " + sel
		})
	})
}

// sectionStart is /^section(?![\w-])/.
var sectionStart = regexp.MustCompile(`^section(?:[^\w-]|$)`)

// remToRoot rewrites rem lengths against the slide's font size (Marpit's
// root/rem.js).
func remToRoot(nodes []css.Node) {
	css.Walk(nodes, func(n, _ css.Node) bool {
		d, ok := n.(*css.Decl)
		if !ok {
			return true
		}
		parts := splitKeep(d.Value, remSkip)
		for i := 0; i < len(parts); i += 2 {
			parts[i] = remValue.ReplaceAllString(parts[i], "calc(var("+rootFontSizeCP+", 1rem) * $1)")
		}
		d.Value = strings.Join(parts, "")
		return true
	})
}

// lowerAfter rewrites the slide's ::after rules to select the element the
// renderer writes in its place, and returns the content expression of the
// last plain section::after rule.
func lowerAfter(nodes []css.Node, slide string) string {
	content := ""
	eachRule(nodes, func(r *css.Rule) {
		changed := false
		r.Selector = mapSelectors(r.Selector, func(sel string) string {
			for _, suffix := range []string{"::after", ":after"} {
				if strings.HasSuffix(sel, suffix) && strings.HasPrefix(sel, slide) {
					head := strings.TrimSuffix(sel, suffix)
					// Only the slide's own pseudo-element, not a
					// descendant's.
					if !strings.ContainsAny(head[len(slide):], " >+~") {
						changed = true
						return head + ` > [data-slides-pseudo="after"]`
					}
				}
			}
			return sel
		})
		if !changed {
			return
		}
		kept := r.Nodes[:0]
		for _, n := range r.Nodes {
			if d, ok := n.(*css.Decl); ok && d.Prop == "content" {
				if r.Selector == slide+` > [data-slides-pseudo="after"]` {
					content = d.Value
				}
				continue
			}
			kept = append(kept, n)
		}
		r.Nodes = kept
	})
	return content
}

// pixels converts an absolute CSS length to CSS pixels (Marpit's theme.js).
func pixels(v string) float64 {
	m := lengthRe.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return 0
	}
	n, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0
	}
	switch strings.ToLower(m[2]) {
	case "px":
		return n
	case "cm":
		return n * 960 / 25.4
	case "mm":
		return n * 96 / 25.4
	case "in":
		return n * 96
	case "pc":
		return n * 16
	case "pt":
		return n * 4 / 3
	case "q":
		return n * 24 / 25.4
	}
	return 0
}

var lengthRe = regexp.MustCompile(`^(-?[.0-9]+)([a-zA-Z]+)$`)
