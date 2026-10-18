// Package css is the small CSS tree Marpit's theme pipeline needs: rules,
// at-rules, declarations and comments, parsed leniently, rewritten, and
// written back out. It plays the part postcss plays in Marpit; it is not a
// validating parser, and keeps what it does not understand as written.
package css

import (
	"strings"
)

// Node is a Comment, a Decl, a Rule or an AtRule.
type Node interface{ node() }

// Comment is a /* comment */; Text excludes the delimiters.
type Comment struct{ Text string }

// Decl is a declaration.
type Decl struct {
	Prop, Value string
	Important   bool
}

// Rule is a style rule.
type Rule struct {
	Selector string
	Nodes    []Node
}

// AtRule is an at-rule; Nodes is nil for a statement (@import ...;) and
// non-nil, possibly empty, for a block.
type AtRule struct {
	Name, Params string
	Nodes        []Node
	Block        bool
}

func (*Comment) node() {}
func (*Decl) node()    {}
func (*Rule) node()    {}
func (*AtRule) node()  {}

// Sheet is a parsed style sheet.
type Sheet struct{ Nodes []Node }

// Parse parses a style sheet.
func Parse(src string) *Sheet {
	p := &parser{s: src}
	return &Sheet{Nodes: p.block(false)}
}

type parser struct {
	s   string
	pos int
}

// block parses nodes until the end of input or, when nested, a closing
// brace (consumed).
func (p *parser) block(nested bool) []Node {
	nodes := []Node{}
	for {
		p.skipSpace()
		if p.pos >= len(p.s) {
			return nodes
		}
		c := p.s[p.pos]
		switch {
		case c == '}':
			p.pos++
			if nested {
				return nodes
			}
		case c == ';':
			p.pos++
		case strings.HasPrefix(p.s[p.pos:], "/*"):
			end := strings.Index(p.s[p.pos+2:], "*/")
			if end < 0 {
				nodes = append(nodes, &Comment{Text: p.s[p.pos+2:]})
				p.pos = len(p.s)
				continue
			}
			nodes = append(nodes, &Comment{Text: p.s[p.pos+2 : p.pos+2+end]})
			p.pos += end + 4
		case c == '@':
			nodes = append(nodes, p.atRule())
		default:
			nodes = append(nodes, p.ruleOrDecl())
		}
	}
}

func (p *parser) skipSpace() {
	for p.pos < len(p.s) && strings.IndexByte(" \t\n\r\f", p.s[p.pos]) >= 0 {
		p.pos++
	}
}

// scan reads up to (not including) the first top-level byte of stop,
// skipping strings, comments and bracketed groups.
func (p *parser) scan(stop string) (string, byte) {
	start := p.pos
	depth := 0
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		switch {
		case c == '"' || c == '\'':
			p.skipString(c)
			continue
		case c == '\\':
			p.pos += 2
			continue
		case strings.HasPrefix(p.s[p.pos:], "/*"):
			end := strings.Index(p.s[p.pos+2:], "*/")
			if end < 0 {
				p.pos = len(p.s)
				continue
			}
			p.pos += end + 4
			continue
		case c == '(' || c == '[':
			depth++
		case c == ')' || c == ']':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && strings.IndexByte(stop, c) >= 0 {
				return p.s[start:p.pos], c
			}
		}
		p.pos++
	}
	return p.s[start:p.pos], 0
}

func (p *parser) skipString(q byte) {
	p.pos++
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		if c == '\\' {
			p.pos += 2
			continue
		}
		p.pos++
		if c == q || c == '\n' {
			return
		}
	}
}

func (p *parser) atRule() Node {
	p.pos++ // @
	start := p.pos
	for p.pos < len(p.s) && (isIdent(p.s[p.pos])) {
		p.pos++
	}
	name := p.s[start:p.pos]
	params, stop := p.scan(";{}")
	a := &AtRule{Name: name, Params: strings.TrimSpace(params)}
	switch stop {
	case ';':
		p.pos++
	case '{':
		p.pos++
		a.Block = true
		a.Nodes = p.block(true)
	}
	return a
}

func isIdent(c byte) bool {
	return c == '-' || c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}

