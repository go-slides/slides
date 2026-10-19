package export

import (
	"bytes"
	"image"
	"io"
	"strings"

	"github.com/go-ooxml/ooxml"
	"github.com/go-opentype/opentype"
	"github.com/go-webengine/engine"
	"github.com/go-webengine/engine/officecanvas"

	"github.com/go-slides/slides"
)

// emuPerPixel converts a CSS pixel to EMU: 914400 EMU per inch, 96 px per
// inch.
const emuPerPixel = 9525

// PPTX writes a PowerPoint deck through go-ooxml: each slide is a full-slide
// picture, at the slide's size, with the slide's speaker notes, as marp-cli's
// --pptx does. The pictures are PNG; render them at Scale 2 for a sharp deck
// (marp-cli's default for PPTX).
func PPTX(w io.Writer, b *slides.Build, images []*image.RGBA) error {
	p := NewPPTX(b)
	for _, img := range images {
		var buf bytes.Buffer
		if err := PNG(&buf, img); err != nil {
			return err
		}
		p.Add(buf.Bytes())
	}
	return p.Write(w)
}

// PPTXWriter builds the deck PPTX writes one slide at a time, from each
// slide's PNG (EachEncoded gives them), so that the deck is never held as
// images.
type PPTXWriter struct {
	b     *slides.Build
	p     ooxml.Presentation
	pics  pictures
	faces map[*opentype.Font]*officecanvas.Face
}

// AddLayout adds the next slide as editable content: its text in text boxes
// set in the slide's own fonts (embedded), its backgrounds, borders and
// gradients as shapes, its pictures each stored once — drawn from the slide's
// layout, so at the positions of the image and PDF exports. What a slide
// cannot hold is a picture of that element (see officecanvas).
func (w *PPTXWriter) AddLayout(l *engine.PageLayout) error {
	if w.faces == nil {
		w.faces = map[*opentype.Font]*officecanvas.Face{}
	}
	pg, dev := record(l, w.faces)
	k := float64(w.p.Width) / float64(dev.Dx()) // EMU per device pixel
	bg, shapes, err := pptxShapes(pg, k, k/12700, &w.pics)
	if err != nil {
		return err
	}
	i := len(w.p.Slides)
	sl := ooxml.PresentationSlide{Background: bg, Shapes: shapes}
	if bg == nil && len(shapes) == 0 {
		// A blank slide still needs something to be a slide of.
		sl.Background = &ooxml.Fill{Kind: ooxml.FillSolid, Color: ooxml.RGBA{R: 255, G: 255, B: 255, A: 255}}
	}
	if i < len(w.b.Slides) {
		s := w.b.Slides[i].Slide
		if len(s.Headings) > 0 {
			sl.Name = s.Headings[0].Text
		}
		sl.Notes = joinNotes(s.Notes)
	}
	w.p.Slides = append(w.p.Slides, sl)
	return nil
}

// NewPPTX starts the PowerPoint deck of b.
func NewPPTX(b *slides.Build) *PPTXWriter {
	g := b.Deck.Global
	return &PPTXWriter{b: b, p: ooxml.Presentation{
		Width:    int64(b.Packed.Width) * emuPerPixel,
		Height:   int64(b.Packed.Height) * emuPerPixel,
		Title:    g.Get("title"),
		Author:   g.Get("author"),
		Subject:  g.Get("description"),
		Keywords: g.Get("keywords"),
	}}
}

// Add adds the next slide, its picture given as PNG bytes.
func (w *PPTXWriter) Add(png []byte) {
	i := len(w.p.Slides)
	sl := ooxml.PresentationSlide{Image: png, ImageType: "png"}
	if i < len(w.b.Slides) {
		s := w.b.Slides[i].Slide
		if len(s.Headings) > 0 {
			sl.Name = s.Headings[0].Text
		}
		sl.Notes = strings.Join(s.Notes, "\n\n")
	}
	w.p.Slides = append(w.p.Slides, sl)
}

// Write writes the deck.
func (w *PPTXWriter) Write(out io.Writer) error {
	w.p.Images = w.p.Images[:0]
	for _, d := range w.pics.data {
		w.p.Images = append(w.p.Images, ooxml.Image{Data: d, Type: "png"})
	}
	w.p.Fonts = w.p.Fonts[:0]
	for _, f := range embeddedFaces(w.faces) {
		w.p.Fonts = append(w.p.Fonts, ooxml.Font{Typeface: f.family, Regular: f.faces[0], Bold: f.faces[1], Italic: f.faces[2], BoldItal: f.faces[3]})
	}
	data, err := ooxml.WritePresentation(w.p)
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	return err
}
