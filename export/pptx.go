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
	g := b.Deck.Global
	p := ooxml.Presentation{
		Width:    int64(b.Packed.Width) * emuPerPixel,
		Height:   int64(b.Packed.Height) * emuPerPixel,
		Title:    g.Get("title"),
		Author:   g.Get("author"),
		Subject:  g.Get("description"),
		Keywords: g.Get("keywords"),
	}
	for i, img := range images {
		var buf bytes.Buffer
		if err := PNG(&buf, img); err != nil {
			return err
		}
		s := b.Slides[i].Slide
		name := ""
		if len(s.Headings) > 0 {
			name = s.Headings[0].Text
		}
		p.Slides = append(p.Slides, ooxml.PresentationSlide{
			Image:     buf.Bytes(),
			ImageType: "png",
			Notes:     strings.Join(s.Notes, "\n\n"),
			Name:      name,
		})
	}
	out, err := ooxml.WritePresentation(p)
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}
