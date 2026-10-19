package export

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/go-odf/odf"
	"github.com/go-ooxml/ooxml"
	"github.com/go-ooxml/opc"
	"github.com/go-richdoc/richdoc"
	"github.com/go-webengine/engine"
)

// editableDeck has text with a link, a heading in gaia's Lato, the same
// background picture on two slides, and notes.
func editableDeck(t *testing.T) string {
	bg := bluePNG(t)
	return "---\ntheme: gaia\n---\n# Editable *deck*\n\nSee [the site](https://example.com/x).\n\n![bg right](" + bg + ")\n\n<!-- say this -->\n\n---\n\n![bg right](" + bg + ")\n\nSecond slide\n"
}

func zipFiles(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		out[f.Name], _ = io.ReadAll(rc)
		rc.Close()
	}
	return out
}

func TestEditablePPTX(t *testing.T) {
	b := build(t, editableDeck(t))
	w := NewPPTX(b)
	if err := EachLayout(context.Background(), b, Options{}, func(_ int, l *engine.PageLayout) error { return w.AddLayout(l) }); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatal(err)
	}
	doc, err := ooxml.ParsePresentation(buf.Bytes(), opc.Limits{})
	if err != nil {
		t.Fatalf("go-ooxml cannot read it back: %v", err)
	}
	text := richdoc.PlainText(doc)
	for _, want := range []string{"Editable", "deck", "the site", "Second slide"} {
		if !strings.Contains(text, want) {
			t.Errorf("the deck's text lacks %q: %q", want, text)
		}
	}
	files := zipFiles(t, buf.Bytes())
	media, fonts := 0, 0
	for name := range files {
		switch {
		case strings.HasPrefix(name, "ppt/media/"):
			media++
		case strings.HasPrefix(name, "ppt/fonts/"):
			fonts++
		}
	}
	if media != 1 {
		t.Errorf("%d pictures, want the one background shown on both slides", media)
	}
	if fonts == 0 || !strings.Contains(string(files["ppt/presentation.xml"]), `typeface="Lato"`) {
		t.Error("gaia's Lato is not embedded")
	}
	s1 := string(files["ppt/slides/slide1.xml"])
	if !strings.Contains(s1, `txBox="1"`) || !strings.Contains(s1, `<a:hlinkClick`) {
		t.Error("slide 1 should hold text boxes and a link")
	}
	if !strings.Contains(string(files["ppt/slides/_rels/slide1.xml.rels"]), "https://example.com/x") {
		t.Error("the link's target is not related")
	}
	if !strings.Contains(string(files["ppt/notesSlides/notesSlide1.xml"]), "say this") {
		t.Error("the notes are lost")
	}
}

func TestEditableODP(t *testing.T) {
	b := build(t, editableDeck(t))
	w := NewODP(b)
	if err := EachLayout(context.Background(), b, Options{}, func(_ int, l *engine.PageLayout) error { return w.AddLayout(l) }); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatal(err)
	}
	doc, err := odf.Parse(buf.Bytes())
	if err != nil {
		t.Fatalf("go-odf cannot read it back: %v", err)
	}
	if text := richdoc.PlainText(doc); !strings.Contains(text, "the site") || !strings.Contains(text, "Second slide") {
		t.Errorf("the deck's text: %q", text)
	}
	files := zipFiles(t, buf.Bytes())
	c := string(files["content.xml"])
	for _, want := range []string{`<draw:text-box>`, `xlink:href="https://example.com/x"`, `svg:font-family="Lato"`, "say this"} {
		if !strings.Contains(c, want) {
			t.Errorf("content.xml lacks %s", want)
		}
	}
}

// TestBlankSlideStillWrites: a slide that draws nothing still makes a slide.
func TestBlankSlideStillWrites(t *testing.T) {
	b := build(t, "<!-- _backgroundColor: transparent -->\n")
	for _, w := range []interface {
		AddLayout(*engine.PageLayout) error
		Write(io.Writer) error
	}{NewPPTX(b), NewODP(b)} {
		if err := EachLayout(context.Background(), b, Options{NoFonts: true}, func(_ int, l *engine.PageLayout) error { return w.AddLayout(l) }); err != nil {
			t.Fatal(err)
		}
		if err := w.Write(io.Discard); err != nil {
			t.Errorf("%T: %v", w, err)
		}
	}
}
