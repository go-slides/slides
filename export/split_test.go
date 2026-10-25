package export

import (
	"bytes"
	"context"
	"io"
	"math"
	"regexp"
	"strconv"
	"testing"

	"github.com/go-ooxml/ooxml"
	"github.com/go-ooxml/opc"
	"github.com/go-webengine/engine"
)

// Marpit's split backgrounds, `![bg left]`, `![bg right:40%]` and the rest
// (https://marpit.marp.app/image-syntax?id=split-backgrounds): the image fills a
// band of the slide, the content the other side. Marpit lays the band out with
// a flex container whose offset is `margin-left: calc(100% - split)` for right;
// the engine dropped that calc() and drew every right split on the left
// (go-slides#53). These tests look at where the image's centre is, in each
// export: the PNG's pixels, the vector PDF's image matrix, and the PPTX's
// picture.

// splits are the decks' forms and the centre of the band the image fills, in
// parts of the slide's width.
var splits = []struct {
	name, directive string
	centre          float64
}{
	{"left", "bg left", 0.25},
	{"left 30%", "bg left:30%", 0.15},
	{"left 40%", "bg left:40%", 0.20},
	{"right", "bg right", 0.75},
	{"right 30%", "bg right:30%", 0.85},
	{"right 40%", "bg right:40%", 0.80},
	{"right 60%", "bg right:60%", 0.70},
}

// splitDeck is a deck with a single slide: a split background and a line of text.
func splitDeck(t *testing.T, directive string) string {
	return "---\ntheme: default\n---\n![" + directive + "](" + bluePNG(t) + ")\n\n# Title\n"
}

// within reports whether got is within tol of want.
func within(got, want, tol float64) bool { return math.Abs(got-want) <= tol }

func TestSplitBackgroundPNG(t *testing.T) {
	for _, s := range splits {
		t.Run(s.name, func(t *testing.T) {
			b := build(t, splitDeck(t, s.directive))
			imgs, err := Rasterize(context.Background(), b, Options{})
			if err != nil {
				t.Fatal(err)
			}
			w := float64(imgs[0].Bounds().Dx())
			first, last := -1, -1
			y := imgs[0].Bounds().Dy() - 40 // below the text
			for x := 0; x < imgs[0].Bounds().Dx(); x++ {
				if c := imgs[0].RGBAAt(x, y); near(c, 0, 0, 255) {
					if first < 0 {
						first = x
					}
					last = x
				}
			}
			if first < 0 {
				t.Fatal("no blue image in the slide")
			}
			if centre := float64(first+last) / 2 / w; !within(centre, s.centre, 0.01) {
				t.Errorf("the image's centre is at %.3f of the width (columns %d..%d), want %.3f", centre, first, last, s.centre)
			}
		})
	}
}

// pdfImageRe finds `a b c d e f cm /Name Do`: the matrix an image is drawn with.
var pdfImageRe = regexp.MustCompile(`(-?[\d.]+) (-?[\d.]+) (-?[\d.]+) (-?[\d.]+) (-?[\d.]+) (-?[\d.]+) cm\s*/\w+ Do`)

func TestSplitBackgroundPDF(t *testing.T) {
	for _, s := range splits {
		t.Run(s.name, func(t *testing.T) {
			b := build(t, splitDeck(t, s.directive))
			var buf bytes.Buffer
			if err := VectorPDF(context.Background(), &buf, b, Options{NoFonts: true}); err != nil {
				t.Fatal(err)
			}
			p := readPDF(t, buf.Bytes()).Page(1)
			content, err := io.ReadAll(p.V.Key("Contents").Reader())
			if err != nil {
				// Contents may be an array of streams
				cs := p.V.Key("Contents")
				for i := 0; i < cs.Len(); i++ {
					data, _ := io.ReadAll(cs.Index(i).Reader())
					content = append(content, data...)
				}
			}
			m := pdfImageRe.FindSubmatch(content)
			if m == nil {
				t.Fatalf("no image drawn in the page's content: %.300s", content)
			}
			num := func(i int) float64 { f, _ := strconv.ParseFloat(string(m[i]), 64); return f }
			box := p.V.Key("MediaBox")
			width := box.Index(2).Float64() - box.Index(0).Float64()
			// the image is drawn from x = e over a width of a, then clipped to its band
			// (a clip rectangle before it): the band's centre is what the clip says
			clip := regexp.MustCompile(`(-?[\d.]+) (-?[\d.]+) (-?[\d.]+) (-?[\d.]+) re\s*W n`).FindSubmatch(content)
			lo, hi := num(5), num(5)+num(1)
			if clip != nil {
				cx, _ := strconv.ParseFloat(string(clip[1]), 64)
				cw, _ := strconv.ParseFloat(string(clip[3]), 64)
				lo, hi = cx, cx+cw
			}
			if centre := (lo + hi) / 2 / width; !within(centre, s.centre, 0.01) {
				t.Errorf("the image's centre is at %.3f of the page's width (x %.1f..%.1f of %.1f), want %.3f", centre, lo, hi, width, s.centre)
			}
		})
	}
}

