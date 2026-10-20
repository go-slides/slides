package deck

import (
	"github.com/yuin/goldmark/ast"
)

// SourceLine returns the 0-based line of the deck's source file that a
// byte offset of the parsed Markdown comes from. The Markdown is the deck
// after its front matter and its templates: the front matter's lines are
// added back, and with Options.LineMap the template stage's line map
// traces a generated or included line to the source line that produced it.
func (d *Deck) SourceLine(offset int) int {
	body := lineOf(d.source, offset) - 1
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
