package slides

import (
	"strings"
	"testing"
)

func TestMath(t *testing.T) {
	out, err := Math(`x^2 + \frac{a}{b}`, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<svg xmlns=`, `class="slides-math"`, "vertical-align:-0.", "viewBox=", "currentColor"} {
		if !strings.Contains(out, want) {
			t.Errorf("inline formula lacks %q", want)
		}
	}
	disp, err := Math(`\sum_{i=1}^n i`, true)
	if err != nil || !strings.Contains(disp, "slides-math-display") {
		t.Errorf("display formula: %v %.80s", err, disp)
	}
	if _, err := Math(`\frac{`, false); err == nil {
		t.Error("a malformed formula is an error")
	}
	if got := sized(`<svg width="10" height="20" viewBox="0 0 10 20"><path/></svg>`, `style="a"`); got != `<svg viewBox="0 0 10 20" style="a"><path/></svg>` {
		t.Errorf("sized: %s", got)
	}
	if root, _, _ := strings.Cut(out, ">"); strings.Contains(root, ` width="`) {
		t.Error("the reference pixel size must not stay on the svg")
	}
}

func TestMathInDeck(t *testing.T) {
	page, _, err := HTML([]byte("Energy $E=mc^2$.\n\n$$\n\\int_0^1 x\\,dx\n$$\n"), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(page, `class="slides-math"`) != 2 || strings.Contains(page, "$E=mc^2$") {
		t.Errorf("both formulas are typeset")
	}
	page, _, _ = HTML([]byte("Energy $E=mc^2$.\n"), Config{NoMath: true})
	if !strings.Contains(page, "$E=mc^2$") {
		t.Error("NoMath leaves the source")
	}
}
