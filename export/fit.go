package export

import (
	"context"

	"github.com/go-webengine/engine"
	"github.com/go-webengine/engine/dom"
)

// fitHeadings is deck.FitScript, Marp's fitting header, in Go: run by the
// engine where the script would run (engine.Adjust), it reads the same
// geometry in the same order and computes the same lengths, so the slide is
// the one the script fits — without a JavaScript engine, which a browser
// worker would otherwise download only for this.
//
// For each element marked data-fit-content whose parent is marked
// data-auto-scaling ('[data-auto-scaling]>[data-fit-content]', in document
// order), the parent's font size is scaled by the width available in it
// (its clientWidth less its horizontal padding) over the content's
// offsetWidth.
func fitHeadings(_ context.Context, root *dom.Node, g *engine.Geometry) {
	for _, c := range fitTargets(root) {
		h := c.Parent
		avail := float64(g.ClientWidth(h)) - pxOr0(g.Computed(h, "padding-left")) - pxOr0(g.Computed(h, "padding-right"))
		natural := float64(g.OffsetWidth(c))
		if natural > 0 && avail > 0 {
			size := engine.JSParseFloat(g.Computed(h, "font-size")) * avail / natural
			h.SetStyleProperty("font-size", engine.JSNumberString(size)+"px")
		}
	}
}

// pxOr0 is the script's parseFloat(v||0): an empty value is 0.
func pxOr0(v string) float64 {
	if v == "" {
		return 0
	}
	return engine.JSParseFloat(v)
}

// fitTargets are the elements querySelectorAll('[data-auto-scaling]>
// [data-fit-content]') returns: in document order, not inside a template's
// content or a shadow tree.
func fitTargets(root *dom.Node) []*dom.Node {
	var out []*dom.Node
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		for _, c := range n.Children {
			if c.Type == dom.Element && n.Type == dom.Element {
				_, fit := c.Attribute("data-fit-content")
				_, auto := n.Attribute("data-auto-scaling")
				if fit && auto {
					out = append(out, c)
				}
			}
			walk(c)
		}
	}
	walk(root)
	return out
}
