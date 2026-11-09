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

	"github.com/go-slides/slides/doc"
)

// emuPerPixel converts a CSS pixel to EMU: 914400 EMU per inch, 96 px per
// inch.
const emuPerPixel = 9525

// PPTX writes a PowerPoint deck through go-ooxml: each slide is a full-slide
// picture, at the slide's size, with the slide's speaker notes, as marp-cli's
// --pptx does. The pictures are PNG; render them at Scale 2 for a sharp deck
// (marp-cli's default for PPTX).
func PPTX(w io.Writer, b *doc.Doc, images []*image.RGBA) error {
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
	b     *doc.Doc
	p     ooxml.Presentation
	pics  pictures
	faces map[*opentype.Font]*officecanvas.Face
	// anims holds, by slide index, the transition and builds go-ooxml has no
	// field for: written into the slides' XML when the deck is (pptxanim.go).
	anims map[int]pptxAnim
	// videos holds, by slide index, the pictures that are the poster of a
	// video of the deck's Media (video.go).
	videos pptxVideos
}

// noteAnim records the transition into slide i and, when it has fragment
// steps, its builds (clicks makes them: it is called only then).
func (w *PPTXWriter) noteAnim(i int, steps bool, clicks func() [][]pptxTarget) {
	var a pptxAnim
	if i < len(w.b.Slides) {
		a.transition = w.b.Slides[i].Transition
	}
	if steps && clicks != nil {
		a.clicks = clicks()
	}
	if a.transition == nil && len(a.clicks) == 0 {
		return
	}
	if w.anims == nil {
		w.anims = map[int]pptxAnim{}
	}
	w.anims[i] = a
}

// AddLayout adds the next slide as editable content: its text in text boxes
// set in the slide's own fonts (embedded), its backgrounds, borders and
// gradients as shapes, its pictures each stored once — drawn from the slide's
// layout, so at the positions of the image and PDF exports. What a slide
// cannot hold is a picture of that element (see officecanvas).
func (w *PPTXWriter) AddLayout(l *engine.PageLayout) error {
	return w.AddSteps([]*engine.PageLayout{l})
}

// AddSteps adds the next slide from the layouts of its steps (EachSteps): the
// last is the slide, and the shapes each earlier step leaves out are revealed
// by a click each, as the slide's fragments are in the viewer (see builds.go).
// The slide's transition, which the writer takes from the deck, is written
// either way.
func (w *PPTXWriter) AddSteps(steps []*engine.PageLayout) error {
	if w.faces == nil {
		w.faces = map[*opentype.Font]*officecanvas.Face{}
	}
	pages := make([]officecanvas.Page, len(steps))
	var dev image.Rectangle
	for s, l := range steps {
		pages[s], dev = record(l, w.faces)
	}
	pg := pages[len(pages)-1]
	k := float64(w.p.Width) / float64(dev.Dx()) // EMU per device pixel
	bg, shapes, from, err := pptxShapes(pg, k, k/12700, &w.pics)
	if err != nil {
		return err
	}
	i := len(w.p.Slides)
	if vs := findVideos(steps[len(steps)-1].Links(), w.b.Media, pptxBoxes(shapes, k)); len(vs) > 0 {
		if w.videos == nil {
			w.videos = pptxVideos{}
		}
		w.videos[i] = vs
	}
	w.noteAnim(i, len(steps) > 1, func() [][]pptxTarget { return pptxClicks(shapes, from, fragmentShapes(pages)) })
	sl := ooxml.PresentationSlide{Background: bg, Shapes: shapes}
	if bg == nil && len(shapes) == 0 {
		// A blank slide still needs something to be a slide of.
		sl.Background = &ooxml.Fill{Kind: ooxml.FillSolid, Color: ooxml.RGBA{R: 255, G: 255, B: 255, A: 255}}
	}
	if i < len(w.b.Slides) {
		s := w.b.Slides[i]
		if len(s.Headings) > 0 {
			sl.Name = s.Headings[0].Text
		}
		sl.Notes = joinNotes(s.Notes)
	}
	w.p.Slides = append(w.p.Slides, sl)
	return nil
}

// NewPPTX starts the PowerPoint deck of b.
func NewPPTX(b *doc.Doc) *PPTXWriter {
	return &PPTXWriter{b: b, p: ooxml.Presentation{
		Width:    int64(b.Width) * emuPerPixel,
		Height:   int64(b.Height) * emuPerPixel,
		Title:    b.TitleDirective,
		Author:   b.Author,
		Subject:  b.Description,
		Keywords: b.KeywordsDirective,
	}}
}

// Add adds the next slide, its picture given as PNG bytes.
func (w *PPTXWriter) Add(png []byte) {
	i := len(w.p.Slides)
	w.noteAnim(i, false, nil)
	sl := ooxml.PresentationSlide{Image: png, ImageType: "png"}
	if i < len(w.b.Slides) {
		s := w.b.Slides[i]
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
	if len(w.anims) > 0 {
		if data, err = addPPTXAnimations(data, w.anims); err != nil {
			return err
		}
	}
	if len(w.videos) > 0 {
		if data, err = addPPTXVideos(data, w.videos, w.b.Media); err != nil {
			return err
		}
	}
	_, err = out.Write(data)
	return err
}
