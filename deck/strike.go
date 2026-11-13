package deck

import (
	"io"

	gast "github.com/yuin/goldmark/v2/ast"
	east "github.com/yuin/goldmark/v2/extension/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer"
	ghtml "github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// strikethrough is markdown-it's strikethrough, which Marp Core enables:
// exactly two tildes (goldmark's GFM extension also takes one), rendered
// as <s> rather than <del>. It is a parser extension; the renderer is
// strikeRenderer, registered by the slide renderer.
type strikethrough struct{}

func (strikethrough) ParserOptions(*parser.Config) []parser.Option {
	return []parser.Option{parser.WithInlineParsers(util.Prioritized[parser.InlineParser](strikeParser{}, 500))}
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
	if block.PrecedingCharacter() == '~' {
		return nil
	}
	line, _ := block.PeekLine()
	n := 0
	for n < len(line) && line[n] == '~' {
		n++
	}
	if n != 2 {
		return nil
	}
	return parser.ParseDelimiter(block, 1, strikeDelimiter{}, pc)
}

func (strikeParser) CloseBlock(gast.Node, parser.Context) {}

// strikeRenderer writes <s>.
func strikeRenderer(w io.Writer, _ []byte, _ gast.Node, entering bool, _ renderer.Context) (gast.WalkStatus, error) {
	if entering {
		_, _ = w.(util.BufWriter).WriteString("<s>")
	} else {
		_, _ = w.(util.BufWriter).WriteString("</s>")
	}
	return gast.WalkContinue, nil
}

var strikeNodeRenderer = ghtml.NodeRendererFunc(strikeRenderer)