var offRe = regexp.MustCompile(`<p:pic>.*?<a:off x="(-?\d+)" y="-?\d+"/><a:ext cx="(\d+)" cy="\d+"/>`)

func TestSplitBackgroundPPTX(t *testing.T) {
	for _, s := range splits {
		t.Run(s.name, func(t *testing.T) {
			b := build(t, splitDeck(t, s.directive))
			w := NewPPTX(b)
			if err := EachLayout(context.Background(), b, Options{NoFonts: true}, func(_ int, l *engine.PageLayout) error { return w.AddLayout(l) }); err != nil {
				t.Fatal(err)
			}
			var buf bytes.Buffer
			if err := w.Write(&buf); err != nil {
				t.Fatal(err)
			}
			if _, err := ooxml.ParsePresentation(buf.Bytes(), opc.Limits{}); err != nil {
				t.Fatal(err)
			}
			files := zipFiles(t, buf.Bytes())
			slide := string(files["ppt/slides/slide1.xml"])
			m := regexp.MustCompile(`(?s)` + offRe.String()).FindStringSubmatch(slide)
			if m == nil {
				t.Fatalf("no picture on the slide: %.400s", slide)
			}
			x, _ := strconv.ParseFloat(m[1], 64)
			cx, _ := strconv.ParseFloat(m[2], 64)
			pw := regexp.MustCompile(`<p:sldSz cx="(\d+)"`).FindStringSubmatch(string(files["ppt/presentation.xml"]))
			if pw == nil {
				t.Fatal("no slide size")
			}
			width, _ := strconv.ParseFloat(pw[1], 64)
			if centre := (x + cx/2) / width; !within(centre, s.centre, 0.01) {
				t.Errorf("the picture's centre is at %.3f of the slide's width (x %.0f, width %.0f of %.0f), want %.3f", centre, x, cx, width, s.centre)
			}
		})
	}
}

// The text goes to the other side: a right split leaves the content on the
// left, in the band the image does not take.
func TestSplitBackgroundContentSide(t *testing.T) {
	for name, side := range map[string]string{"bg right:40%": "left", "bg left:40%": "right"} {
		// the band the content has is 60% of the slide: from 0 to 768 px for a
		// right split, from 512 px to the edge for a left one
		b := build(t, splitDeck(t, name))
		imgs, err := Rasterize(context.Background(), b, Options{})
		if err != nil {
			t.Fatal(err)
		}
		w := imgs[0].Bounds().Dx()
		dark := func(x0, x1 int) int {
			n := 0
			for y := 0; y < imgs[0].Bounds().Dy(); y++ {
				for x := x0; x < x1; x++ {
					if c := imgs[0].RGBAAt(x, y); int(c.R)+int(c.G)+int(c.B) < 3*100 && !near(c, 0, 0, 255) {
						n++ // the heading's ink: dark, and not the blue image
					}
				}
			}
			return n
		}
		split := w * 2 / 5
		if side == "left" {
			split = w * 3 / 5
		}
		l, r := dark(0, split), dark(split, w)
		if side == "left" && !(l > 0 && r == 0) || side == "right" && !(r > 0 && l == 0) {
			t.Errorf("%s: the heading's ink is %d px left of %d and %d right of it, want it on the %s (the band the image leaves)", name, l, split, r, side)
		}
	}
}
