package deck

import (
	"slices"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/text"
)

// SourceLine returns the 0-based line of the deck's source file that a
// byte offset of the parsed Markdown comes from. The Markdown is the deck
// after its front matter and its templates: the front matter's lines are
// added back, and with Options.LineMap the template stage's line map
// traces a generated or included line to the source line that produced it.
func (d *Deck) SourceLine(offset int) int {
	return d.sourceLineOf(d.lineIndex(offset))
}

// lineIndex returns the 0-based line of the parsed Markdown holding offset.
func (d *Deck) lineIndex(offset int) int {
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
	return body
}

// sourceLineOf returns the source file line of a line of the parsed Markdown.
func (d *Deck) sourceLineOf(body int) int {
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

// setAttr sets a node attribute to a literal string (goldmark v2 keeps
// attribute values as text values, not as bytes).
func setAttr(n ast.Node, name, value string) {
	n.SetAttribute(name, text.NewMultiLineValueFromString(value, text.IdentityDecoder))
}

// fencedBlock returns n as a fenced code block: goldmark v2 has one
// CodeBlock node for the indented and the fenced kinds.
func fencedBlock(n ast.Node) (*ast.CodeBlock, bool) {
	c, ok := n.(*ast.CodeBlock)
	return c, ok && c.CodeBlockKind == ast.CodeBlockKindFenced
}

// blockSegments returns the source segments a block node holds: the lines
// goldmark v1 called Lines(). A paragraph or heading keeps them in Source();
// a code or HTML block keeps its content in Value.
func blockSegments(n ast.Node) []text.Segment {
	switch x := n.(type) {
	case *ast.CodeBlock:
		return x.Value.Segments()
	case *ast.HTMLBlock:
		return x.Value.Segments()
	case ast.BlockNode:
		return x.Source()
	}
	return nil
}

// nodeLine returns the source line of a block node, -1 when it has no
// position (a node the parser synthesised).
func (d *Deck) nodeLine(n ast.Node) int {
	for c := n; c != nil; c = c.FirstChild() {
		if p := c.Pos(); p >= 0 {
			return d.SourceLine(p)
		}
		if segs := blockSegments(c); len(segs) > 0 {
			return d.SourceLine(segs[0].Start)
		}
		if t, ok := c.(*ast.Text); ok && !t.Value.IsOwned() {
			return d.SourceLine(t.Value.Index().Start)
		}
	}
	return -1
}
