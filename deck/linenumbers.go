package deck

import (
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// Line numbers on fenced code blocks. Marp Core has none (marp-core 4.4.0:
// no line numbering in its sources or README), and it ignores what follows the
// language of a fence, so the options live there and a deck that uses them
// renders in Marp as it would without:
//
//	```go {lineNumbers}          numbers from 1
//	```go {lineNumbers=10}       numbers from 10
//	```go {lineNumbers=false}    none, whatever the deck says
//	```go {.numberLines startFrom="10"}    Pandoc's spelling
//
// and, for every block of a slide and the slides after it, the lineNumbers
// directive (<!-- lineNumbers: true -->, a kind can set it). The accepted
// words are the ones Slidev (lineNumbers, lines, startLine), Pandoc
// (numberLines, startFrom) and reveal.js (a number) users know.

// fenceOptions are what the braces after a fence's language say.
type fenceOptions struct {
	// numbers is nil when the block says nothing.
	numbers *bool
	// start is the number of the first line; 0 is 1.
	start int
}

// parseFenceOptions reads the options in the info string of a fence: the
// words after the language, in braces.
func parseFenceOptions(info string) fenceOptions {
	var o fenceOptions
	_, rest, _ := strings.Cut(strings.TrimSpace(info), " ")
	rest = strings.TrimSpace(rest)
	if !strings.HasPrefix(rest, "{") || !strings.HasSuffix(rest, "}") {
		return o
	}
	for _, f := range strings.FieldsFunc(rest[1:len(rest)-1], func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		key, val, _ := strings.Cut(f, "=")
		if k2, v2, ok := strings.Cut(key, ":"); ok && val == "" {
			key, val = k2, v2
		}
		key = strings.TrimPrefix(key, ".")
		val = strings.Trim(val, `"'`)
		switch key {
		case "lineNumbers", "lines", "numberLines":
			switch n, err := strconv.Atoi(val); {
			case val == "" || val == "true":
				o.numbers = boolp(true)
			case val == "false":
				o.numbers = boolp(false)
			case err == nil && n >= 1 && n <= 1_000_000:
				o.numbers, o.start = boolp(true), n
			}
		case "startLine", "startFrom":
			if n, err := strconv.Atoi(val); err == nil && n >= 1 && n <= 1_000_000 {
				o.start = n
			}
		}
	}
	return o
}

func boolp(b bool) *bool { return &b }

// lineGutter is the column of numbers for n lines from start: one number a
// line, each ended by a line feed, in an element the page hides from readers
// and the style sheet from selection.
func lineGutter(n, start int) string {
	if start < 1 {
		start = 1
	}
	var b strings.Builder
	// The width is set from the digits of the last number: go-webengine
	// sizes a flex item from its text with the forced line breaks ignored,
	// so the column came out as wide as all the digits laid side by side.
	// The fence's options and text are fixed before, so n is never 0 here
	// unless the block is empty; then there is nothing to number.
	b.WriteString(`<span class="ln-gutter" aria-hidden="true" style="width:`)
	// (0.6em a digit, the advance of the monospace fonts; ch is not
	// supported there; tenths in integers, so that no float is printed)
	tenths := 6 * len(strconv.Itoa(start+n-1))
	b.WriteString(strconv.Itoa(tenths/10) + "." + strconv.Itoa(tenths%10))
	b.WriteString(`em">`)
	for i := 0; i < n; i++ {
		b.WriteString(strconv.Itoa(start + i))
		b.WriteByte('\n')
	}
	b.WriteString("</span>")
	return b.String()
}

// codeLines counts the lines of a code block's text.
func codeLines(code string) int {
	n := strings.Count(code, "\n")
	if code != "" && !strings.HasSuffix(code, "\n") {
		n++
	}
	return n
}

// UsesLineNumbers reports whether any slide turns line numbers on, by the
// directive or in a fence: the page then carries the style for them.
func (d *Deck) UsesLineNumbers() bool {
	for _, s := range d.Slides {
		if s.Dirs.Get("lineNumbers") == "true" {
			return true
		}
		for _, top := range s.nodes {
			found := false
			_ = ast.Walk(top, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
				if f, ok := n.(*ast.FencedCodeBlock); ok && entering && f.Info != nil {
					if o := parseFenceOptions(string(f.Info.Segment.Value(d.source))); o.numbers != nil && *o.numbers {
						found = true
						return ast.WalkStop, nil
					}
				}
				return ast.WalkContinue, nil
			})
			if found {
				return true
			}
		}
	}
	return false
}

// LineNumbersCSS is the style of the numbers: a column before the code, in
// the code's own font, set apart by a rule, muted, and out of selection. It
// is added after the theme when UsesLineNumbers.
const LineNumbersCSS = `section pre.line-numbers>code{display:flex}section pre.line-numbers .ln-gutter{display:block;flex:none;box-sizing:content-box;text-align:right;padding:0 .8em 0 0;margin:0 .8em 0 0;border-right:1px solid currentcolor;opacity:.4;-webkit-user-select:none;user-select:none;pointer-events:none}section pre.line-numbers .ln-code{display:block;flex:1;min-width:0;white-space:pre}`
