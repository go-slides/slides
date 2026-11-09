package export

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/go-slides/slides/doc"
)

// The transition and the builds of an OpenDocument slide. The transition is
// the page style's smil:type, smil:subtype, smil:direction and smil:fadeColor
// with presentation:transition-speed (OpenDocument 1.3 Part 3, 20.238 and
// 20.242 to 20.245 on style:drawing-page-properties). The builds are the
// page's animation: an anim:par (presentation:node-type "timing-root") whose
// anim:seq ("main-sequence") holds one click, an anim:par that starts on
// "next", for each fragment, in which each of the fragment's shapes is an
// entrance effect that sets its visibility and fades it in (anim:set,
// anim:transitionFilter, 15.2.4 and 15.2.7; presentation:node-type on-click
// and with-previous, 19.406; presentation:preset-class entrance, 19.414), in
// the shape LibreOffice writes and reads. go-odf has no field for either, so
// they are written into the content.xml of the deck it wrote, as pptxanim.go
// does for PowerPoint.

// odpAnim is what a page gets: its transition, and its builds, one click each.
type odpAnim struct {
	transition *doc.Transition
	clicks     [][]int // indexes of the page's top-level shapes, a click each
	// fx are the animated SVG shapes' effects, which start with the page; the
	// page is pageW by pageH device pixels (svgemit.go, odpsvg.go).
	fx           []svgFx
	pageW, pageH float64
}

// odpTransitionAttrs are the attributes a page style gets for its transition.
func odpTransitionAttrs(t doc.Transition) string {
	e := EffectOf(t).ODP
	s := ` presentation:transition-speed="` + [...]string{"fast", "medium", "slow"}[speed(t)] + `" smil:type="` + e.Type + `"`
	if e.Subtype != "" {
		s += ` smil:subtype="` + e.Subtype + `"`
	}
	if e.Reverse {
		s += ` smil:direction="reverse"`
	}
	if e.Subtype == "fadeToColor" {
		// SMIL 12.8: fadeToColor is a fade to a colour, black here, the
		// colour of the screen between two slides
		s += ` smil:fadeColor="#000000"`
	}
	return s
}

// shapeID is the draw:id the i-th top-level shape of page n gets.
func shapeID(n, i int) string { return "s" + strconv.Itoa(n) + "-" + strconv.Itoa(i) }

// pageID is the draw:id of page n, which its transition's timing starts on.
func pageID(n int) string { return "p" + strconv.Itoa(n) }

// odpAnimationXML is the page's animation: the transition, with its exact
// duration, which the style's coarse speed cannot say, and the clicks'
// entrance effects.
func odpAnimationXML(n int, a odpAnim) string {
	var b strings.Builder
	b.WriteString(`<anim:par smil:dur="indefinite" smil:restart="never" presentation:node-type="timing-root">`)
	if t := a.transition; t != nil {
		e := EffectOf(*t).ODP
		b.WriteString(`<anim:par smil:begin="` + pageID(n) + `.begin"><anim:transitionFilter smil:dur="` + strconv.FormatFloat(seconds(*t), 'f', -1, 64) + `s" smil:type="` + e.Type + `"`)
		if e.Subtype != "" {
			b.WriteString(` smil:subtype="` + e.Subtype + `"`)
		}
		if e.Reverse {
			b.WriteString(` smil:direction="reverse"`)
		}
		b.WriteString(`/></anim:par>`)
	}
	if len(a.clicks) == 0 && len(a.fx) == 0 {
		b.WriteString(`</anim:par>`)
		return b.String()
	}
	b.WriteString(`<anim:seq smil:dur="indefinite" presentation:node-type="main-sequence">`)
	if len(a.fx) > 0 {
		// what starts with the page comes first in the sequence
		b.WriteString(odpFxXML(a.fx, func(i int) string { return shapeID(n, i) }, a.pageW, a.pageH))
	}
	clicks := a.clicks
	for _, click := range clicks {
		b.WriteString(`<anim:par smil:begin="next" smil:fill="hold"><anim:par smil:begin="0s" smil:fill="hold">`)
		for j, sh := range click {
			kind := "with-previous"
			if j == 0 {
				kind = "on-click"
			}
			id := shapeID(n, sh)
			b.WriteString(`<anim:par smil:begin="0s" smil:fill="hold" presentation:node-type="` + kind + `" presentation:preset-class="entrance" presentation:preset-id="ooo-entrance-fade-in">`)
			b.WriteString(`<anim:set smil:begin="0s" smil:dur="0.001s" smil:fill="hold" smil:targetElement="` + id + `" smil:attributeName="visibility" smil:to="visible"/>`)
			b.WriteString(`<anim:transitionFilter smil:dur="` + strconv.FormatFloat(fragmentFadeMS/1000.0, 'f', -1, 64) + `s" smil:targetElement="` + id + `" smil:type="fade" smil:subtype="crossfade"/>`)
			b.WriteString(`</anim:par>`)
		}
		b.WriteString(`</anim:par></anim:par>`)
	}
	b.WriteString(`</anim:seq></anim:par>`)
	return b.String()
}

