package deck

import (
	"github.com/yuin/goldmark"
	gast "github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// strikethrough is markdown-it's strikethrough, which Marp Core enables:
// exactly two tildes (goldmark's GFM extension also takes one), rendered
// as <s> rather than <del>.
type strikethrough struct{}

func (strikethrough) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(util.Prioritized(strikeParser{}, 500)))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(strikeRenderer{}, 500)))
}

type strikeDelimiter struct{}

func (strikeDelimiter) IsDelimiter(b byte) bool { return b == '~' }
func (strikeDelimiter) CanOpenCloser(opener, closer *parser.Delimiter) bool {
	return opener.Char == closer.Char
}
func (strikeDelimiter) OnMatch(int) gast.Node { return east.NewStrikethrough() }

type strikeParser struct{}

func (strikeParser) Trigger() []byte { return []byte{'~'} }

func (strikeParser) Parse(parent gast.Node, block text.Reader, pc parser.Context) gast.Node {
	before := block.PrecendingCharacter()
	line, segment := block.PeekLine()
	node := parser.ScanDelimiter(line, before, 1, strikeDelimiter{})
	if node == nil || node.OriginalLength != 2 || before == '~' {
		return nil
	}
	node.Segment = segment.WithStop(segment.Start + node.OriginalLength)
	block.Advance(node.OriginalLength)
	pc.PushDelimiter(node)
	return node
}

func (strikeParser) CloseBlock(gast.Node, parser.Context) {}

type strikeRenderer struct{}

func (strikeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(east.KindStrikethrough, func(w util.BufWriter, _ []byte, _ gast.Node, entering bool) (gast.WalkStatus, error) {
		if entering {
			_, _ = w.WriteString("<s>")
		} else {
			_, _ = w.WriteString("</s>")
		}
		return gast.WalkContinue, nil
	})
}
