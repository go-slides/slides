// Package export turns a built deck into images and documents without a
// browser: go-webengine lays each slide out and paints it, go-pdfkit writes
// the PDF. Marp CLI drives a headless Chrome for the same outputs.
//
// The slides are rendered in the box frame (deck.FrameBox), which needs
// neither <foreignObject> nor generated content from the layout engine.
package export

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/go-opentype/fonts/lato"
	"github.com/go-opentype/fonts/robotomono"
	"github.com/go-pdfkit/pdfkit"
	"github.com/go-webengine/engine"
	"github.com/go-webengine/engine/paint"
	"github.com/go-webengine/engine/pdfcanvas"

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
	// Workers is how many slides are rendered at once: 0 means
	// DefaultWorkers(). It also bounds memory, since Each holds at most
	// this many slide images at a time.
	Workers int
}

// DefaultWorkers is how many slides Each renders at once by default: the
// number of CPUs Go may use, at most 4. The slides are independent, so more
// workers render a deck faster; each holds one slide's layout and image, so
// more workers also hold more memory. Measured on 10 slides (BENCHMARKS.md):
// 1 → 2 workers saves 47 % of the time, 2 → 4 another 27 %, 4 → 8 only 21 %
// for 15 % more heap; 4 keeps the heap near one worker's.
func DefaultWorkers() int { return min(runtime.GOMAXPROCS(0), 4) }