// ruleOrDecl reads a rule or, inside a block, a declaration. A declaration
// ends at ';' or '}' before any '{'; a rule's prelude ends at '{'.
func (p *parser) ruleOrDecl() Node {
	start := p.pos
	text, stop := p.scan(";{}")
	if stop == '{' && !looksLikeDecl(text) {
		p.pos++
		return &Rule{Selector: strings.TrimSpace(text), Nodes: p.block(true)}
	}
	if stop == '{' {
		// A declaration whose value holds a block (custom properties
		// may): read the whole value.
		p.pos = start
		text, stop = p.scanDeclValue()
	}
	if stop == ';' {
		p.pos++
	}
	return parseDecl(text)
}

// looksLikeDecl tells "prop: value {" (a custom property holding braces)
// from "a:hover {" (a rule): a custom property's name starts with "--".
func looksLikeDecl(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "--") && strings.Contains(text, ":")
}

func (p *parser) scanDeclValue() (string, byte) {
	start := p.pos
	depth := 0
	for p.pos < len(p.s) {
		c := p.s[p.pos]
		switch c {
		case '"', '\'':
			p.skipString(c)
			continue
		case '{', '(', '[':
			depth++
		case '}', ')', ']':
			if depth == 0 {
				return p.s[start:p.pos], c
			}
			depth--
		case ';':
			if depth == 0 {
				return p.s[start:p.pos], c
			}
		}
		p.pos++
	}
	return p.s[start:p.pos], 0
}

func parseDecl(text string) Node {
	prop, value, ok := strings.Cut(text, ":")
	if !ok {
		return &Decl{Prop: strings.TrimSpace(text)}
	}
	d := &Decl{Prop: strings.TrimSpace(prop), Value: strings.TrimSpace(value)}
	if i := strings.LastIndex(d.Value, "!"); i >= 0 && strings.EqualFold(strings.TrimSpace(d.Value[i+1:]), "important") {
		d.Important = true
		d.Value = strings.TrimSpace(d.Value[:i])
	}
	return d
}

// String writes the sheet out.
func (s *Sheet) String() string {
	var b strings.Builder
	write(&b, s.Nodes, "")
	return b.String()
}

func write(b *strings.Builder, nodes []Node, indent string) {
	for _, n := range nodes {
		switch x := n.(type) {
		case *Comment:
			b.WriteString(indent + "/*" + x.Text + "*/\n")
		case *Decl:
			b.WriteString(indent + x.Prop)
			if x.Value != "" || x.Important {
				b.WriteString(": " + x.Value)
			}
			if x.Important {
				b.WriteString(" !important")
			}
			b.WriteString(";\n")
		case *Rule:
			b.WriteString(indent + x.Selector + " {\n")
			write(b, x.Nodes, indent+"  ")
			b.WriteString(indent + "}\n")
		case *AtRule:
			b.WriteString(indent + "@" + x.Name)
			if x.Params != "" {
				b.WriteString(" " + x.Params)
			}
			if !x.Block {
				b.WriteString(";\n")
				continue
			}
			b.WriteString(" {\n")
			write(b, x.Nodes, indent+"  ")
			b.WriteString(indent + "}\n")
		}
	}
}

// SplitSelectors splits a selector list at its top-level commas.
func SplitSelectors(sel string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(sel); i++ {
		switch c := sel[i]; c {
		case '"', '\'':
			for i++; i < len(sel) && sel[i] != c; i++ {
				if sel[i] == '\\' {
					i++
				}
			}
		case '\\':
			i++
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(sel[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(sel[start:]))
}

// JoinSelectors joins a selector list.
func JoinSelectors(sels []string) string { return strings.Join(sels, ", ") }

// Walk calls fn for every node, depth first; fn returns false to skip a
// node's children.
func Walk(nodes []Node, fn func(n Node, parent Node) bool) {
	walk(nodes, nil, fn)
}

func walk(nodes []Node, parent Node, fn func(Node, Node) bool) {
	for _, n := range nodes {
		if !fn(n, parent) {
			continue
		}
		switch x := n.(type) {
		case *Rule:
			walk(x.Nodes, x, fn)
		case *AtRule:
			walk(x.Nodes, x, fn)
		}
	}
}
