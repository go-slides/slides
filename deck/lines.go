package deck

import (
	"slices"

	"github.com/yuin/goldmark/ast"
)

// SourceLine returns the 0-based line of the deck's source file that a
// byte offset of the parsed Markdown comes from. The Markdown is the deck
// after its front matter and its templates: the front matter's lines are
// added back, and with Options.LineMap the template stage's line map
// traces a generated or included line to the source line that produced it.
func (d *Deck) SourceLine(offset int) int {
	if d.lineStarts == nil {
		// Where each line of the source starts, once: a binary search
		// then finds an offset's line, where counting the newlines
		// before it for every block would be quadratic.
		d.lineStarts = []int{0}
		for i, c := range d.source {
			if c == '\n' {
				d.lineStarts = append(d.lineStarts, i+1)
			}
		}
	}
	offset = min(offset, len(d.source))
	body, found := slices.BinarySearch(d.lineStarts, offset)
	if !found {
		body--
	}
	if d.lineMap != nil {
		if body < len(d.lineMap) {
			return d.lineMap[body]
		}
		if len(d.lineMap) > 0 {
			return d.lineMap[len(d.lineMap)-1]
		}
	}
	return d.fmLines + body
}

// nodeLine returns the source line of a block node, -1 when it has no
// position (a node the parser synthesised).
func (d *Deck) nodeLine(n ast.Node) int {
	for c := n; c != nil; c = c.FirstChild() {
		if p := c.Pos(); p >= 0 {
			return d.SourceLine(p)
		}
		if c.Type() == ast.TypeBlock && c.Lines().Len() > 0 {
			return d.SourceLine(c.Lines().At(0).Start)
		}
		if c.Type() == ast.TypeInline {
			if t, ok := c.(*ast.Text); ok {
				return d.SourceLine(t.Segment.Start)
			}
		}
	}
	return -1
}
