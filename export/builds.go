package export

import (
	"fmt"
	"strings"

	"github.com/go-webengine/engine/officecanvas"
)

// Fragments in the editable decks. A slide's fragmented list items are
// revealed one by one in the HTML viewer. In a PPTX or ODP they become
// builds: one click-triggered entrance effect for each fragment, in fragment
// order, on the shapes of that fragment.
//
// Which shapes are a fragment's is not read from the markup but from the
// layout, so that it follows whatever the slide draws for an item (its text
// lines, its marker, a border or background of its own): the slide is laid
// out once for each step (EachSteps), step s with the first s fragments shown
// and the others hidden but in place, and a shape belongs to the first
// fragment whose step draws it. A shape every step draws belongs to none and
// is always visible.

// shapeKey identifies a shape across the steps of a slide: what it is, where,
// and what it says. Two shapes with one key are interchangeable.
func shapeKey(s officecanvas.Shape) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d|%.2f|%.2f|%.2f|%.2f|%v|%d", s.Kind, s.Bounds.X, s.Bounds.Y, s.Bounds.W, s.Bounds.H, s.Color, len(s.Group))
	for _, r := range s.Runs {
		b.WriteString("|" + r.Text)
	}
	return b.String()
}

// revealedAt returns, for each shape of the last page of steps, the first step
// that draws it. steps[s] is the slide with the first s fragments shown; its
// shapes are those of the same slide minus the hidden ones, so the shapes of
// a step are a sub-multiset of the next one's.
func revealedAt(steps []officecanvas.Page) []int {
	last := steps[len(steps)-1].Shapes
	counts := make([]map[string]int, len(steps))
	for s, pg := range steps {
		counts[s] = map[string]int{}
		for _, sh := range pg.Shapes {
			counts[s][shapeKey(sh)]++
		}
	}
	rank := map[string]int{}
	out := make([]int, len(last))
	for i, sh := range last {
		k := shapeKey(sh)
		r := rank[k]
		rank[k]++
		out[i] = len(steps) - 1
		for s := range steps {
			if r < counts[s][k] {
				out[i] = s
				break
			}
		}
	}
	return out
}

// fragmentShapes groups the indexes of the last page's shapes by the fragment
// that reveals them: result[k-1] holds fragment k's, in drawing order. Shapes
// every step draws are in no group.
func fragmentShapes(steps []officecanvas.Page) [][]int {
	n := len(steps) - 1
	out := make([][]int, n)
	for i, s := range revealedAt(steps) {
		if s > 0 {
			out[s-1] = append(out[s-1], i)
		}
	}
	return out
}
