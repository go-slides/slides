package css

import "strings"

// Flatten resolves CSS nesting: a rule nested in a rule becomes a sibling
// whose selector combines both, and a conditional at-rule nested in a rule
// wraps a copy of the parent rule.
//
// Marpit runs postcss-nesting, then turns the :is() it produces back into
// plain selectors when they start from section or :root, so that its later
// prefixing sees a section. Expanding every parent/child combination gives
// those plain selectors directly.
func Flatten(nodes []Node) []Node {
	var out []Node
	for _, n := range nodes {
		switch x := n.(type) {
		case *Rule:
			out = append(out, flattenRule(SplitSelectors(x.Selector), x.Nodes)...)
		case *AtRule:
			if x.Block {
				x.Nodes = Flatten(x.Nodes)
			}
			out = append(out, x)
		default:
			out = append(out, n)
		}
	}
	return out
}

// flattenRule returns the rules a (possibly nested) rule stands for, in
// source order: declarations before a nested rule stay in a rule of their
// own, and declarations after it start a new one.
func flattenRule(parents []string, nodes []Node) []Node {
	var out []Node
	var decls []Node
	flush := func() {
		if len(decls) > 0 {
			out = append(out, &Rule{Selector: JoinSelectors(parents), Nodes: decls})
			decls = nil
		}
	}
	for _, n := range nodes {
		switch x := n.(type) {
		case *Rule:
			flush()
			out = append(out, flattenRule(combine(parents, SplitSelectors(x.Selector)), x.Nodes)...)
		case *AtRule:
			if !x.Block {
				decls = append(decls, x)
				continue
			}
			flush()
			out = append(out, &AtRule{Name: x.Name, Params: x.Params, Block: true, Nodes: flattenRule(parents, x.Nodes)})
		default:
			decls = append(decls, n)
		}
	}
	flush()
	return out
}

// combine resolves nested selectors against their parents: "&" stands for
// the parent, and a selector without one is a descendant of it, or, when
// it starts with a combinator, relative to it.
func combine(parents, children []string) []string {
	var out []string
	for _, c := range children {
		for _, p := range parents {
			if strings.Contains(c, "&") {
				out = append(out, strings.ReplaceAll(c, "&", p))
			} else {
				out = append(out, p+" "+c)
			}
		}
	}
	return out
}

// HasNesting reports whether any rule holds a rule.
func HasNesting(nodes []Node) bool {
	found := false
	Walk(nodes, func(n, parent Node) bool {
		if _, ok := n.(*Rule); ok {
			if _, inRule := parent.(*Rule); inRule {
				found = true
			}
		}
		return !found
	})
	return found
}
