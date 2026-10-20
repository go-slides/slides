package deck

import (
	"fmt"
	"slices"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// applyGlobals collects the global directives: the front matter's first,
// then every comment's in document order, the last value winning
// (Marpit's marpit_directives_global_parse).
func (d *Deck) applyGlobals(fm map[string]any, comments []*comment) error {
	apply := func(m map[string]any) bool {
		recognized := false
		for _, k := range orderedKeys(m) {
			if !isGlobal(k) {
				continue
			}
			recognized = true
			v, ok := normalizeGlobal(k, m[k])
			if !ok {
				continue
			}
			if k == "headingDivider" && v != "false" && parseHeadingDivider(v) == nil {
				continue // an invalid level leaves the previous value
			}
			if k == "url" && v != "" && !ValidURL(v) {
				d.Warnings = append(d.Warnings, fmt.Sprintf("specified canonical URL is ignored since invalid URL: %s", v))
				continue // marp-cli ignores it and keeps the previous one
			}
			d.Global.Dirs.Set(k, v)
		}
		return recognized
	}
	if fm != nil {
		apply(fm)
	}
	for _, c := range comments {
		if c.dirs != nil && apply(c.dirs) {
			c.parsed = true
		}
	}
	d.Global.HeadingDivider = parseHeadingDivider(d.Global.Dirs.Get("headingDivider"))
	if style := d.Global.Dirs.Get("style"); style != "" {
		d.Styles = append(d.Styles, style)
	}
	return nil
}

// split cuts the document's top-level blocks into slides at every thematic
// break, and before the headings the headingDivider directive names
// (Marpit's slide.js and heading_divider.js). A heading starts a slide only
// once something visible precedes it anywhere in the deck, a thematic break
// included, which is why a break followed by a divided heading yields an
// empty slide in Marpit too.
func (d *Deck) split(doc ast.Node) {
	cur := &Slide{}
	d.Slides = []*Slide{cur}
	visible := false
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		switch {
		case n.Kind() == ast.KindThematicBreak:
			cur = &Slide{start: lineEnd(d.source, n.Pos())}
			d.Slides = append(d.Slides, cur)
			visible = true
			continue
		case n.Kind() == ast.KindHeading && visible &&
			slices.Contains(d.Global.HeadingDivider, n.(*ast.Heading).Level):
			cur = &Slide{start: lineStart(d.source, n.Pos())}
			d.Slides = append(d.Slides, cur)
		}
		if !d.hiddenBlock(n) {
			visible = true
		}
		cur.nodes = append(cur.nodes, n)
	}
	for i, s := range d.Slides {
		s.Index = i
		s.Line = d.SourceLine(s.start)
	}
}

func lineEnd(source []byte, pos int) int {
	for pos < len(source) && source[pos] != '\n' {
		pos++
	}
	return min(pos+1, len(source))
}

func lineStart(source []byte, pos int) int {
	for pos > 0 && source[pos-1] != '\n' {
		pos--
	}
	return pos
}

// hiddenBlock reports whether a top-level block renders nothing in Marpit's
// token stream at the time headings divide slides: comments and styles.
func (d *Deck) hiddenBlock(n ast.Node) bool {
	if _, _, ok := commentText(n, d.source); ok {
		return true
	}
	if _, _, ok := isStyleBlock(n, d.source); ok {
		return true
	}
	return false
}

// localState is Marpit's directive cursor plus the kind bookkeeping.
type localState struct {
	local, spot Dirs
	// base holds the front matter's local directives: what a kind's
	// directives fall back to when another kind replaces it.
	base Dirs
	// kind is the sticky kind and kindKeys what it set; spotKind is the
	// current slide's spot kind.
	kind, spotKind string
	kindKeys       []string
}

