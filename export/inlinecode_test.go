package export

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/go-webengine/engine"
)

// gaia sets code{display:inline-block} inside a section that has
// overflow-wrap:break-word, so an inline-block's word has to fit the box it sized.
// go-webengine's lines used to break a word that filled its box exactly, as
// "chapte" and "r" (a float rounding: go-webengine v0.15.2, FIDELITY.md round
// 185); the gallery's gaia decks avoided inline code for it. The editable PPTX
// holds the layout's text, a run for each piece a line holds: a broken word is
// two runs, a whole one is one.
func TestGaiaInlineCodeIsNotBrokenInTheWord(t *testing.T) {
	const deck = "---\ntheme: gaia\n---\n\nThe file **kinds/brand.yaml** defines `chapter`, `callout` and `quote`, and the text carries on.\n\n" +
		"| Kind | Meaning |\n|---|---|\n| `section` | a chapter break |\n"
	b := build(t, deck)
	w := NewPPTX(b)
	if err := EachLayout(context.Background(), b, Options{}, func(_ int, l *engine.PageLayout) error { return w.AddLayout(l) }); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatal(err)
	}
	slide := string(zipFiles(t, buf.Bytes())["ppt/slides/slide1.xml"])
	var runs []string
	for _, m := range regexp.MustCompile(`<a:t>([^<]*)</a:t>`).FindAllStringSubmatch(slide, -1) {
		runs = append(runs, m[1])
	}
	for _, word := range []string{"chapter", "callout", "quote", "section"} {
		found := false
		for _, r := range runs {
			if strings.Contains(r, word) {
				found = true
			}
		}
		if !found {
			t.Errorf("%q is not in one run of text: the word is broken (runs: %q)", word, runs)
		}
	}
}
