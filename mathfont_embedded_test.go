//go:build !slides_nomathfont

package slides

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-tex/math"

	"github.com/go-slides/slides/mathfont"
	"github.com/go-slides/slides/mathfont/embedded"
)

// The default build links the embedded provider: the other half of
// TestNoMathFontBuildLinksNoFontProvider.
func TestDefaultBuildLinksTheEmbeddedFont(t *testing.T) {
	if !strings.Contains(goList(t, false), "github.com/go-slides/slides/mathfont/embedded\n") {
		t.Error("the default build does not link mathfont/embedded")
	}
}

// lateFont is a provider whose font arrives later, as a browser host fetches it.
type lateFont struct{ loaded atomic.Bool }

func (l *lateFont) Font() ([]byte, error) {
	if !l.loaded.Load() {
		return nil, errors.New("not fetched yet")
	}
	return math.DefaultFont(), nil
}

// TestMathBeforeAndAfterTheFont: through a whole render, with a font that
// arrives later (the contract of package mathfont). Before it the formula is
// its source, with the reason; after Register and a new render it is typeset,
// and the page is the embedded font's, byte for byte.
func TestMathBeforeAndAfterTheFont(t *testing.T) {
	t.Cleanup(embedded.Register)
	const src = "Energy $E=mc^2$ and\n\n$$\n\\int_0^1 x\\,dx\n$$\n"
	want, _, err := HTML([]byte(src), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(want, `class="slides-math"`) != 2 {
		t.Fatal("the embedded font did not typeset")
	}
	l := &lateFont{}
	mathfont.Register(l)
	before, b, err := HTML([]byte(src), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(before, `class="slides-math"`) || strings.Count(before, `class="math-error"`) != 2 || !strings.Contains(before, "$E=mc^2$") {
		t.Errorf("before the font: %.300s", before)
	}
	if len(b.Warnings) != 0 {
		t.Errorf("warnings before the font: %q", b.Warnings)
	}
	l.loaded.Store(true)
	mathfont.Register(l)
	after, _, err := HTML([]byte(src), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if after != want {
		t.Errorf("after the font: %d bytes, the embedded font's page is %d", len(after), len(want))
	}
}
