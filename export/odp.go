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

	"github.com/go-slides/slides/doc"
)

// ODP writes an OpenDocument presentation through go-odf: each slide is a
// full-slide picture at the slide's size, with the slide's speaker notes, as
// PPTX does. Render at Scale 2 for a sharp deck.
func ODP(w io.Writer, b *doc.Doc, images []*image.RGBA) error {
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
	b     *doc.Doc
	p     odf.Presentation
	pics  pictures
	faces map[*opentype.Font]*officecanvas.Face
	// anims holds, by slide index, the transition and builds go-odf has no
	// field for: written into content.xml with the deck (odpanim.go).
	anims map[int]odpAnim
	// videos holds, by slide index, the pictures that are the poster of a
	// video of the deck's Media (video.go).
	videos odpVideos
}

// noteAnim records the transition into slide i and, when it has fragment
// steps, its builds (clicks makes them: it is called only then).
func (w *ODPWriter) noteAnim(i int, steps bool, clicks func() [][]int) {
	var a odpAnim
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
		w.anims = map[int]odpAnim{}
	}
	w.anims[i] = a
}

// odpClicks turns the fragments' shapes (indexes of the recorded page) into
// click targets: the indexes of the emitted shapes, shapes[j] having been
// made from the page's shape from[j].
func odpClicks(from []int, fragments [][]int) [][]int {
	pos := make(map[int]int, len(from))
	for j, f := range from {
		pos[f] = j
	}
	var clicks [][]int
	for _, frag := range fragments {
		var c []int
		for _, f := range frag {
			if j, ok := pos[f]; ok {
				c = append(c, j)
			}
		}
		if len(c) > 0 {
			clicks = append(clicks, c)
		}
	}
	return clicks
}

// AddLayout adds the next slide as editable content, as PPTXWriter.AddLayout
// does. A gradient is a picture of itself: an ODF gradient has two colours.
func (w *ODPWriter) AddLayout(l *engine.PageLayout) error {
	return w.AddSteps([]*engine.PageLayout{l})
}

// AddSteps adds the next slide from the layouts of its steps (EachSteps), as
// PPTXWriter.AddSteps does: the shapes each earlier step leaves out are
// revealed by a click each (see builds.go and odpanim.go).
func (w *ODPWriter) AddSteps(steps []*engine.PageLayout) error {
	if w.faces == nil {
		w.faces = map[*opentype.Font]*officecanvas.Face{}
	}
	pages := make([]officecanvas.Page, len(steps))
	var dev image.Rectangle
	for s, l := range steps {
		pages[s], dev = record(l, w.faces)
	}
	pg := pages[len(pages)-1]
	px := float64(w.p.Width) / float64(dev.Dx()) // CSS px per device pixel
	bg, shapes, from, err := odpShapes(pg, px, px*0.75, &w.pics)
	if err != nil {
		return err
	}
	i := len(w.p.Slides)
	if vs := findVideos(steps[len(steps)-1].Links(), w.b.Media, odpBoxes(shapes, px)); len(vs) > 0 {
		if w.videos == nil {
			w.videos = odpVideos{}
		}
		w.videos[i] = vs
	}
	w.noteAnim(i, len(steps) > 1, func() [][]int { return odpClicks(from, fragmentShapes(pages)) })
	sl := odf.PresentationSlide{Background: bg, Shapes: shapes}
	if bg == nil && len(shapes) == 0 {
		sl.Background = &odf.RGBA{R: 255, G: 255, B: 255, A: 255}
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

// NewODP starts the OpenDocument deck of b. go-odf takes the size in CSS
// pixels.
func NewODP(b *doc.Doc) *ODPWriter {
	return &ODPWriter{b: b, p: odf.Presentation{
		Width:    int(b.Width),
		Height:   int(b.Height),
		Title:    b.TitleDirective,
		Author:   b.Author,
		Subject:  b.Description,
		Keywords: b.KeywordsDirective,
	}}
}

// Add adds the next slide, its picture given as PNG bytes.
func (w *ODPWriter) Add(png []byte) {
	i := len(w.p.Slides)
	w.noteAnim(i, false, nil)
	sl := odf.PresentationSlide{Image: png, ImageType: "png"}
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
	if len(w.anims) > 0 {
		if data, err = addODPAnimations(data, w.anims); err != nil {
			return err
		}
	}
	if len(w.videos) > 0 {
		if data, err = addODPVideos(data, w.videos, w.b.Media); err != nil {
			return err
		}
	}
	_, err = out.Write(data)
	return err
}
