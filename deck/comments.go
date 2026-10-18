package deck

import (
	"regexp"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// comment is an HTML comment, block or inline: a directive carrier or a
// speaker note.
type comment struct {
	node    ast.Node
	offset  int
	content string
	dirs    map[string]any
	// parsed is set once the comment is known not to be a note: it held a
	// recognized directive, a well-known magic comment, or a fit marker.
	parsed bool
}

var (
	commentMatcher = regexp.MustCompile(`<!--+\s*([\s\S]*?)\s*--+>`)
	// Marpit (comment.js) does not take these for notes.
	magicComments = []*regexp.Regexp{
		regexp.MustCompile(`^prettier-ignore(-(start|end))?$`),
		regexp.MustCompile(`^markdownlint-((disable|enable).*|capture|restore)$`),
		regexp.MustCompile(`^lint (disable|enable|ignore).*$`),
	}
)

// collectComments finds every HTML comment in document order, at any depth.
func collectComments(doc ast.Node, source []byte, loose bool) []*comment {
	var out []*comment
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		raw, offset, ok := commentText(n, source)
		if !ok {
			return ast.WalkContinue, nil
		}
		c := &comment{node: n, offset: offset}
		if m := commentMatcher.FindStringSubmatch(raw); m != nil {
			c.content = strings.TrimSpace(m[1])
		}
		c.dirs = parseDirectiveYAML(c.content, loose, looseKeys)
		for _, re := range magicComments {
			if re.MatchString(c.content) {
				c.parsed = true
			}
		}
		out = append(out, c)
		return ast.WalkSkipChildren, nil
	})
	return out
}

// commentText returns the raw text of a node that is an HTML comment.
func commentText(n ast.Node, source []byte) (string, int, bool) {
	switch x := n.(type) {
	case *ast.HTMLBlock:
		if x.HTMLBlockType != ast.HTMLBlockType2 {
			return "", 0, false
		}
		raw := blockText(x, source)
		if x.Lines().Len() == 0 {
			return "", 0, false
		}
		return raw, x.Lines().At(0).Start, true
	case *ast.RawHTML:
		if x.Segments.Len() == 0 {
			return "", 0, false
		}
		raw := string(x.Segments.Value(source))
		if !strings.HasPrefix(raw, "<!--") {
			return "", 0, false
		}
		return raw, x.Segments.At(0).Start, true
	}
	return "", 0, false
}

// blockText returns an HTML block's whole text, closing line included.
func blockText(b *ast.HTMLBlock, source []byte) string {
	var sb strings.Builder
	for i := 0; i < b.Lines().Len(); i++ {
		seg := b.Lines().At(i)
		sb.Write(seg.Value(source))
	}
	if b.HasClosure() {
		sb.Write(b.ClosureLine.Value(source))
	}
	return sb.String()
}

// isStyleBlock reports whether n is a <style> element Marpit takes over, and
// returns its content and whether it is scoped.
func isStyleBlock(n ast.Node, source []byte) (css string, scoped bool, ok bool) {
	b, isBlock := n.(*ast.HTMLBlock)
	if !isBlock || b.HTMLBlockType != ast.HTMLBlockType1 {
		return "", false, false
	}
	raw := blockText(b, source)
	if !styleOpening.MatchString(raw) {
		return "", false, false
	}
	if m := styleMatcher.FindStringSubmatch(raw); m != nil {
		return strings.TrimSpace(m[2]), styleScoped.MatchString(strings.TrimSpace(m[1])), true
	}
	return "", false, true
}

// Marpit's style matchers (markdown/style/parse.js).
var (
	styleMatcher = regexp.MustCompile(`(?i)<style([\s\S]*?)>([\s\S]*?)</style>`)
	styleOpening = regexp.MustCompile(`(?i)^\s*<style(\s|>|$)`)
	styleScoped  = regexp.MustCompile(`(?i)\bscoped\b`)
)
