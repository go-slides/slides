package export

import (
	"bytes"
	"context"
	"image"
	"testing"
)

// The scheduler itself (order, bound, first error, no leaked goroutine) is
// tested where it lives, go-filesystems/inflight's Ordered; what is tested
// here is that the slides come out of it identical at any worker count.

// TestEachMatchesSerial renders the same deck with one worker and with
// several: the images must be identical, which also exercises one Engine
// rendering concurrently (run with -race).
func TestEachMatchesSerial(t *testing.T) {
	b := build(t, "---\ntheme: gaia\n---\n# One\n\n---\n\n## Two\n\n- a\n- b\n\n---\n\n![bg]("+bluePNG(t)+")\n\nthree\n\n---\n\n`four`\n")
	serial, err := Rasterize(context.Background(), b, Options{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	parallel, err := Rasterize(context.Background(), b, Options{Workers: 4})
	if err != nil {
		t.Fatal(err)
	}
	for i := range serial {
		if !bytes.Equal(serial[i].Pix, parallel[i].Pix) {
			t.Errorf("slide %d differs between 1 and 4 workers", i+1)
		}
	}
}

func TestEachEncoded(t *testing.T) {
	b := build(t, "# One\n\n---\n\n## Two\n")
	imgs, err := Rasterize(context.Background(), b, Options{NoFonts: true})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	if err := EachEncoded(context.Background(), b, Options{NoFonts: true, Workers: 2}, "png", func(i int, data []byte) error {
		var want bytes.Buffer
		if err := PNG(&want, imgs[i]); err != nil {
			return err
		}
		if !bytes.Equal(data, want.Bytes()) {
			t.Errorf("slide %d: the PNG differs from encoding Each's image", i+1)
		}
		n++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("%d slides", n)
	}
	if err := EachEncoded(context.Background(), b, Options{}, "gif", func(int, []byte) error { return nil }); err == nil {
		t.Error("gif is not a format")
	}
}

// TestBundledFacesApply: gaia's text is set in the bundled Lato and Roboto
// Mono — the slide differs from the one rendered without them — and the
// default theme, which names neither, is not affected by them.
func TestBundledFacesApply(t *testing.T) {
	for _, c := range []struct {
		src    string
		differ bool
	}{
		{"---\ntheme: gaia\n---\n# Heading\n\ntext `code`\n", true},
		{"# Heading\n\ntext\n", false},
	} {
		b := build(t, c.src)
		with, err := Rasterize(context.Background(), b, Options{})
		if err != nil {
			t.Fatal(err)
		}
		without, err := Rasterize(context.Background(), b, Options{NoFonts: true})
		if err != nil {
			t.Fatal(err)
		}
		if got := !bytes.Equal(with[0].Pix, without[0].Pix); got != c.differ {
			t.Errorf("%q: the bundled fonts change the slide = %v, want %v", c.src, got, c.differ)
		}
	}
}

func TestImageOnlyFirst(t *testing.T) {
	b := build(t, "# A\n\n---\n\n# B\n")
	first := *b
	first.Slides = b.Slides[:1]
	n := 0
	if err := Each(context.Background(), &first, Options{NoFonts: true}, func(int, *image.RGBA) error { n++; return nil }); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("%d images", n)
	}
}
