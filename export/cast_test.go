package export

import (
	"bytes"
	"context"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/go-webengine/engine"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/doc"
	"github.com/go-slides/slides/vfs"
)

// A recording with every kind of colour and attribute: the 16 colours, a 256
// colour, a direct colour, bold, italic, underline and inverse.
const castFile = `{"version": 2, "width": 40, "height": 6}
[0.1, "o", "$ ls --color\r\n"]
[0.2, "o", "\u001b[31mred\u001b[0m \u001b[1;32mbold green\u001b[0m \u001b[3mitalic\u001b[0m \u001b[4munder\u001b[0m\r\n"]
[0.3, "o", "\u001b[7m inverse \u001b[0m \u001b[38;5;208morange\u001b[0m \u001b[38;2;10;200;255mtruecolor\u001b[0m\r\n"]
[0.4, "o", "\u001b[44;37m blue bg \u001b[0m done\r\n"]
[9.0, "o", "LATE-FRAME"]
`

func castDoc(t *testing.T, deckSrc string, frame deck.Frame) *doc.Doc {
	t.Helper()
	cfg := slides.Config{Parse: deck.Options{FS: vfs.NewMap(map[string][]byte{"demo.cast": []byte(castFile)})}}
	b, err := slides.Render([]byte(deckSrc), cfg, frame)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Warnings) > 0 {
		t.Fatalf("warnings: %v", b.Warnings)
	}
	return b.Doc()
}

var (
	bfchar  = regexp.MustCompile(`<([0-9A-Fa-f]{4})>\s*<([0-9A-Fa-f]{4})>`)
	bfrange = regexp.MustCompile(`<([0-9A-Fa-f]{4})>\s*<([0-9A-Fa-f]{4})>\s*<([0-9A-Fa-f]{4})>`)
)

// pdfText is the text of the pages, read through each font's /ToUnicode map:
// the text a reader copies out of the PDF.
func pdfText(t *testing.T, data []byte) string {
	t.Helper()
	r := readPDF(t, data)
	var b strings.Builder
	var pend []byte
	for i := 1; i <= r.NumPage(); i++ {
		p := r.Page(i)
		maps := map[string]map[rune]rune{}
		fonts := p.V.Key("Resources").Key("Font")
		for _, k := range fonts.Keys() {
			raw, err := io.ReadAll(fonts.Key(k).Key("ToUnicode").Reader())
			if err != nil {
				t.Fatal(err)
			}
			m := map[rune]rune{}
			hex := func(s string) rune { v, _ := strconv.ParseUint(s, 16, 32); return rune(v) }
			s := string(raw)
			for _, section := range strings.Split(s, "beginbfchar")[1:] {
				section, _, _ = strings.Cut(section, "endbfchar")
				for _, x := range bfchar.FindAllStringSubmatch(section, -1) {
					m[hex(x[1])] = hex(x[2])
				}
			}
			for _, section := range strings.Split(s, "beginbfrange")[1:] {
				section, _, _ = strings.Cut(section, "endbfrange")
				for _, x := range bfrange.FindAllStringSubmatch(section, -1) {
					for c := hex(x[1]); c <= hex(x[2]); c++ {
						m[c] = hex(x[3]) + c - hex(x[1])
					}
				}
			}
			maps[k] = m
		}
		for _, tx := range p.Content().Text {
			m := maps[tx.Font]
			if m == nil {
				m = maps[strings.TrimPrefix(tx.Font, "/")]
			}
			if m == nil && len(maps) == 1 {
				for _, only := range maps {
					m = only
				}
			}
			// the codes are two bytes each, and a text item may hold half of one
			pend = append(pend, tx.S...)
			for len(pend) >= 2 {
				b.WriteRune(m[rune(pend[0])<<8|rune(pend[1])])
				pend = pend[2:]
			}
		}
	}
	return b.String()
}

// The PDF holds the terminal as text, at the time asked for, in the font of the
// theme's code, with no bitmap.
func TestCastPDFIsVectorText(t *testing.T) {
	for name, src := range map[string]string{
		"default (the last frame)": "![cast](demo.cast)\n",
		"#t=0.5":                   "![cast](demo.cast#t=0.5)\n",
	} {
		d := castDoc(t, src, deck.FrameBox)
		var buf bytes.Buffer
		if err := VectorPDF(context.Background(), &buf, d, Options{NoFonts: true}); err != nil {
			t.Fatal(name, err)
		}
		txt := pdfText(t, buf.Bytes())
		for _, want := range []string{"ls --color", "bold green", "truecolor"} {
			if !strings.Contains(strings.ReplaceAll(txt, " ", ""), strings.ReplaceAll(want, " ", "")) {
				t.Errorf("%s: the page text lacks %q: %q", name, want, txt)
			}
		}
		if got := strings.Contains(txt, "LATE-FRAME"); got != (name == "default (the last frame)") {
			t.Errorf("%s: the last frame is shown = %v", name, got)
		}
		if strings.Contains(buf.String(), "/Subtype /Image") || strings.Contains(buf.String(), "/Subtype/Image") {
			t.Errorf("%s: the terminal is a bitmap", name)
		}
	}
}


