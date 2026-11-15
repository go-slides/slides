package slides

import (
	"fmt"
	"html"
	"strings"

	"github.com/go-tex/math"

	"github.com/go-slides/slides/mathfont"
)

// mathRef is the pixel size formulas are typeset at; every dimension is then
// divided by it and written in em, so a formula follows the font size of the
// text around it, as MathJax's output does in Marp.
const mathRef = 100

// Math typesets a formula with go-tex/math into inline SVG, with the font of
// the registered mathfont.Provider (STIX Two Math unless the build has the tag
// slides_nomathfont). An inline formula sits on the text's baseline; a display
// one is centred on a line of its own. Without a font (no provider, or its
// font has not arrived) it returns an error that wraps mathfont.ErrUnavailable,
// and the deck keeps the formula's source.
func Math(tex string, display bool) (string, error) {
	r, err := mathfont.Renderer()
	if err != nil {
		return "", err
	}
	var (
		svg string
		m   math.Metrics
	)
	err = mathfont.Safely(func() error {
		var err error
		if display {
			svg, m, err = r.RenderDisplaySVGMetrics(tex, mathRef)
		} else {
			svg, m, err = r.RenderSVGMetrics(tex, mathRef)
		}
		return err
	})
	if err != nil {
		return "", err
	}
	em := func(px float64) string { return fmt.Sprintf("%.4gem", px/mathRef) }
	// The size goes in the SVG's style: CSS sizes an SVG in any length,
	// and its viewBox scales the drawing to it.
	style := `width:` + em(m.Width) + `;height:` + em(m.Height+m.Depth) + `;vertical-align:-` + em(m.Depth)
	// A formula says what it is: role="math" and its TeX source as its
	// name, which a screen reader reads, and a tagged PDF makes the
	// Formula's /Alt (go-webengine's pdfcanvas).
	label := html.EscapeString(strings.TrimSpace(tex))
	inner := sized(svg, `class="slides-math" style="`+style+`" role="math" aria-label="`+label+`"`)
	if display {
		return `<span class="slides-math-display" style="display:block;text-align:center">` + inner + `</span>`, nil
	}
	return inner, nil
}

// sized drops the root <svg>'s width and height attributes, which hold the
// reference pixel size, and adds attrs in their place.
func sized(svg, attrs string) string {
	end := strings.IndexByte(svg, '>')
	if end < 0 || !strings.HasPrefix(svg, "<svg") {
		return svg
	}
	head := svg[:end]
	for _, attr := range []string{"width", "height"} {
		i := strings.Index(head, " "+attr+`="`)
		if i < 0 {
			continue
		}
		j := strings.IndexByte(head[i+len(attr)+3:], '"')
		if j < 0 {
			continue
		}
		head = head[:i] + head[i+len(attr)+3+j+1:]
	}
	return head + " " + attrs + svg[end:]
}