// addODPAnimations writes each page's transition and animation into the
// deck's content.xml. Page n (1-based) is the n-th draw:page.
func addODPAnimations(deck []byte, anims map[int]odpAnim) ([]byte, error) {
	const part = "content.xml"
	found := false
	out, err := rewriteZip(deck, func(name string) bool { return name == part }, func(_ string, body []byte) ([]byte, error) {
		found = true
		return editODPContent(string(body), anims)
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("export: the deck has no %s", part)
	}
	return out, nil
}

// editODPContent rewrites content.xml: the pages in anims get a style of their
// own with the transition, ids on the shapes their builds act on, and the
// animation as their first child.
func editODPContent(s string, anims map[int]odpAnim) ([]byte, error) {
	pages, err := pageSpans(s)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	var styles strings.Builder // the new styles, added to the automatic styles
	last := 0
	for i, pg := range pages {
		a, ok := anims[i]
		if !ok {
			continue
		}
		n := i + 1
		b.WriteString(s[last:pg.start])
		last = pg.end
		open, body := s[pg.start:pg.bodyStart], s[pg.bodyStart:pg.end]
		if a.transition != nil {
			old, ok := attrValue(open, "draw:style-name")
			if !ok {
				return nil, fmt.Errorf("export: page %d has no style", n)
			}
			style, err := pageStyleWith(s, old, "dpa"+strconv.Itoa(n), odpTransitionAttrs(*a.transition))
			if err != nil {
				return nil, err
			}
			styles.WriteString(style)
			open = strings.Replace(open, `draw:style-name="`+old+`"`, `draw:style-name="dpa`+strconv.Itoa(n)+`"`, 1)
			// the transition's timing begins with the page
			at := len("<draw:page")
			open = open[:at] + ` xml:id="` + pageID(n) + `" draw:id="` + pageID(n) + `"` + open[at:]
		}
		if len(a.clicks) > 0 || len(a.fx) > 0 {
			// the top-level shapes get ids, from the last to keep the offsets
			tops, err := topLevel(body)
			if err != nil {
				return nil, err
			}
			for j := len(tops) - 1; j >= 0; j-- {
				at := tops[j] + 1 + len(tagName(body[tops[j]:])) // after the element's name
				// LibreOffice resolves smil:targetElement by xml:id: with only a
				// draw:id the effect is read and its shape is lost
				body = body[:at] + ` xml:id="` + shapeID(n, j) + `" draw:id="` + shapeID(n, j) + `"` + body[at:]
			}
			for _, c := range a.clicks {
				for _, sh := range c {
					if sh >= len(tops) {
						return nil, fmt.Errorf("export: page %d has no shape %d to build", n, sh)
					}
				}
			}
			for _, fx := range a.fx {
				if fx.index >= len(tops) {
					return nil, fmt.Errorf("export: page %d has no shape %d to animate", n, fx.index)
				}
			}
		}
		if a.transition != nil || len(a.clicks) > 0 || len(a.fx) > 0 {
			// After the shapes, before the notes, where LibreOffice writes it: it
			// resolves smil:targetElement while reading, so the shapes the
			// effects name must have been read
			at := strings.Index(body, "<presentation:notes")
			if at < 0 {
				at = len(body) - len("</draw:page>")
			}
			body = body[:at] + odpAnimationXML(n, a) + body[at:]
		}
		b.WriteString(open + body)
	}
	b.WriteString(s[last:])
	out := b.String()
	// go-odf declares neither prefix
	const nsAttrs = ` xmlns:smil="urn:oasis:names:tc:opendocument:xmlns:smil-compatible:1.0" xmlns:anim="urn:oasis:names:tc:opendocument:xmlns:animation:1.0"`
	root := strings.Index(out, "<office:document-content")
	if root < 0 {
		return nil, fmt.Errorf("export: content.xml has no office:document-content")
	}
	if at := root + len("<office:document-content"); !strings.Contains(out[:tagEnd(out[root:])+root], "xmlns:smil=") {
		out = out[:at] + nsAttrs + out[at:]
	}
	if styles.Len() > 0 {
		const end = `</office:automatic-styles>`
		i := strings.Index(out, end)
		if i < 0 {
			return nil, fmt.Errorf("export: content.xml has no automatic styles")
		}
		out = out[:i] + styles.String() + out[i:]
	}
	return []byte(out), nil
}

// pageStyleWith is the style named from, copied as name with attrs added to
// its drawing-page-properties.
func pageStyleWith(s, from, name, attrs string) (string, error) {
	head := `<style:style style:name="` + from + `" style:family="drawing-page">`
	i := strings.Index(s, head)
	if i < 0 {
		return "", fmt.Errorf("export: the page style %q is not in content.xml", from)
	}
	j := strings.Index(s[i:], `</style:style>`)
	if j < 0 {
		return "", fmt.Errorf("export: the page style %q does not end", from)
	}
	def := s[i : i+j+len(`</style:style>`)]
	k := strings.Index(def, `<style:drawing-page-properties`)
	if k < 0 {
		return "", fmt.Errorf("export: the page style %q has no drawing-page-properties", from)
	}
	m := strings.Index(def[k:], `/>`)
	if m < 0 {
		return "", fmt.Errorf("export: the page style %q has a properties element that is not empty", from)
	}
	def = def[:k+m] + attrs + def[k+m:]
	return strings.Replace(def, `style:name="`+from+`"`, `style:name="`+name+`"`, 1), nil
}

// attrValue is the value of attribute name in the start tag tag.
func attrValue(tag, name string) (string, bool) {
	_, rest, ok := strings.Cut(tag, " "+name+`="`)
	if !ok {
		return "", false
	}
	v, _, ok := strings.Cut(rest, `"`)
	return v, ok
}

// tagName is the name of the element a start tag at the front of s opens.
func tagName(s string) string {
	s = s[1:]
	if i := strings.IndexAny(s, " \t\r\n/>"); i >= 0 {
		return s[:i]
	}
	return s
}

// tagEnd is the index of the '>' that ends the tag at the front of s, past any
// in a quoted value.
func tagEnd(s string) int {
	var q byte
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case q != 0:
			if c == q {
				q = 0
			}
		case c == '"' || c == '\'':
			q = c
		case c == '>':
			return i
		}
	}
	return -1
}

