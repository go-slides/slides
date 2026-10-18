package deck

import (
	"errors"
	"strings"
	"testing"
)

func mathOf(t *testing.T, src string) string {
	t.Helper()
	d := parseDeck(t, src, Options{NoTemplate: true})
	return d.Render(RenderOptions{Frame: FramePlain, Math: func(tex string, display bool) (string, error) {
		if tex == "bad" {
			return "", errors.New("missing }")
		}
		if display {
			return "[D:" + tex + "]", nil
		}
		return "[I:" + tex + "]", nil
	}})[0].HTML
}

// TestMathSyntax pins Marp Core's delimiter rules (marp_math_inline and
// marp_math_block in marp-core 4.4.0).
func TestMathSyntax(t *testing.T) {
	cases := map[string]string{
		"a $x^2$ b":       "a [I:x^2] b",
		"$ x$ stays":      "$ x$ stays",     // not openable: space after $
		"$x $ stays":      "$x $ stays",     // not closable: space before $
		"cost $5 and $6":  "cost $5 and $6", // not closable: digit after $
		"empty $$ here":   "empty $$ here",  // $$ is text
		"no close $x":     "no close $x",    // no closing $
		`escaped \$x$`:    "escaped $x$",    // an escaped $ never opens
		`a $x\$y$ b`:      `a [I:x\$y] b`,   // an escaped $ does not close
		"two $a$ and $b$": "two [I:a] and [I:b]",
		"$bad$":           `<span class="math-error" title="missing }">$bad$</span>`,
	}
	for src, want := range cases {
		if got := mathOf(t, src+"\n"); !strings.Contains(got, want) {
			t.Errorf("%q: got %s, want it to contain %q", src, got, want)
		}
	}
}

func TestMathBlock(t *testing.T) {
	cases := map[string]string{
		"$$ x^2 $$\n":                 "<p>[D:x^2]</p>",
		"$$\na\nb\n$$\n":              "<p>[D:a\nb\n]</p>",
		"$$ first\nsecond $$\n":       "<p>[D:first\nsecond]</p>",
		"para\n$$\nx\n$$\n":           "<p>para</p>\n<p>[D:x\n]</p>",
		"- item\n\n  $$\n  y\n  $$\n": "[D:",
	}
	for src, want := range cases {
		if got := mathOf(t, src); !strings.Contains(got, want) {
			t.Errorf("%q: got %s, want it to contain %q", src, got, want)
		}
	}
}

func TestFitHeadingMarkup(t *testing.T) {
	d := parseDeck(t, "# <!-- fit --> Fit me\n\n## plain\n", Options{})
	on := d.Render(RenderOptions{Frame: FramePlain, FitHeadings: true})[0].HTML
	if !strings.Contains(on, `<h1 data-auto-scaling="" style="white-space:nowrap" id="fit-me"><span data-fit-content="" style="white-space:nowrap"> Fit me</span></h1>`) {
		t.Errorf("fit markup: %s", on)
	}
	if strings.Count(on, "data-fit-content") != 1 {
		t.Error("only the marked heading fits")
	}
	if off := d.Render(RenderOptions{Frame: FramePlain})[0].HTML; strings.Contains(off, "data-auto-scaling") {
		t.Errorf("without FitHeadings, Marpit's plain output: %s", off)
	}
}

func TestMathWithoutEngine(t *testing.T) {
	d := parseDeck(t, "a $x$ b\n\n$$\ny\n$$\n", Options{})
	html := d.Render(RenderOptions{Frame: FramePlain})[0].HTML
	if !strings.Contains(html, "a $x$ b") || !strings.Contains(html, "$$\ny\n$$") {
		t.Errorf("without an engine the source stays: %s", html)
	}
}
