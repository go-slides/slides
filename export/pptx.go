package export

import (
	"bytes"
	"image"
	"io"
	"strings"

	"github.com/go-ooxml/ooxml"

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
	b *slides.Build
	p ooxml.Presentation
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
	data, err := ooxml.WritePresentation(w.p)
	if err != nil {
		return err
	}
	_, err = out.Write(data)
	return err
}