// span is a draw:page: its start tag at start, its content from bodyStart,
// the element ending at end.
type span struct{ start, bodyStart, end int }

// pageSpans finds the draw:page elements of content.xml, in order. The
// document is what go-odf wrote: escaped text, no comments or CDATA, and
// pages that do not nest.
func pageSpans(s string) ([]span, error) {
	var out []span
	from := 0
	for {
		i := strings.Index(s[from:], "<draw:page ")
		if i < 0 {
			return out, nil
		}
		i += from
		e := tagEnd(s[i:])
		if e < 0 {
			return nil, fmt.Errorf("export: a draw:page start tag does not end")
		}
		j := strings.Index(s[i:], "</draw:page>")
		if j < 0 {
			return nil, fmt.Errorf("export: a draw:page does not end")
		}
		out = append(out, span{start: i, bodyStart: i + e + 1, end: i + j + len("</draw:page>")})
		from = i + j
	}
}

// topLevel returns the offsets, in the body of a page (its content up to and
// including its end tag), of its shapes' start tags: the elements at depth 0
// other than the page's notes and animation, and the end tag.
func topLevel(body string) ([]int, error) {
	var tops []int
	depth := 0
	for i := 0; i < len(body); {
		k := strings.IndexByte(body[i:], '<')
		if k < 0 {
			break
		}
		i += k
		e := tagEnd(body[i:])
		if e < 0 {
			return nil, fmt.Errorf("export: a tag does not end")
		}
		tag := body[i : i+e+1]
		switch {
		case strings.HasPrefix(tag, "</"):
			depth--
		default:
			name := tagName(tag)
			if depth == 0 && name != "presentation:notes" && !strings.HasPrefix(name, "anim:") {
				tops = append(tops, i)
			}
			if !strings.HasSuffix(tag, "/>") {
				depth++
			}
		}
		if depth < 0 {
			break // the page's own end tag
		}
		i += e + 1
	}
	return tops, nil
}
