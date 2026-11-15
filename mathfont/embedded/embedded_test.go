//go:build !slides_nomathfont

package embedded_test

import (
	"bytes"
	"testing"

	"github.com/go-tex/math"

	"github.com/go-slides/slides/mathfont"
	"github.com/go-slides/slides/mathfont/embedded"
)

// TestEmbeddedIsGoTexsFont: the default provider is the font go-tex/math
// embeds, which is why the default build writes the same bytes as before.
func TestEmbeddedIsGoTexsFont(t *testing.T) {
	if !mathfont.Registered() {
		t.Fatal("the embedded provider did not register")
	}
	if len(math.DefaultFont()) > mathfont.MaxFontBytes {
		t.Fatalf("the font is over the bound: %d bytes", len(math.DefaultFont()))
	}
	r, err := mathfont.Renderer()
	if err != nil {
		t.Fatal(err)
	}
	want, err := math.New(math.DefaultFont())
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := r.RenderSVGMetrics(`\sum_{i=1}^n \alpha_i`, 100)
	if err != nil {
		t.Fatal(err)
	}
	b, _, _ := want.RenderSVGMetrics(`\sum_{i=1}^n \alpha_i`, 100)
	if !bytes.Equal([]byte(a), []byte(b)) {
		t.Error("the provider's renderer differs from math.New(math.DefaultFont())")
	}
	mathfont.Register(nil)
	embedded.Register()
	if !mathfont.Registered() {
		t.Error("Register did not put the embedded font back")
	}
}
