package slides

import (
	"strings"
	"testing"

	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/theme"
)

func TestHTML(t *testing.T) {
	src := "---\ntheme: gaia\ntitle: T <x>\nauthor: A\nlang: fr\npaginate: true\n---\n# One\n\n<style scoped>h1{color:red}</style>\n\n---\n\n![bg left](a.png)\n\nTwo\n"
	page, b, err := HTML([]byte(src), Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<html lang="fr">`, "<title>T &lt;x&gt;</title>", `<meta name="author" content="A">`,
		`div.marpit > svg > foreignObject > section[data-marpit-scope-s1] h1`,
		`data-marpit-advanced-background="background"`, "<script>",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %q", want)
		}
	}
	if b.Theme.Name != "gaia" || len(b.Slides) != 2 || len(b.Warnings) != 0 {
		t.Errorf("theme %s, %d slides, warnings %v", b.Theme.Name, len(b.Slides), b.Warnings)
	}
	bare, _, err := HTML([]byte("x"), Config{Bare: true})
	if err != nil || strings.Contains(bare, "<script>") {
		t.Errorf("bare must not carry the script: %v", err)
	}
}

func TestUnknownThemeWarns(t *testing.T) {
	_, b, err := HTML([]byte("<!-- theme: nope -->\n"), Config{Themes: theme.NewSet()})
	if err != nil || len(b.Warnings) != 1 || b.Theme.Name != "default" {
		t.Fatalf("%v %v", err, b.Warnings)
	}
}

func TestBoxFrame(t *testing.T) {
	b, err := Render([]byte("---\npaginate: true\n---\n![bg right:40%](a.png)\n\nx\n"), Config{}, deck.FrameBox)
	if err != nil {
		t.Fatal(err)
	}
	h := b.Slides[0].HTML
	for _, want := range []string{`class="marpit-svg"`, `class="marpit-fo"`, `width:60%`, `<span data-slides-pseudo="after">1</span>`} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in %s", want, h)
		}
	}
	if strings.Count(h, "data-slides-pseudo") != 1 {
		t.Error("the page number belongs to the pseudo layer only")
	}
	if b.Packed.After != "attr(data-marpit-pagination)" {
		t.Errorf("after %q", b.Packed.After)
	}
	if _, err := Render([]byte("<!-- kind: nope -->"), Config{}, deck.FrameSVG); err == nil {
		t.Error("parse errors surface")
	}
}