// Rasterize renders every slide of b, which must have been rendered in
// deck.FrameBox, to an image of the slide's size times opts.Scale. It holds
// every image at once; Each hands them over one at a time.
func Rasterize(ctx context.Context, b *slides.Build, opts Options) ([]*image.RGBA, error) {
	out := make([]*image.RGBA, len(b.Slides))
	err := Each(ctx, b, opts, func(i int, img *image.RGBA) error {
		out[i] = img
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Each renders every slide of b, which must have been rendered in
// deck.FrameBox, and calls emit with each image in slide order, from one
// goroutine. Up to opts.Workers slides are rendered concurrently, and no
// more images than that exist at once: a slide starts only when the image
// of the one that many places before it has been emitted. emit may keep
// the image. An error from a slide or from emit stops the render and is
// returned.
func Each(ctx context.Context, b *slides.Build, opts Options, emit func(i int, img *image.RGBA) error) error {
	return each(ctx, b, opts, func(img *image.RGBA) (*image.RGBA, error) { return img, nil }, emit)
}

// EachEncoded is Each with every image encoded, as "png" or "jpeg", by the
// worker that rendered it, so that encoding runs in parallel too and only
// the encoded bytes wait for their turn.
func EachEncoded(ctx context.Context, b *slides.Build, opts Options, format string, emit func(i int, data []byte) error) error {
	var enc func(io.Writer, image.Image) error
	switch format {
	case "png":
		enc = PNG
	case "jpeg":
		enc = func(w io.Writer, img image.Image) error { return JPEG(w, img, opts) }
	default:
		return fmt.Errorf("export: unknown image format %q", format)
	}
	return each(ctx, b, opts, func(img *image.RGBA) ([]byte, error) {
		var buf bytes.Buffer
		err := enc(&buf, img)
		return buf.Bytes(), err
	}, emit)
}

func each[T any](ctx context.Context, b *slides.Build, opts Options, convert func(*image.RGBA) (T, error), emit func(int, T) error) error {
	scale := opts.Scale
	if scale <= 0 {
		scale = 1
	}
	w, h := int(b.Packed.Width), int(b.Packed.Height)
	// The viewport is in CSS pixels; the image comes back in device pixels.
	device := image.Rect(0, 0, int(float64(w)*scale+0.5), int(float64(h)*scale+0.5))
	return eachLayout(ctx, b, opts, func(l *engine.PageLayout) (T, error) {
		img := l.Paint()
		// The canvas grows with the content; the slide is its top-left
		// corner.
		if img.Bounds() != device {
			c := image.NewRGBA(device)
			copyRect(c, img)
			img = c
		}
		return convert(img)
	}, emit)
}

// EachLayout lays every slide of b out, as Each does, and calls emit with
// each layout in slide order, from one goroutine, unpainted: the caller
// paints it — as vectors, for a PDF (see PDFWriter.AddLayout). Up to
// opts.Workers slides are laid out concurrently, and no more layouts than
// that exist at once.
func EachLayout(ctx context.Context, b *slides.Build, opts Options, emit func(i int, l *engine.PageLayout) error) error {
	return eachLayout(ctx, b, opts, func(l *engine.PageLayout) (*engine.PageLayout, error) { return l, nil }, emit)
}

func eachLayout[T any](ctx context.Context, b *slides.Build, opts Options, convert func(*engine.PageLayout) (T, error), emit func(int, T) error) error {
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
		return fmt.Errorf("export: the theme gives the slides no size")
	}
	view := image.Rect(0, 0, w, h)
	if !opts.NoFonts {
		faces, err := bundledFaces()
		if err != nil {
			return err
		}
		e.FontFaces = faces
	}
	// Only slides with a fitting heading are fitted (fitHeadings, deck.FitScript
	// in Go, through engine.Adjust): the others are laid out once. No slide
	// runs JavaScript. The engine that fits is made before the workers start,
	// since they share it.
	var fitting *engine.Engine
	if slices.ContainsFunc(b.Slides, func(s deck.Rendered) bool { return fits(s.HTML) }) {
		fitting = engine.New()
		fitting.DisableJS = true
		fitting.FileRoot, fitting.DeviceScale, fitting.FontFaces = e.FileRoot, e.DeviceScale, e.FontFaces
		fitting.Adjust = fitHeadings
	}
	render := func(ctx context.Context, i int) (T, error) {
		eng, html := e, page(b.Packed.CSS, b.Slides[i].HTML)
		if fits(b.Slides[i].HTML) {
			eng = fitting
		}
		l, err := eng.LayoutHTML(ctx, html, opts.BaseURL, view)
		if err != nil {
			var zero T
			return zero, fmt.Errorf("slide %d: %w", i+1, err)
		}
		return convert(l)
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = DefaultWorkers()
	}
	return ordered(ctx, len(b.Slides), workers, render, emit)
}

// fits reports whether a slide has a fitting heading (<!-- fit -->).
func fits(html string) bool { return strings.Contains(html, "data-fit-content") }

// ordered runs work(0), …, work(n-1) on up to workers goroutines and hands
// the results to emit in index order. A token is taken before item i starts
// and given back once emit has had it, so at most workers results exist at
// any time, being computed or waiting for their turn. No goroutine outlives
// the call.
func ordered[T any](ctx context.Context, n, workers int, work func(context.Context, int) (T, error), emit func(int, T) error) error {
	type result struct {
		v   T
		err error
	}
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel() // runs first: the producer and the workers stop, then Wait
	results := make([]chan result, n)
	for i := range results {
		results[i] = make(chan result, 1) // a worker never blocks on its send
	}
	tokens := make(chan struct{}, workers)
	wg.Go(func() {
		for i := range n {
			select {
			case tokens <- struct{}{}:
			case <-ctx.Done():
				return
			}
			wg.Go(func() {
				v, err := work(ctx, i)
				results[i] <- result{v, err}
			})
		}
	})
	for i := range n {
		var r result
		select {
		case r = <-results[i]:
		case <-ctx.Done():
			return ctx.Err()
		}
		if r.err != nil {
			return r.err
		}
		if err := emit(i, r.v); err != nil {
			return err
		}
		<-tokens
	}
	return nil
}

func copyRect(dst, src *image.RGBA) {
	r := dst.Bounds().Intersect(src.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		copy(dst.Pix[dst.PixOffset(r.Min.X, y):dst.PixOffset(r.Max.X, y)], src.Pix[src.PixOffset(r.Min.X, y):src.PixOffset(r.Max.X, y)])
	}
}

// page is the document one slide is rendered from. The theme comes first in
// a sheet of its own, since its @import rules must lead a sheet.
func page(themeCSS, slide string) string {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html><html><head><meta charset=\"utf-8\">")
	b.WriteString("<style>" + themeCSS + "</style>")
	b.WriteString("<style>html,body{margin:0;padding:0;background:#fff}</style>")
	b.WriteString("</head><body><div class=\"marpit\">" + slide + "</div></body></html>")
	return b.String()
}

// bundledFaces are the fonts Marp's built-in themes ask for, embedded, so
// that a render needs no network: gaia and uncover's Lato and Roboto Mono,
// from go-opentype/fonts (OFL-1.1). gaia's headings ask for Lato 900, its
// code for Roboto Mono 700, and comments and emphasis for italics; each
// weight and style is its own face so that the engine picks it instead of
// synthesising one (Lato's 700 keeps the bold slot, being nearer 700 than
// its 900).
//
// They are given to the engine as installed fonts (engine.FontFaces),
// parsed once per process, rather than declared in every slide as @font-face
// rules with data: URIs, which the engine had to read, decode and parse on
// every slide.
var bundledFaces = sync.OnceValues(func() ([]engine.LoadedFontFace, error) {
	var faces []engine.LoadedFontFace
	for _, f := range []struct {
		family string
		weight int
		italic bool
		ttf    []byte
	}{
		{"Lato", 400, false, lato.TTF},
		{"Lato", 700, false, lato.Bold},
		{"Lato", 900, false, lato.Black},
		{"Lato", 400, true, lato.Italic},
		{"Lato", 700, true, lato.BoldItalic},
		{"Lato", 900, true, lato.BlackItalic},
		{"Roboto Mono", 400, false, robotomono.TTF},
		{"Roboto Mono", 700, false, robotomono.Bold},
		{"Roboto Mono", 400, true, robotomono.Italic},
		{"Roboto Mono", 700, true, robotomono.BoldItalic},
	} {
		face, err := engine.ParseFontFace(f.family, f.weight, f.italic, f.ttf)
		if err != nil {
			return nil, fmt.Errorf("export: bundled font %s %d: %w", f.family, f.weight, err)
		}
		faces = append(faces, face)
	}
	return faces, nil
})

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

// PDF writes one page per slide, each drawn from its image (see
// PDFWriter.Add; VectorPDF draws them as vectors). A page is the slide's
// size, a CSS pixel being 3/4 of a point, as Chrome prints Marp's @page
// size. The outline lists each slide and, under it, its headings (marp
// --pdf-outlines).
func PDF(w io.Writer, b *slides.Build, images []*image.RGBA) error {
	p := NewPDF(b)
	for _, img := range images {
		p.Add(img)
	}
	return p.Write(w)
}

// VectorPDF writes one page per slide drawn as vectors (see
// PDFWriter.AddLayout), laying the slides out as Each renders them.
func VectorPDF(ctx context.Context, w io.Writer, b *slides.Build, opts Options) error {
	p := NewPDF(b)
	if err := EachLayout(ctx, b, opts, func(_ int, l *engine.PageLayout) error {
		return p.AddLayout(l)
	}); err != nil {
		return err
	}
	return p.Write(w)
}

// PDFWriter builds a PDF one slide at a time, so that a deck is never held
// whole: each page is drawn when it is added — from an image (Add) or from
// a layout, as vectors (AddLayout) — and what it was drawn from can be
// dropped.
type PDFWriter struct {
	b     *slides.Build
	opts  PDFOptions
	doc   *pdfkit.Document
	size  pdfkit.PageSize
	n     int
	fonts *pdfcanvas.Fonts
	// root is the structure's root, nil for an untagged PDF.
	root *pdfkit.StructElem

	// Stats adds up what AddLayout drew as vectors and what it had to
	// draw as bitmaps (paint.VectorStats).
	Stats paint.VectorStats
}

// PDFOptions are what the PDF carries beyond its pages, marp-cli's
// --pdf-notes and --pdf-outlines.
type PDFOptions struct {
	// Notes adds each slide's speaker notes to its page as a text annotation
	// (marp-cli --pdf-notes).
	Notes bool
	// OutlinePages adds a bookmark "Page N" for each slide, and
	// OutlineHeadings one for each heading of the slide, nested by level
	// under it (marp-cli --pdf-outlines.pages and .headings).
	OutlinePages, OutlineHeadings bool
	// Untagged writes the PDF without its logical structure. By default a
	// PDF is tagged, as Chrome's and so marp-cli's are: a screen reader
	// reads its headings, paragraphs, lists, tables, links, images and
	// formulas in reading order, and the file declares PDF/UA-1, with the
	// deck's title and language.
	Untagged bool
	// Lang is the language when the deck's lang directive gives none
	// (--lang); with neither, a tagged PDF says "en", as it must say one.
	Lang string
}

// DefaultPDFOptions are what NewPDF uses: no notes, and the whole outline.
// marp-cli writes no outline unless --pdf-outlines is given; go-slides has
// written one since its first PDF, and keeps doing it.
var DefaultPDFOptions = PDFOptions{OutlinePages: true, OutlineHeadings: true}

// NewPDF starts the PDF of b, with DefaultPDFOptions.
func NewPDF(b *slides.Build) *PDFWriter { return NewPDFWith(b, DefaultPDFOptions) }

// NewPDFWith starts the PDF of b.
func NewPDFWith(b *slides.Build, opts PDFOptions) *PDFWriter {
	g := b.Deck.Global
	po := pdfkit.Options{
		Title:    slides.Title(b.Deck),
		Author:   g.Get("author"),
		Subject:  g.Get("description"),
		Keywords: strings.Join(deck.KeywordList(g.Get("keywords")), "; "),
		Producer: "go-slides",
		// Content and font streams flated, the many small objects of a
		// vector page (link annotations, font dictionaries, structure
		// elements) packed into object streams.
		Compress:      true,
		ObjectStreams: true,
	}
	p := &PDFWriter{
		b:     b,
		opts:  opts,
		size:  pdfkit.NewPageSize(b.Packed.Width*0.75, b.Packed.Height*0.75),
		fonts: pdfcanvas.NewFonts(),
	}
	if !opts.Untagged {
		// What PDF/UA-1 asks of the file: the language, the title in the
		// window's title bar, the identification in the metadata.
		po.Lang = cmp.Or(g.Get("lang"), opts.Lang, "en")
		po.DisplayDocTitle = true
		po.PDFUA = true
		if po.Title == "" {
			po.Title = "Slides"
		}
	}
	p.doc = pdfkit.New(po)
	if !opts.Untagged {
		p.root = p.doc.Structure()
	}
	return p
}

// Add adds the next slide's page, drawn from its image.
func (p *PDFWriter) Add(img image.Image) {
	pg := p.next()
	if p.root != nil {
		// in a tagged PDF, a page that is one picture is one figure
		fig := p.root.Add(pdfkit.StructFigure)
		fig.Alt = fmt.Sprintf("Slide %d", p.n)
		pg.BeginTag(fig)
		defer pg.EndMarkedContent()
	}
	pg.DrawImage(img, pdfkit.Rect{Width: p.size.Width, Height: p.size.Height})
}

// AddLayout adds the next slide's page, drawn from its layout as vectors:
// text as text in the fonts the slide was laid out with (subset-embedded,
// selectable and searchable), backgrounds, borders and gradients as paths
// and shadings, images placed once per document however many slides show
// them, and every link a clickable area. What has no vector form here — an
// element under opacity, a filter, a blurred shadow — is drawn as a bitmap
// of just that element (paint.PaintVector).
func (p *PDFWriter) AddLayout(l *engine.PageLayout) error {
	pg := p.next()
	dev := l.Page()
	if dev.Dx() <= 0 {
		return fmt.Errorf("export: slide %d has no width", p.n)
	}
	s := p.size.Width / float64(dev.Dx()) // points per device pixel
	c := pdfcanvas.New(pg, s, p.fonts)
	if p.root != nil {
		// each slide's <section> is a Sect, its content in reading order
		c.Tag(p.root)
	}
	pg.Save()
	pg.Rectangle(pdfkit.Rect{Width: p.size.Width, Height: p.size.Height})
	pg.Clip()
	pg.EndPath()
	st := l.PaintVector(c)
	c.Finish()
	pg.Restore()
	if c.Err != nil {
		return fmt.Errorf("export: slide %d: %w", p.n, c.Err)
	}
	p.Stats.Rasterized += st.Rasterized
	p.Stats.RasterPixels += st.RasterPixels
	p.Stats.Skipped += st.Skipped
	p.Stats.Images += st.Images
	p.Stats.Glyphs += st.Glyphs
	for _, ln := range l.Links() {
		r := ln.Rect.Intersect(dev)
		if r.Empty() {
			continue
		}
		rect := pdfkit.Rect{
			X: float64(r.Min.X) * s, Y: p.size.Height - float64(r.Max.Y)*s,
			Width: float64(r.Dx()) * s, Height: float64(r.Dy()) * s,
		}
		if e := c.Element(ln.Node); e != nil {
			// under its Link element, its text as its description
			pg.AddTaggedLink(e, rect, ln.Href, cmp.Or(pdfcanvas.TextOf(ln.Node), ln.Href))
			continue
		}
		pg.AddLink(rect, ln.Href)
	}
	return nil
}

// next adds the next slide's page and its outline entries.
func (p *PDFWriter) next() *pdfkit.Page {
	i := p.n
	p.n++
	pg := p.doc.AddPage(p.size)
	if i < len(p.b.Slides) {
		s := p.b.Slides[i].Slide
		// As marp-cli's pdf-lib post-processing builds it: "Page N", bold,
		// open, and the slide's headings nested by their level under it —
		// or, without the pages, at the top. A heading jumps to its slide;
		// marp-cli also scrolls to the heading's place, which needs the
		// layout's element positions.
		base := 0
		if p.opts.OutlinePages {
			p.doc.AddOutlineItemWith(fmt.Sprintf("Page %d", i+1), 1, i, pdfkit.OutlineOptions{Bold: true})
			base = 1
		}
		if p.opts.OutlineHeadings {
			for _, hd := range s.Headings {
				p.doc.AddOutlineItem(hd.Text, base+hd.Level, i)
			}
		}
		if p.opts.Notes && len(s.Notes) > 0 {
			// marp-cli's annotation, field for field: a yellow Note icon at
			// the bottom left, the slide's notes as its contents and the
			// deck's author as the annotation's title.
			pg.AddTextAnnotation(pdfkit.TextAnnotation{
				Rect:     pdfkit.Rect{X: 0, Y: 20, Width: 20},
				Contents: strings.Join(s.Notes, "\n\n"),
				Title:    p.b.Deck.Global.Get("author"),
				Subject:  "Note",
				Name:     "Note",
				Color:    []float64{1, 0.92, 0.42},
				Opacity:  0.25,
			})
		}
	}
	return pg
}

// Write writes the document.
func (p *PDFWriter) Write(w io.Writer) error {
	var buf bytes.Buffer
	if err := p.doc.Write(&buf); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}
