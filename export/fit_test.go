package export

import (
	"bytes"
	"context"
	"image"
	"strings"
	"testing"

	"github.com/go-webengine/engine"

	"github.com/go-slides/slides/deck"
)

// fitDeck has fitting headings in three themes, two on one slide, one with
// inline markup, and a slide without one.
const fitDeck = `---
theme: gaia
---

# <!-- fit --> A fitting heading far too long for one line of the slide

---

# <!-- fit --> **Bold**, *italic* and ` + "`code`" + `

## <!-- fit --> and a second, much longer fitting heading under the first

---

<!-- theme: uncover -->

# <!-- fit --> Hi

---

<!-- theme: default -->

# Not fitted
`

// fitHeadings lays a slide out as deck.FitScript does in a browser, pixel
// for pixel: each slide with a fitting heading is laid out by a
// JavaScript-enabled engine running the script, and by the export's engine
// fitting it natively, and the two are painted alike.
func TestFitHeadingsMatchFitScript(t *testing.T) {
	b := build(t, fitDeck)
	faces, err := bundledFaces()
	if err != nil {
		t.Fatal(err)
	}
	scripted := engine.New()
	scripted.FontFaces = faces
	native := engine.New()
	native.DisableJS = true
	native.FontFaces = faces
	native.Adjust = fitHeadings
	plain := engine.New()
	plain.DisableJS = true
	plain.FontFaces = faces
	view := image.Rect(0, 0, int(b.Width), int(b.Height))
	paint := func(e *engine.Engine, html string) []byte {
		l, err := e.LayoutHTML(context.Background(), html, "", view)
		if err != nil {
			t.Fatal(err)
		}
		return l.Paint().Pix
	}
	fitted := 0
	for i, s := range b.Slides {
		html := page(b.CSS, s.HTML)
		if !fits(s.HTML) {
			continue
		}
		fitted++
		js := paint(scripted, strings.Replace(html, "</body>", "<script>"+deck.FitScript+"</script></body>", 1))
		if !bytes.Equal(js, paint(native, html)) {
			t.Errorf("slide %d: the native fit is not the script's", i+1)
		}
		if bytes.Equal(js, paint(plain, html)) {
			t.Errorf("slide %d: the script fitted nothing, so the comparison proves nothing", i+1)
		}
	}
	if fitted != 3 {
		t.Fatalf("%d slides with a fitting heading, want 3", fitted)
	}
}