func castSlideXML(t *testing.T, deckSrc string) (string, map[string][]byte) {
	t.Helper()
	d := castDoc(t, deckSrc, deck.FrameBox)
	w := NewPPTX(d)
	if err := EachLayout(context.Background(), d, Options{}, func(_ int, l *engine.PageLayout) error { return w.AddLayout(l) }); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatal(err)
	}
	files := zipFiles(t, buf.Bytes())
	return string(files["ppt/slides/slide1.xml"]), files
}

// The PPTX holds the terminal as runs of text with the recording's colours,
// the backgrounds as shapes, no bitmap.
func TestCastPPTXHoldsStyledRuns(t *testing.T) {
	slide, files := castSlideXML(t, "![cast](demo.cast#t=0.5)\n")
	for _, want := range []string{"red", "bold green", "truecolor", `val="CD0000"`, `val="0AC8FF"`, `val="FF8700"`, `val="3B78FF"`, `val="1E1E1E"`} {
		if !strings.Contains(strings.ToUpper(slide), strings.ToUpper(want)) {
			t.Errorf("slide1.xml lacks %q", want)
		}
	}
	for name := range files {
		if strings.HasPrefix(name, "ppt/media/") {
			t.Errorf("the terminal is a bitmap: %s", name)
		}
	}
}

// Bold and italic are drawn with the theme's monospace font's own faces: where
// it has them (gaia's Roboto Mono). The engine's fallback monospace face has
// only a regular one.
func TestCastPPTXBoldAndItalicWhereTheFontHasThem(t *testing.T) {
	slide, _ := castSlideXML(t, "---\ntheme: gaia\n---\n\n![cast](demo.cast#t=0.5)\n")
	for _, a := range []string{` b="1"`, ` i="1"`} {
		if !strings.Contains(slide, a) {
			t.Errorf("no run with %s", a)
		}
	}
}

// What the browser player carries is not in what the exports are given.
func TestCastFramesAreForTheBrowserOnly(t *testing.T) {
	svg := castDoc(t, "![cast](demo.cast)\n", deck.FrameSVG).Slides[0].HTML
	box := castDoc(t, "![cast](demo.cast)\n", deck.FrameBox).Slides[0].HTML
	if !strings.Contains(svg, "slides-cast-data") || strings.Contains(box, "slides-cast-data") {
		t.Errorf("frames in the SVG frame %v, in the box frame %v", strings.Contains(svg, "slides-cast-data"), strings.Contains(box, "slides-cast-data"))
	}
	// and a document built for the browser still exports, the frames ignored
	d := castDoc(t, "![cast](demo.cast)\n", deck.FrameSVG)
	var buf bytes.Buffer
	if err := VectorPDF(context.Background(), &buf, d, Options{NoFonts: true}); err != nil {
		t.Fatal(err)
	}
	if txt := pdfText(t, buf.Bytes()); strings.Contains(txt, "\"f\"") || strings.Contains(txt, "slides-cast") {
		t.Errorf("the frames' data is in the page: %q", txt)
	}
}

// The ODP holds the same runs: text in the recording's colours, no picture.
func TestCastODPHoldsStyledRuns(t *testing.T) {
	d := castDoc(t, "---\ntheme: gaia\n---\n\n![cast](demo.cast#t=0.5)\n", deck.FrameBox)
	w := NewODP(d)
	if err := EachLayout(context.Background(), d, Options{}, func(_ int, l *engine.PageLayout) error { return w.AddLayout(l) }); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatal(err)
	}
	files := zipFiles(t, buf.Bytes())
	content := strings.ToLower(string(files["content.xml"]))
	for _, want := range []string{"bold green", "truecolor", `fo:color="#cd0000"`, `fo:color="#0ac8ff"`, `fo:font-weight="bold"`, `fo:font-style="italic"`} {
		if !strings.Contains(content, want) {
			t.Errorf("content.xml lacks %q", want)
		}
	}
	// (gaia draws its own background picture: the default theme has none)
	d = castDoc(t, "![cast](demo.cast#t=0.5)\n", deck.FrameBox)
	w = NewODP(d)
	if err := EachLayout(context.Background(), d, Options{}, func(_ int, l *engine.PageLayout) error { return w.AddLayout(l) }); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := w.Write(&buf); err != nil {
		t.Fatal(err)
	}
	for name := range zipFiles(t, buf.Bytes()) {
		if strings.HasPrefix(name, "Pictures/") {
			t.Errorf("the terminal is a bitmap: %s", name)
		}
	}
}
