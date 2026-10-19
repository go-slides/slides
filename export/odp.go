package export

import (
	"bytes"
	"image"
	"io"
	"strings"

	"github.com/go-odf/odf"
	"github.com/go-opentype/opentype"
	"github.com/go-webengine/engine"
	"github.com/go-webengine/engine/officecanvas"

	"github.com/go-slides/slides"
)

// ODP writes an OpenDocument presentation through go-odf: each slide is a
// full-slide picture at the slide's size, with the slide's speaker notes, as
// PPTX does. Render at Scale 2 for a sharp deck.
func ODP(w io.Writer, b *slides.Build, images []*image.RGBA) error {
	p := NewODP(b)
	for _, img := range images {
		var buf bytes.Buffer
		if err := PNG(&buf, img); err != nil {
			return err
		}
		p.Add(buf.Bytes())
	}
	return p.Write(w)
}

// ODPWriter builds the deck ODP writes one slide at a time, from each
// slide's PNG (EachEncoded gives them), so that the deck is never held as
// images.
type ODPWriter struct {
	b     *slides.Build
	p     odf.Presentation
	pics  pictures
	faces map[*opentype.Font]*officecanvas.Face
}

// AddLayout adds the next slide as editable content, as PPTXWriter.AddLayout
// does. A gradient is a picture of itself: an ODF gradient has two colours.
func (w *ODPWriter) AddLayout(l *engine.PageLayout) error {
	if w.faces == nil {
		w.faces = map[*opentype.Font]*officecanvas.Face{}
	}
	pg, dev := record(l, w.faces)
	px := float64(w.p.Width) / float64(dev.Dx()) // CSS px per device pixel
	bg, shapes, err := odpShapes(pg, px, px*0.75, &w.pics)
	if err != nil {
		return err
	}
	i := len(w.p.Slides)
	sl := odf.PresentationSlide{Background: bg, Shapes: shapes}
	if bg == nil && len(shapes) == 0 {
		sl.Background = &odf.RGBA{R: 255, G: 255, B: 255, A: 255}
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

// NewODP starts the OpenDocument deck of b. go-odf takes the size in CSS
// pixels.
func NewODP(b *slides.Build) *ODPWriter {
	g := b.Deck.Global
	return &ODPWriter{b: b, p: odf.Presentation{
		Width:    int(b.Packed.Width),
		Height:   int(b.Packed.Height),
		Title:    g.Get("title"),
		Author:   g.Get("author"),
		Subject:  g.Get("description"),
		Keywords: g.Get("keywords"),
	}}
}

// Add adds the next slide, its picture given as PNG bytes.
func (w *ODPWriter) Add(png []byte) {
	i := len(w.p.Slides)
	sl := odf.PresentationSlide{Image: png, ImageType: "png"}
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
func (w *ODPWriter) Write(out io.Writer) error {
	w.p.Images = w.p.Images[:0]
	for _, d := range w.pics.data {
		w.p.Images = append(w.p.Images, odf.Image{Data: d, Type: "png"})
	}
	w.p.Fonts = w.p.Fonts[:0]
	for _, f := range embeddedFaces(w.faces) {
		w.p.Fonts = append(w.p.Fonts, odf.Font{Family: f.family, Regular: f.faces[0], Bold: f.faces[1], Italic: f.faces[2], BoldItal: f.faces[3]})
	}
	data, err := odf.WritePresentation(w.p)
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	return err
}
