package deck

import (
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// Code callouts: a numbered disc at the end of a code line, referring to the
// note of the same number in the ordered list right after the block. The
// syntax is Quarto's (code-annotation.qmd) and Asciidoctor's comment form
// (verbatim/callouts.adoc): the marker sits in a comment of the code's own
// language, so the code stays valid where it is copied from the Markdown, and
// the notes are a plain Markdown ordered list:
//
//	```go
//	x := load() // <1>
//	return x    // <2>
//	```
//
//	1. Reads the file.
//	2. Hands it back.
//
// Line comments recognised before the marker: //, #, --, ;;, ;, %, ' — and
// the wrapped forms <!--1-->, <!-- <1> -->, /* <1> */. Several markers may
// end one line (<1> <2>), and a number may mark several lines. A marker that
// follows a comment's own text (x = 1 // the count <1>) keeps the text.
//
// The markers become discs only when the block is followed directly by an
// ordered list starting at 1 with an item for every number used: otherwise
// the block is left as written. Marp Core has no callouts (marp-core 4.4.0:
// no callout or annotation module among its sources), so in Marp a deck that
// uses them shows the comments and a numbered list, which still reads.

// lineCallouts are the callout numbers that end one line of a code block.
type lineCallouts struct {
	line int // 0-based, in the block
	nums []int
}

// codeCallouts is what a code block's markers say: the code without them,
// and the numbers by line.
type codeCallouts struct {
	code  string
	lines []lineCallouts
	max   int
}

// maxCallout bounds the numbers recognised; a larger one is left as text.
const maxCallout = 99

// lineCommentPrefixes are the line comments a marker may follow, longest
// first where one is a prefix of another.
var lineCommentPrefixes = []string{"//", "#", "--", ";;", ";", "%", "'"}

// parseCallouts reads the markers of a code block. ok is false when there is
// none.
func parseCallouts(code string) (c codeCallouts, ok bool) {
	lines := strings.SplitAfter(code, "\n")
	var b strings.Builder
	b.Grow(len(code))
	for i, raw := range lines {
		body, nl := strings.CutSuffix(raw, "\n")
		if rest, nums, found := cutCallouts(body); found {
			c.lines = append(c.lines, lineCallouts{line: i, nums: nums})
			for _, n := range nums {
				c.max = max(c.max, n)
			}
			body = rest
		}
		b.WriteString(body)
		if nl {
			b.WriteByte('\n')
		}
	}
	if len(c.lines) == 0 {
		return codeCallouts{}, false
	}
	c.code = b.String()
	return c, true
}

// cutCallouts removes the markers ending a line, with the comment that only
// held them, and returns what is left and the numbers in order.
func cutCallouts(line string) (rest string, nums []int, ok bool) {
	s := strings.TrimRight(line, " \t")
	// Wrapped forms: <!--1-->, <!-- <1> <2> -->, /* <1> */.
	for _, w := range [][2]string{{"<!--", "-->"}, {"/*", "*/"}} {
		if !strings.HasSuffix(s, w[1]) {
			continue
		}
		open := strings.LastIndex(s[:len(s)-len(w[1])], w[0])
		if open < 0 {
			continue // also "/*/" or "<!-->": the opener overlaps the closer
		}
		inner := strings.TrimSpace(s[open+len(w[0]) : len(s)-len(w[1])])
		var ns []int
		if n, good := calloutNumber(inner); good && w[0] == "<!--" {
			ns = []int{n}
		} else if ns = markerList(inner); ns == nil {
			continue
		}
		return strings.TrimRight(s[:open], " \t"), ns, true
	}
	// <n> markers at the very end, each after white space.
	var rev []int
	for {
		if !strings.HasSuffix(s, ">") {
			break
		}
		lt := strings.LastIndexByte(s, '<')
		if lt < 1 || (s[lt-1] != ' ' && s[lt-1] != '\t') {
			break
		}
		n, good := calloutNumber(s[lt+1 : len(s)-1])
		if !good {
			break
		}
		rev = append(rev, n)
		s = strings.TrimRight(s[:lt], " \t")
	}
	if len(rev) == 0 {
		return line, nil, false
	}
	for i := len(rev) - 1; i >= 0; i-- {
		nums = append(nums, rev[i])
	}
	// A comment that held only the markers goes with them.
	for _, p := range lineCommentPrefixes {
		if strings.HasSuffix(s, p) {
			before := s[:len(s)-len(p)]
			if before == "" || strings.HasSuffix(before, " ") || strings.HasSuffix(before, "\t") {
				return strings.TrimRight(before, " \t"), nums, true
			}
		}
	}
	// Otherwise the markers must follow a comment's own text.
	if hasLineComment(s) {
		return s, nums, true
	}
	return line, nil, false
}

// hasLineComment reports a line comment in s: a prefix at the start of the
// line or after white space, followed by white space.
func hasLineComment(s string) bool {
	for _, p := range lineCommentPrefixes {
		for i := 0; ; {
			j := strings.Index(s[i:], p)
			if j < 0 {
				break
			}
			at := i + j
			end := at + len(p)
			startOK := at == 0 || s[at-1] == ' ' || s[at-1] == '\t'
			if startOK && end < len(s) && (s[end] == ' ' || s[end] == '\t') {
				return true
			}
			i = at + 1
		}
	}
	return false
}

// markerList reads "<1> <2>" (white space between): nil if it is anything
// else.
func markerList(s string) []int {
	var ns []int
	for _, f := range strings.Fields(s) {
		if len(f) < 3 || f[0] != '<' || f[len(f)-1] != '>' {
			return nil
		}
		n, ok := calloutNumber(f[1 : len(f)-1])
		if !ok {
			return nil
		}
		ns = append(ns, n)
	}
	return ns
}

// calloutNumber reads 1 to maxCallout, written without a leading zero.
func calloutNumber(s string) (int, bool) {
	if s == "" || len(s) > 2 || s[0] == '0' {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > maxCallout {
		return 0, false
	}
	return n, true
}

// calloutList returns the list that holds a code block's notes: the ordered
// list right after it, starting at 1, with an item for every number used.
func calloutList(f *ast.FencedCodeBlock, c codeCallouts) (*ast.List, bool) {
	l, ok := f.NextSibling().(*ast.List)
	if !ok || !l.IsOrdered() || l.Start != 1 || l.ChildCount() < c.max {
		return nil, false
	}
	return l, true
}

// scanCallouts finds a slide's code blocks with callouts and their lists:
// the block's info keeps the parse, the list takes the class it is styled
// by and its items their numbers.
func (d *Deck) scanCallouts(f *ast.FencedCodeBlock, info func(ast.Node) *nodeInfo) {
	var code strings.Builder
	for i := 0; i < f.Lines().Len(); i++ {
		seg := f.Lines().At(i)
		code.Write(seg.Value(d.source))
	}
	c, ok := parseCallouts(code.String())
	if !ok {
		return
	}
	l, ok := calloutList(f, c)
	if !ok {
		return
	}
	info(f).callouts = &c
	l.SetAttributeString("class", []byte("co-list"))
	i := 0
	for it := l.FirstChild(); it != nil; it = it.NextSibling() {
		i++
		info(it).callout = i
	}
	if i > d.maxCallout {
		d.maxCallout = i
	}
}

// withCallouts writes a disc at the end of each marked line of a block's
// markup: before its line feed, so after whatever the highlighter wrapped
// the line's last token in.
func withCallouts(markup string, c *codeCallouts) string {
	marks := map[int][]int{}
	for _, l := range c.lines {
		marks[l.line] = l.nums
	}
	var b strings.Builder
	b.Grow(len(markup) + 64*len(c.lines))
	line := 0
	disc := func() {
		for _, n := range marks[line] {
			b.WriteString(`<span class="co-disc" data-co="` + strconv.Itoa(n) + `" aria-hidden="true"></span>`)
		}
	}
	for {
		nl := strings.IndexByte(markup, '\n')
		if nl < 0 {
			b.WriteString(markup)
			if markup != "" {
				disc()
			}
			return b.String()
		}
		b.WriteString(markup[:nl])
		disc()
		b.WriteByte('\n')
		markup = markup[nl+1:]
		line++
	}
}

// UsesCallouts returns the largest callout number a deck draws, 0 for none:
// the page then carries the style for 1 to that number.
func (d *Deck) UsesCallouts() int { return d.maxCallout }

// CalloutsCSS is the style of the discs, for numbers 1 to n: a ring in the
// code's own colour, the number written by the style sheet (::before) so
// that it is not in the text, out of selection; the notes' list numbered by
// the same discs.
func CalloutsCSS(n int) string {
	var b strings.Builder
	b.WriteString(`section pre .co-disc,section ol.co-list>li::before{display:inline-block;box-sizing:border-box;width:1.5em;height:1.5em;line-height:1.35em;border:.08em solid currentcolor;border-radius:50%;font-size:.7em;font-weight:bold;text-align:center;vertical-align:.15em;-webkit-user-select:none;user-select:none}`)
	// No opacity: a translucent element is drawn as a picture in the PDF and
	// the PowerPoint, where the disc must stay a vector ring.
	b.WriteString(`section pre .co-disc{margin-left:.7em}`)
	b.WriteString(`section ol.co-list{list-style:none;padding-left:0}section ol.co-list>li::before{margin-right:.6em}`)
	for i := 1; i <= n; i++ {
		s := strconv.Itoa(i)
		b.WriteString(`section pre .co-disc[data-co="` + s + `"]::before,section ol.co-list>li[data-co="` + s + `"]::before{content:"` + s + `"}`)
	}
	return b.String()
}
