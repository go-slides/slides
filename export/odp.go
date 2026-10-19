package export

import (
	"bytes"
	"image"
	"io"
	"strings"

	"github.com/go-odf/odf"

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
	b *slides.Build
	p odf.Presentation
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
	data, err := odf.WritePresentation(w.p)
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	return err
}
