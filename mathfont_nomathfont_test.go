//go:build slides_nomathfont

package slides

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-slides/slides/mathfont"
)

// Built with slides_nomathfont no font is linked: a formula stays its TeX
// source in a math-error span that says why, and the rest of the deck renders.
func TestNoMathFontBuildKeepsTheSource(t *testing.T) {
	if mathfont.Registered() {
		t.Fatal("a provider is registered in a build without the embedded one")
	}
	if _, err := Math(`x^2`, false); !errors.Is(err, mathfont.ErrUnavailable) {
		t.Fatalf("Math without a font: %v", err)
	}
	page, b, err := HTML([]byte("# Hi 😄\n\nEnergy $E=mc^2$.\n\n$$\n\\int_0^1 x\\,dx\n$$\n"), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page, `class="slides-math"`) {
		t.Error("a typeset formula without a font")
	}
	if strings.Count(page, `class="math-error"`) != 2 || !strings.Contains(page, "$E=mc^2$") || !strings.Contains(page, "mathfont: the math font is not available") {
		t.Errorf("the source and the reason are not kept: %.300s", page)
	}
	if len(b.Warnings) != 0 {
		t.Errorf("warnings: %q", b.Warnings)
	}
}

// TestNoMathFontBuildLinksNoFontProvider: the tag leaves out the embedded
// provider, the only thing that refers to go-tex/math's font.
func TestNoMathFontBuildLinksNoFontProvider(t *testing.T) {
	for _, js := range []bool{false, true} {
		out := goList(t, js, "slides_nomathfont")
		for _, line := range strings.Split(out, "\n") {
			if strings.HasSuffix(line, "/mathfont/embedded") {
				t.Errorf("js=%v: slides_nomathfont still links %s", js, line)
			}
		}
		if !strings.Contains(out, "github.com/go-slides/slides/mathfont\n") || !strings.Contains(out, "github.com/go-tex/math\n") {
			t.Errorf("js=%v: package mathfont, and go-tex/math's code, stay: go list does not list them", js)
		}
	}
}