// applyLocals assigns local and spot directives, and kinds, to each slide
// (Marpit's marpit_directives_parse).
func (d *Deck) applyLocals(fm map[string]any, comments []*comment, fmLines int) error {
	st := &localState{}
	if fm != nil {
		// The front matter's explicit local directives are the base the
		// kinds sit on.
		for _, k := range orderedKeys(fm) {
			if isLocal(k) {
				if v, ok := normalizeLocal(k, fm[k]); ok {
					st.base.Set(k, v)
				}
			}
		}
		if _, err := d.applyObject(st, fm); err != nil {
			return d.errorf(1, "front matter: %v", err)
		}
	}
	byNode := map[ast.Node]*comment{}
	for _, c := range comments {
		byNode[c.node] = c
	}
	for _, s := range d.Slides {
		for _, n := range s.nodes {
			err := ast.Walk(n, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
				c, ok := byNode[n]
				if !entering || !ok {
					return ast.WalkContinue, nil
				}
				if c.dirs != nil {
					recognized, err := d.applyObject(st, c.dirs)
					if err != nil {
						return ast.WalkStop, d.errorf(d.SourceLine(c.offset)+1, "%v", err)
					}
					if recognized {
						c.parsed = true
					}
				}
				if !c.parsed && !isFitComment(c) {
					s.Notes = append(s.Notes, c.content)
				}
				return ast.WalkSkipChildren, nil
			})
			if err != nil {
				return err
			}
		}
		s.Dirs = st.local.Clone()
		s.Dirs.Merge(st.spot)
		s.Kind = st.kind
		if st.spotKind != "" {
			s.Kind = st.spotKind
		}
		st.spot, st.spotKind = Dirs{}, ""
	}
	return nil
}

// isFitComment reports a Marp Core fitting-header marker, which is
// neither a directive nor a note.
func isFitComment(c *comment) bool {
	if c.content != "fit" {
		return false
	}
	for p := c.node.Parent(); p != nil; p = p.Parent() {
		if p.Kind() == ast.KindHeading {
			return true
		}
	}
	return false
}

// applyObject applies one directive mapping (a front matter or a comment).
// Kinds go first so that the mapping's own directives override them,
// whatever order they were written in.
func (d *Deck) applyObject(st *localState, m map[string]any) (bool, error) {
	recognized := false
	keys := orderedKeys(m)
	for _, k := range keys {
		if k != KindDirective && k != "_"+KindDirective {
			continue
		}
		recognized = true
		name, ok := m[k].(string)
		if !ok || name == "" {
			return true, fmt.Errorf("%s wants a kind name", k)
		}
		dirs, err := d.Kinds.Resolve(name)
		if err != nil {
			return true, err
		}
		if k == KindDirective {
			st.applyKind(name, dirs)
		} else {
			st.applySpotKind(name, dirs)
		}
	}
	for _, k := range keys {
		if isGlobal(k) {
			recognized = true
		}
		if isLocal(k) {
			recognized = true
			if v, ok := normalizeLocal(k, m[k]); ok {
				st.local.Set(k, v)
				// Written by hand, the value is the slide's own: the
				// next kind switch leaves it alone.
				st.kindKeys = slices.DeleteFunc(st.kindKeys, func(x string) bool { return x == k })
			}
		}
		if spot, isSpot := strings.CutPrefix(k, "_"); isSpot && isLocal(spot) {
			recognized = true
			if v, ok := normalizeLocal(spot, m[k]); ok {
				st.spot.Set(spot, v)
			}
		}
	}
	return recognized, nil
}

// applyKind switches the sticky kind: what the previous kind set goes back
// to the base, then the new kind's directives apply.
func (st *localState) applyKind(name string, dirs Dirs) {
	for _, k := range st.kindKeys {
		if st.base.Has(k) {
			st.local.Set(k, st.base.Get(k))
		} else {
			st.local.Delete(k)
		}
	}
	st.local.Merge(dirs)
	st.kind, st.kindKeys = name, dirs.Keys()
}

// applySpotKind gives the current slide a kind: what the sticky kind set
// and this one does not is reset to the base for this slide only.
func (st *localState) applySpotKind(name string, dirs Dirs) {
	for _, k := range st.kindKeys {
		if dirs.Has(k) {
			continue
		}
		switch {
		case st.base.Has(k):
			st.spot.Set(k, st.base.Get(k))
		case k == "paginate":
			st.spot.Set(k, "false")
		default:
			st.spot.Set(k, "")
		}
	}
	st.spot.Merge(dirs)
	st.spotKind = name
}

// paginate numbers the slides (Marpit's marpit_directives_apply): every
// slide advances the page number except those whose paginate directive is
// skip or hold; the number shows when paginate is true or hold.
func (d *Deck) paginate() {
	page := 0
	for _, s := range d.Slides {
		p := s.Paginate()
		if p != "skip" && p != "hold" {
			page++
		}
		s.Page = max(page, 1)
		s.Paginated = p == "true" || p == "hold"
	}
	for _, s := range d.Slides {
		s.Total = max(page, 1)
	}
}
