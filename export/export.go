// Package export turns a built deck into images and documents without a
// browser: go-webengine lays each slide out and paints it, go-pdfkit writes
// the PDF. Marp CLI drives a headless Chrome for the same outputs.
//
// The slides are rendered in the box frame (deck.FrameBox), which needs
// neither <foreignObject> nor generated content from the layout engine.
package export

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"strings"

	"github.com/go-opentype/fonts/lato"
	"github.com/go-opentype/fonts/robotomono"
	"github.com/go-pdfkit/pdfkit"
	"github.com/go-webengine/engine"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
)

// Options configures the export.
type Options struct {
	// BaseURL resolves the deck's relative URLs (images, backgrounds):
	// typically "file:///path/to/deck/dir/".
	BaseURL string
	// NoFonts leaves out the fonts bundled for the built-in themes.
	NoFonts bool
	// JPEGQuality is the JPEG quality, 1 to 100; 0 means 85, marp-cli's.
	JPEGQuality int
	// Scale is the number of device pixels per CSS pixel: 2 renders a
	// 1280×720 slide as 2560×1440, text and vectors drawn at that density
	// rather than enlarged. 0 means 1 (marp-cli's --image-scale).
	Scale float64
	// FileRoot, when set, lets the slides load file: URLs from under this
	// directory and nowhere else; empty refuses every local file, as
	// marp-cli does without --allow-local-files.
	FileRoot string
}

// Rasterize renders every slide of b, which must have been rendered in
// deck.FrameBox, to an image of the slide's size times opts.Scale.
func Rasterize(ctx context.Context, b *slides.Build, opts Options) ([]*image.RGBA, error) {
	e := engine.New()
	e.DisableJS = true
	e.FileRoot = opts.FileRoot
	scale := opts.Scale
	if scale <= 0 {
		scale = 1
	}
	e.DeviceScale = scale
	w, h := int(b.Packed.Width), int(b.Packed.Height)
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("export: the theme gives the slides no size")
	}
	// The viewport is in CSS pixels; the image comes back in device pixels.
	view := image.Rect(0, 0, w, h)
	device := image.Rect(0, 0, int(float64(w)*scale+0.5), int(float64(h)*scale+0.5))
	fontCSS := ""
	if !opts.NoFonts {
		fontCSS = bundledFonts()
	}
	out := make([]*image.RGBA, len(b.Slides))
	for i, s := range b.Slides {
		img, _, err := e.RenderHTML(ctx, page(b.Packed.CSS, fontCSS, s.HTML), opts.BaseURL, view)
		if err != nil {
			return nil, fmt.Errorf("slide %d: %w", i+1, err)
		}
		// The canvas grows with the content; the slide is its top-left
		// corner.
		if img.Bounds() != device {
			c := image.NewRGBA(device)
			copyRect(c, img)
			img = c
		}
		out[i] = img
	}
	return out, nil
}

func copyRect(dst, src *image.RGBA) {
	r := dst.Bounds().Intersect(src.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		copy(dst.Pix[dst.PixOffset(r.Min.X, y):dst.PixOffset(r.Max.X, y)], src.Pix[src.PixOffset(r.Min.X, y):src.PixOffset(r.Max.X, y)])
	}
}

// page is the document one slide is rendered from. The theme comes first in
// a sheet of its own, since its @import rules must lead a sheet.
func page(themeCSS, fontCSS, slide string) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html><html><head><meta charset=\"utf-8\">")
	b.WriteString("<style>" + themeCSS + "</style>")
	b.WriteString("<style>html,body{margin:0;padding:0;background:#fff}" + fontCSS + "</style>")
	b.WriteString("</head><body><div class=\"marpit\">" + slide + "</div></body></html>")
	return b.String()
}

// bundledFonts declares the fonts Marp's built-in themes ask for, embedded,
// so that a render needs no network: gaia and uncover's Lato and Roboto
// Mono, from go-opentype/fonts (OFL-1.1). Each ships one regular instance,
// declared for every weight.
func bundledFonts() string {
	face := func(family string, ttf []byte) string {
		return "@font-face{font-family:\"" + family + "\";font-weight:100 900;font-style:normal;" +
			"src:url(data:font/ttf;base64," + base64.StdEncoding.EncodeToString(ttf) + ") format(\"truetype\")}"
	}
	return face("Lato", lato.TTF) + face("Roboto Mono", robotomono.TTF)
}

// PNG encodes an image as PNG.
func PNG(w io.Writer, img image.Image) error { return png.Encode(w, img) }

// JPEG encodes an image as JPEG.
func JPEG(w io.Writer, img image.Image, opts Options) error {
	q := opts.JPEGQuality
	if q <= 0 {
		q = 85
	}
	return jpeg.Encode(w, img, &jpeg.Options{Quality: q})
}

// PDF writes one page per slide. A page is the slide's size, a CSS pixel
// being 3/4 of a point, as Chrome prints Marp's @page size. The outline
// lists each slide and, under it, its headings (marp --pdf-outlines).
func PDF(w io.Writer, b *slides.Build, images []*image.RGBA) error {
	g := b.Deck.Global
	doc := pdfkit.New(pdfkit.Options{
		Title:    g.Get("title"),
		Author:   g.Get("author"),
		Subject:  g.Get("description"),
		Keywords: g.Get("keywords"),
		Producer: "go-slides",
	})
	size := pdfkit.NewPageSize(b.Packed.Width*0.75, b.Packed.Height*0.75)
	for i, img := range images {
		p := doc.AddPage(size)
		p.DrawImage(img, pdfkit.Rect{Width: size.Width, Height: size.Height})
		s := b.Slides[i].Slide
		doc.AddOutlineItem(outlineTitle(s.Headings, i), 0, i)
		for _, hd := range s.Headings {
			doc.AddOutlineItem(hd.Text, 1, i)
		}
	}
	var buf bytes.Buffer
	if err := doc.Write(&buf); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

func outlineTitle(hs []deck.Heading, i int) string {
	if len(hs) > 0 {
		return fmt.Sprintf("%d. %s", i+1, hs[0].Text)
	}
	return fmt.Sprintf("%d", i+1)
}
