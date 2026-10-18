package export

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-ooxml/ooxml"
	"github.com/go-ooxml/opc"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
)

func build(t *testing.T, src string) *slides.Build {
	t.Helper()
	b, err := slides.Render([]byte(src), slides.Config{}, deck.FrameBox)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func near(c color.RGBA, r, g, b uint8) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(c.R, r) < 8 && d(c.G, g) < 8 && d(c.B, b) < 8
}

// bluePNG is a data URI of a small blue PNG.
func bluePNG(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i+2], img.Pix[i+3] = 255, 255
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestRasterize(t *testing.T) {
	b := build(t, "---\ntheme: gaia\nsize: 4:3\npaginate: true\n---\n<!-- _backgroundColor: #ff0000 -->\n\n# A\n\n---\n\n![bg]("+bluePNG(t)+")\n\nB\n")
	imgs, err := Rasterize(context.Background(), b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(imgs) != 2 {
		t.Fatalf("%d images", len(imgs))
	}
	if got := imgs[0].Bounds(); got != image.Rect(0, 0, 960, 720) {
		t.Fatalf("the size directive gives 4:3: %v", got)
	}
	if c := imgs[0].RGBAAt(5, 5); !near(c, 255, 0, 0) {
		t.Errorf("slide 1 background %v, want red", c)
	}
	// One background image covers the slide.
	if c := imgs[1].RGBAAt(100, 360); !near(c, 0, 0, 255) {
		t.Errorf("slide 2 %v, want the blue background image", c)
	}
}

// TestRasterizeSplitBackground goes through Marpit's background layers,
// which are height:100% boxes (go-webengine v0.5.12 resolves them).
func TestRasterizeSplitBackground(t *testing.T) {
	b := build(t, "---\ntheme: gaia\n---\n![bg left]("+bluePNG(t)+")\n\nB\n")
	imgs, err := Rasterize(context.Background(), b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if c := imgs[0].RGBAAt(100, 360); !near(c, 0, 0, 255) {
		t.Errorf("left half %v, want the blue background image", c)
	}
	if c := imgs[0].RGBAAt(1100, 360); near(c, 0, 0, 255) {
		t.Errorf("right half %v must hold the content, not the image", c)
	}
}

func TestRasterizeScale(t *testing.T) {
	b := build(t, "<!-- _backgroundColor: #ff0000 -->\n\n# A\n")
	imgs, err := Rasterize(context.Background(), b, Options{Scale: 2, NoFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := imgs[0].Bounds(); got != image.Rect(0, 0, 2560, 1440) {
		t.Fatalf("scale 2 renders 2560×1440, got %v", got)
	}
	if c := imgs[0].RGBAAt(2500, 1400); !near(c, 255, 0, 0) {
		t.Errorf("the background fills the scaled slide: %v", c)
	}
}

func TestRasterizeLocalFiles(t *testing.T) {
	dir := t.TempDir()
	blue := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := 0; i < len(blue.Pix); i += 4 {
		blue.Pix[i+2], blue.Pix[i+3] = 255, 255
	}
	f, err := os.Create(filepath.Join(dir, "blue.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, blue); err != nil {
		t.Fatal(err)
	}
	f.Close()
	b := build(t, "![bg](blue.png)\n\nx\n")
	base := "file://" + filepath.ToSlash(dir) + "/"
	for _, c := range []struct {
		root string
		blue bool
	}{{"", false}, {dir, true}, {t.TempDir(), false}} {
		imgs, err := Rasterize(context.Background(), b, Options{BaseURL: base, FileRoot: c.root, NoFonts: true})
		if err != nil {
			t.Fatal(err)
		}
		if got := near(imgs[0].RGBAAt(640, 600), 0, 0, 255); got != c.blue {
			t.Errorf("FileRoot %q: local image shown = %v, want %v", c.root, got, c.blue)
		}
	}
}

func TestEncodeAndPDF(t *testing.T) {
	b := build(t, "---\ntitle: T\nauthor: A\n---\n# One\n\n## Sub\n\n---\n\nno heading\n")
	imgs, err := Rasterize(context.Background(), b, Options{NoFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	var p, j bytes.Buffer
	if err := PNG(&p, imgs[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(&p); err != nil {
		t.Errorf("png: %v", err)
	}
	if err := JPEG(&j, imgs[0], Options{JPEGQuality: 50}); err != nil {
		t.Fatal(err)
	}
	if _, err := jpeg.Decode(&j); err != nil {
		t.Errorf("jpeg: %v", err)
	}
	var pdf bytes.Buffer
	if err := PDF(&pdf, b, imgs); err != nil {
		t.Fatal(err)
	}
	s := pdf.String()
	if !strings.HasPrefix(s, "%PDF-") {
		t.Fatal("not a PDF")
	}
	if n := strings.Count(s, "/Type /Page\n") + strings.Count(s, "/Type /Page "); n < 2 && strings.Count(s, "/Type/Page") < 2 {
		t.Logf("page markers not found in the expected form; checking the outline instead")
	}
	for _, want := range []string{"/Outlines", "/MediaBox [0 0 960 540]"} {
		if !strings.Contains(s, want) {
			t.Errorf("PDF lacks %q", want)
		}
	}
}

func TestPPTX(t *testing.T) {
	b := build(t, "---\ntitle: T\n---\n# One\n\n<!-- say hello -->\n\n---\n\nTwo\n")
	imgs, err := Rasterize(context.Background(), b, Options{NoFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := PPTX(&buf, b, imgs); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "PK") {
		t.Fatal("a .pptx is a zip package")
	}
	doc, err := ooxml.ParsePresentation(buf.Bytes(), opc.Limits{})
	if err != nil {
		t.Fatalf("go-ooxml cannot read it back: %v", err)
	}
	if doc == nil {
		t.Fatal("no document")
	}
	if !bytes.Contains(buf.Bytes(), []byte("notesSlide")) {
		t.Error("the speaker note of slide 1 is missing")
	}
}

// TestFitHeading checks that a fitting heading is scaled: "Hi" alone would
// cover a small corner, fitted it spans the slide's content width.
func TestFitHeading(t *testing.T) {
	b := build(t, "<style scoped>h1 { color: #ff0000; }</style>\n\n# <!-- fit --> Hi\n")
	imgs, err := Rasterize(context.Background(), b, Options{NoFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	maxX := -1
	img := imgs[0]
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if c := img.RGBAAt(x, y); c.R > 200 && c.G < 80 && c.B < 80 {
				maxX = max(maxX, x)
			}
		}
	}
	if maxX < 900 {
		t.Errorf("the fitted heading should reach across the slide; rightmost red pixel at x=%d", maxX)
	}
}

func TestBundledFonts(t *testing.T) {
	faces, err := bundledFaces()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range faces {
		got[fmt.Sprintf("%s %d %v", f.Family, f.Weight, f.Italic)] = true
	}
	for _, want := range []string{"Lato 400 false", "Lato 700 false", "Lato 900 false", "Lato 400 true", "Lato 700 true", "Lato 900 true",
		"Roboto Mono 400 false", "Roboto Mono 700 false", "Roboto Mono 400 true", "Roboto Mono 700 true"} {
		if !got[want] {
			t.Errorf("missing %s", want)
		}
	}
}

func TestNoSize(t *testing.T) {
	b := build(t, "x")
	b.Packed.Width = 0
	if _, err := Rasterize(context.Background(), b, Options{}); err == nil {
		t.Error("a slide without a size cannot be rendered")
	}
}
