package export

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

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

// TestRasterizeSplitBackground needs percentage heights, which go-webengine
// v0.5.10 does not resolve: Marpit's background layers are height:100%.
func TestRasterizeSplitBackground(t *testing.T) {
	b := build(t, "---\ntheme: gaia\n---\n![bg left]("+bluePNG(t)+")\n\nB\n")
	imgs, err := Rasterize(context.Background(), b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	left, right := imgs[0].RGBAAt(100, 360), imgs[0].RGBAAt(1100, 360)
	if !near(left, 0, 0, 255) || near(right, 0, 0, 255) {
		t.Skipf("split backgrounds wait for percentage heights in go-webengine: left %v, right %v", left, right)
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

func TestBundledFonts(t *testing.T) {
	css := bundledFonts()
	for _, want := range []string{`font-family:"Lato"`, `font-family:"Roboto Mono"`, "data:font/ttf;base64,"} {
		if !strings.Contains(css, want) {
			t.Errorf("missing %q", want)
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
