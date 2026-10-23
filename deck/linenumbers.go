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

// withLineNumbers writes each of the first n lines' number at the start of
// the line, in an element the page hides from readers and the style sheet
// from selection.
//
// The number is in the line's own line box, not in a column beside the code:
// whatever makes one line taller (a callout disc, an emoji, an inline image, a
// fallback font), the number stays on its line's baseline, by construction,
// in a browser as in go-webengine. A separate column only lines up while every
// line has the same height. The numbers are right-aligned by padding them with
// spaces to the widest one's digits, in the code's monospace font.
func withLineNumbers(markup string, n, start int) string {
	if start < 1 {
		start = 1
	}
	width := len(strconv.Itoa(start + n - 1))
	var b strings.Builder
	b.Grow(len(markup) + n*(48+width))
	for i := 0; i < n; i++ {
		num := strconv.Itoa(start + i)
		b.WriteString(`<span class="ln" aria-hidden="true">`)
		b.WriteString(strings.Repeat(" ", width-len(num)))
		b.WriteString(num)
		b.WriteString(`</span>`)
		nl := strings.IndexByte(markup, '\n')
		if nl < 0 {
			b.WriteString(markup)
			return b.String()
		}
		b.WriteString(markup[:nl+1])
		markup = markup[nl+1:]
	}
	b.WriteString(markup)
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

// lineNumberInk is, per built-in theme, how the numbers' colour is mixed:
// the code's colour and its box's background, the theme's own variables (so
// light, dark and the theme's classes follow), and the code's share of the
// mix. The shares keep the numbers at 4.5:1 or more against the box where
// the code itself has 4.5:1 (the contrast test reads the themes' values).
var lineNumberInk = map[string]struct {
	fg, bg string
	pct    int
}{
	"default": {"var(--fgColor-default)", "var(--bgColor-muted)", 65},
	"gaia":    {"var(--color-background)", "var(--color-foreground)", 90},
	"uncover": {"var(--color-foreground)", "var(--color-background-code)", 65},
}

// LineNumbersCSS is the style of the numbers for a theme: at the start of
// each line, in the code's own font, set apart by a rule, muted, and out of
// selection. It is added after the theme when UsesLineNumbers.
//
// Muted by a mixed colour, never by opacity: a translucent element is drawn
// as a picture in the PDF, the PowerPoint and the OpenDocument file, where
// the numbers must stay text. A theme go-slides does not know keeps the
// code's colour.
func LineNumbersCSS(theme string) string {
	// Inline, not inline-block: an inline box has no baseline of its own to
	// get wrong (go-webengine v0.13.0 sets an inline-block's text about half
	// a leading above the line's), and adds nothing to the line's height.
	// The block padding draws the rule across the leading between lines
	// without making them taller (an inline box's padding is not in its
	// line's height, CSS 2.1 §10.8.1).
	css := `section pre.line-numbers .ln{padding:.2em .8em .2em 0;margin-right:.8em;border-right:1px solid;font-style:normal;font-weight:normal;text-decoration:none;-webkit-user-select:none;user-select:none;pointer-events:none}`
	if ink, ok := lineNumberInk[theme]; ok {
		mix := `color-mix(in srgb,` + ink.fg + ` ` + strconv.Itoa(ink.pct) + `%,` + ink.bg + `)`
		css += `section pre.line-numbers .ln{color:` + mix + `;border-right:1px solid ` + mix + `}`
	}
	return css
}
