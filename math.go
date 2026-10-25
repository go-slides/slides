package slides

import (
	"fmt"
	"html"
	"strings"
	"sync"

	"github.com/go-tex/math"
)

// mathRef is the pixel size formulas are typeset at; every dimension is then
// divided by it and written in em, so a formula follows the font size of the
// text around it, as MathJax's output does in Marp.
const mathRef = 100

var (
	mathOnce     sync.Once
	mathRenderer *math.Renderer
	mathErr      error
)

// Math typesets a formula with go-tex/math (STIX Two Math) into inline SVG.
// An inline formula sits on the text's baseline; a display one is centred
// on a line of its own.
func Math(tex string, display bool) (string, error) {
	mathOnce.Do(func() { mathRenderer, mathErr = math.New(math.DefaultFont()) })
	if mathErr != nil {
		return "", mathErr
	}
	var (
		svg string
		m   math.Metrics
		err error
	)
	if display {
		svg, m, err = mathRenderer.RenderDisplaySVGMetrics(tex, mathRef)
	} else {
		svg, m, err = mathRenderer.RenderSVGMetrics(tex, mathRef)
	}
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
