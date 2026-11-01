package deck

import (
	"bytes"

	"github.com/yuin/goldmark/v2"
	gast "github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// Math is Marp Core's math syntax (its marp_math_inline and marp_math_block
// rules, from Pandoc): $…$ inline and $$…$$ as a block.
//
// Inline: the opening $ must not be followed by a space or a tab; the
// closing $ is the next unescaped one, which must not be preceded by a space
// or a tab nor followed by a digit; "$$" with nothing inside stays text.
// Block: a line starting with $$ opens it, and it ends on the line whose
// trimmed text ends with $$ (the same line, or a later one).

// MathInline is an inline formula; Tex is its source.
type MathInline struct {
	gast.BaseInline
	Tex string
}

// KindMathInline is the node kind of MathInline.
var KindMathInline = gast.NewNodeKind("MathInline")

// Kind implements ast.Node.
func (n *MathInline) Kind() gast.NodeKind { return KindMathInline }

// Dump implements ast.Node.
func (n *MathInline) Dump(source []byte, level int) {
	gast.DumpHelper(n, source, level, map[string]string{"Tex": n.Tex}, nil)
}

// MathBlock is a display formula; Tex is its source.
type MathBlock struct {
	gast.BaseBlock
	Tex string
}

// KindMathBlock is the node kind of MathBlock.
var KindMathBlock = gast.NewNodeKind("MathBlock")

// Kind implements ast.Node.
func (n *MathBlock) Kind() gast.NodeKind { return KindMathBlock }

// IsRaw implements ast.Node.
func (n *MathBlock) IsRaw() bool { return true }

// Dump implements ast.Node.
func (n *MathBlock) Dump(source []byte, level int) {
	gast.DumpHelper(n, source, level, map[string]string{"Tex": n.Tex}, nil)
}

type mathExtension struct{}

func (mathExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithInlineParsers(util.Prioritized(mathInlineParser{}, 150)),
		parser.WithBlockParsers(util.Prioritized(mathBlockParser{}, 150)),
	)
}

type mathInlineParser struct{}

func (mathInlineParser) Trigger() []byte { return []byte{'$'} }

func (mathInlineParser) Parse(parent gast.Node, block text.Reader, pc parser.Context) gast.Node {
	line, seg := block.PeekLine()
	if len(line) < 2 || line[0] != '$' {
		return nil
	}
	// Openable: the next character is not a space or a tab.
	if line[1] == ' ' || line[1] == '\t' {
		return nil
	}
	// The closing $ is the next one preceded by an even number of
	// backslashes.
	end := -1
	for i := 1; i < len(line); i++ {
		if line[i] != '$' {
			continue
		}
		bs := 0
		for j := i - 1; j > 0 && line[j] == '\\'; j-- {
			bs++
		}
		if bs%2 == 0 {
			end = i
			break
		}
	}
	if end < 0 || end == 1 {
		// No closing $, or "$$": text (Marp leaves "$$" for the text).
		return nil
	}
	// Closable: not preceded by a space or a tab, not followed by a digit.
	if prev := line[end-1]; prev == ' ' || prev == '\t' {
		return nil
	}
	if end+1 < len(line) && line[end+1] >= '0' && line[end+1] <= '9' {
		return nil
	}
	n := &MathInline{Tex: string(line[1:end])}
	n.SetPos(seg.Start)
	block.Advance(end + 1)
	return n
}

type mathBlockParser struct{}

func (mathBlockParser) Trigger() []byte { return []byte{'$'} }

func (mathBlockParser) Open(parent gast.Node, reader text.Reader, pc parser.Context) (gast.Node, parser.State) {
	line, seg := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 || !bytes.HasPrefix(line[pos:], []byte("$$")) {
		return nil, parser.NoChildren
	}
	rest := line[pos+2:]
	n := &MathBlock{}
	n.SetPos(seg.Start + pos)
	trimmed := bytes.TrimSpace(rest)
	if bytes.HasSuffix(trimmed, []byte("$$")) {
		// $$ … $$ on one line.
		n.Tex = string(bytes.TrimSpace(trimmed[:len(trimmed)-2]))
		reader.Advance(len(line) - 1)
		pc.Set(mathDoneKey, true)
		return n, parser.Close
	}
	if len(trimmed) > 0 {
		n.Tex = string(trimmed) + "\n"
	}
	reader.Advance(len(line) - 1)
	pc.Set(mathDoneKey, false)
	return n, parser.NoChildren
}

var mathDoneKey = parser.NewContextKey()

func (mathBlockParser) Continue(node gast.Node, reader text.Reader, pc parser.Context) parser.State {
	if done, _ := pc.Get(mathDoneKey).(bool); done {
		return parser.Close
	}
	line, _ := reader.PeekLine()
	if line == nil {
		return parser.Close
	}
	n := node.(*MathBlock)
	trimmed := bytes.TrimSpace(line)
	if bytes.HasSuffix(trimmed, []byte("$$")) {
		// The closing line: what precedes its last $$ belongs to the
		// formula.
		i := bytes.LastIndex(line, []byte("$$"))
		if before := bytes.TrimSpace(line[:i]); len(before) > 0 {
			n.Tex += string(before)
		}
		reader.Advance(len(line) - 1)
		pc.Set(mathDoneKey, true)
		return parser.Close
	}
	n.Tex += string(line)
	reader.Advance(len(line) - 1)
	// NoChildren alone would close the block: goldmark keeps a block open
	// only while Continue carries the Continue bit.
	return parser.Continue | parser.NoChildren
}

func (mathBlockParser) Close(node gast.Node, reader text.Reader, pc parser.Context) {
	pc.Set(mathDoneKey, nil)
}

func (mathBlockParser) CanInterruptParagraph() bool { return true }
func (mathBlockParser) CanAcceptIndentedLine() bool { return false }
