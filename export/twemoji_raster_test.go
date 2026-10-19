package export

import (
	"context"
	"image"
	"strings"
	"testing"

	"github.com/go-icons/twemoji"
	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
)

// TestRasterizeEmoji: an emoji in a deck reaches the image as its Twemoji
// graphic, through the data: URI, with no network. 😄 is drawn in #FFCC4D.
func TestRasterizeEmoji(t *testing.T) {
	b := build(t, "# Smile :smile: 😄\n")
	imgs, err := Rasterize(context.Background(), b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	yellow := 0
	for y := 0; y < imgs[0].Bounds().Dy(); y++ {
		for x := 0; x < imgs[0].Bounds().Dx(); x++ {
			if near(imgs[0].RGBAAt(x, y), 0xFF, 0xCC, 0x4D) {
				yellow++
			}
		}
	}
	// Two faces of about 1em (a heading of 2em): a few thousand pixels.
	if yellow < 1500 {
		t.Errorf("%d yellow pixels: the emoji were not drawn", yellow)
	}
	// Without emoji support the same deck has no such pixels.
	c, err := slides.Render([]byte("# Smile :smile: 😄\n"), slides.Config{NoEmoji: true}, deck.FrameBox)
	if err != nil {
		t.Fatal(err)
	}
	imgs, err = Rasterize(context.Background(), c, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for y := 0; y < imgs[0].Bounds().Dy(); y++ {
		for x := 0; x < imgs[0].Bounds().Dx(); x++ {
			if near(imgs[0].RGBAAt(x, y), 0xFF, 0xCC, 0x4D) {
				t.Fatalf("yellow at %d,%d with NoEmoji", x, y)
			}
		}
	}
}

// TestTwemojiRasterizes draws every embedded Twemoji graphic the way the
// engine's SVG images are drawn (oksvg over rasterx) and checks that each one
// parses and leaves ink. A graphic that does not is reported by name.
func TestTwemojiRasterizes(t *testing.T) {
	if testing.Short() {
		t.Skip("rasterizes 4,000 graphics")
	}
	const size = 72
	var blank, failed []string
	for _, stem := range twemoji.Stems() {
		svg, _ := twemoji.SVGByStem(stem)
		icon, err := oksvg.ReadIconStream(strings.NewReader(svg), oksvg.StrictErrorMode)
		if err != nil {
			failed = append(failed, stem+": "+err.Error())
			continue
		}
		icon.SetTarget(0, 0, size, size)
		img := image.NewRGBA(image.Rect(0, 0, size, size))
		scanner := rasterx.NewScannerGV(size, size, img, img.Bounds())
		icon.Draw(rasterx.NewDasher(size, size, scanner), 1)
		ink := 0
		for i := 3; i < len(img.Pix); i += 4 {
			if img.Pix[i] != 0 {
				ink++
			}
		}
		if ink < size*size/50 { // under 2% of the square inked
			blank = append(blank, stem)
		}
	}
	for _, f := range failed {
		t.Errorf("does not parse: %s", f)
	}
	for _, b := range blank {
		t.Errorf("draws (almost) nothing: %s", b)
	}
}
