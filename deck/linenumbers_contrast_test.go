package deck

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

// The numbers' colour, the code's colour mixed with its box's background in
// sRGB (CSS Color 5 §3: color-mix(in srgb, fg p%, bg)), has a contrast of
// 4.5:1 or more against the box (WCAG 2.2, 1.4.3) wherever the code itself
// has, and of 3:1 or more where the theme's own code has less. The colours
// are the built-in themes' values (theme/marp/*.css): default's
// --fgColor-default on --bgColor-muted, gaia's pre (--color-background on
// --color-foreground, and its .gaia class), uncover's --color-foreground on
// --color-background-code; light and dark.
func TestLineNumberContrast(t *testing.T) {
	for _, c := range []struct {
		theme, scheme, fg, bg string
	}{
		{"default", "light", "#1f2328", "#f6f8fa"},
		{"default", "dark", "#f0f6fc", "#151b23"},
		{"gaia", "light", "#fff8e1", "#455a64"},
		{"gaia", "dark", "#455a64", "#fff8e1"},
		{"gaia", ".gaia", "#0288d1", "#fff8e1"},
		{"uncover", "light", "#202228", "#f2f1f4"},
		{"uncover", "dark", "#ffffff", "#2b2d33"},
	} {
		ink, ok := lineNumberInk[c.theme]
		if !ok {
			t.Fatalf("%s: no ink", c.theme)
		}
		fg, bg := hexRGB(t, c.fg), hexRGB(t, c.bg)
		p := float64(ink.pct) / 100
		var mixed [3]float64
		for i := range mixed {
			mixed[i] = fg[i]*p + bg[i]*(1-p)
		}
		code, numbers := contrast(fg, bg), contrast(mixed, bg)
		floor := 4.5
		if code < 4.5 {
			floor = 3
		}
		if numbers < floor {
			t.Errorf("%s %s: numbers at %.2f:1 (code %.2f:1), want %.1f:1 or more", c.theme, c.scheme, numbers, code, floor)
		}
		// muted: visibly less than the code
		if numbers >= code {
			t.Errorf("%s %s: numbers as contrasted as the code", c.theme, c.scheme)
		}
	}
	// The style names the mix, and no opacity.
	css := LineNumbersCSS("gaia")
	if !strings.Contains(css, "color-mix(in srgb,var(--color-background) 90%,var(--color-foreground))") || strings.Contains(css, "opacity") {
		t.Errorf("style: %s", css)
	}
	if strings.Contains(LineNumbersCSS("custom"), "color-mix") {
		t.Error("an unknown theme keeps the code's colour")
	}
}

func hexRGB(t *testing.T, s string) (c [3]float64) {
	t.Helper()
	for i := range c {
		v, err := strconv.ParseUint(s[1+2*i:3+2*i], 16, 8)
		if err != nil {
			t.Fatal(err)
		}
		c[i] = float64(v)
	}
	return c
}

// luminance is WCAG 2.2's relative luminance of an sRGB colour (0..255).
func luminance(c [3]float64) float64 {
	var l [3]float64
	for i, v := range c {
		v /= 255
		if v <= 0.04045 {
			l[i] = v / 12.92
		} else {
			l[i] = math.Pow((v+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*l[0] + 0.7152*l[1] + 0.0722*l[2]
}

func contrast(a, b [3]float64) float64 {
	la, lb := luminance(a), luminance(b)
	return (math.Max(la, lb) + 0.05) / (math.Min(la, lb) + 0.05)
}
