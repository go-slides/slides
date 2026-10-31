package export

import (
	"bytes"
	"crypto/sha256"
	"image"
	"image/draw"
	"image/png"
	"math"
	"strings"

	"github.com/go-odf/odf"
	"github.com/go-ooxml/ooxml"
	"github.com/go-opentype/opentype"
	"github.com/go-webengine/engine"
	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/officecanvas"
	"github.com/go-webengine/engine/paint"
)

// record draws a slide's layout into an office canvas: the slide as the
// filled shapes, gradients, lines of text and pictures an editable deck is
// made of, at the layout's own positions.
func record(l *engine.PageLayout, faces map[*opentype.Font]*officecanvas.Face) (officecanvas.Page, image.Rectangle) {
	page := l.Page()
	var links []officecanvas.Link
	for _, ln := range l.Links() {
		links = append(links, officecanvas.Link{Rect: ln.Rect, Href: ln.Href})
	}
	c := officecanvas.New(page, links, faces)
	l.PaintVector(c)
	return c.Page(), page
}

// pictures stores each distinct picture once: the same pixels — a
// background shown on every slide, decoded again for each — are one image
// in the deck.
type pictures struct {
	index map[[32]byte]int
	data  [][]byte
}

// add returns the picture's 1-based number, encoding it as PNG the first
// time its pixels are seen.
func (p *pictures) add(img image.Image) (int, error) {
	if p.index == nil {
		p.index = map[[32]byte]int{}
	}
	n := toNRGBA(img)
	h := sha256.New()
	var dim [8]byte
	b := n.Bounds()
	dim[0], dim[1], dim[2], dim[3] = byte(b.Dx()>>24), byte(b.Dx()>>16), byte(b.Dx()>>8), byte(b.Dx())
	dim[4], dim[5], dim[6], dim[7] = byte(b.Dy()>>24), byte(b.Dy()>>16), byte(b.Dy()>>8), byte(b.Dy())
	h.Write(dim[:])
	for y := b.Min.Y; y < b.Max.Y; y++ {
		h.Write(n.Pix[n.PixOffset(b.Min.X, y):n.PixOffset(b.Max.X, y)])
	}
	var key [32]byte
	copy(key[:], h.Sum(nil))
	if i, ok := p.index[key]; ok {
		return i, nil
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, n); err != nil {
		return 0, err
	}
	p.data = append(p.data, buf.Bytes())
	p.index[key] = len(p.data)
	return len(p.data), nil
}

func toNRGBA(img image.Image) *image.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n
	}
	b := img.Bounds()
	n := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(n, n.Bounds(), img, b.Min, draw.Src)
	return n
}

// crop cuts the given fractions off a picture's sides.
func crop(img image.Image, c [4]float64) image.Image {
	if c == [4]float64{} {
		return img
	}
	n := toNRGBA(img)
	b := n.Bounds()
	r := image.Rect(
		b.Min.X+int(math.Round(c[0]*float64(b.Dx()))), b.Min.Y+int(math.Round(c[1]*float64(b.Dy()))),
		b.Max.X-int(math.Round(c[2]*float64(b.Dx()))), b.Max.Y-int(math.Round(c[3]*float64(b.Dy()))),
	)
	if r.Empty() {
		return n
	}
	return n.SubImage(r)
}

// embeddedFaces groups the faces the deck's text is set in by family, with
// their font files, leaving out the fonts whose licence forbids it.
type familyFaces struct {
	family string
	faces  [4][]byte // regular, bold, italic, bold italic
}

func embeddedFaces(faces map[*opentype.Font]*officecanvas.Face) []familyFaces {
	byFamily := map[string]*familyFaces{}
	var order []string
	for _, f := range faces {
		if !f.Embeddable {
			continue
		}
		ff := byFamily[f.Family]
		if ff == nil {
			ff = &familyFaces{family: f.Family}
			byFamily[f.Family] = ff
			order = append(order, f.Family)
		}
		slot := 0
		if f.Bold {
			slot |= 1
		}
		if f.Italic {
			slot |= 2
		}
		if ff.faces[slot] == nil {
			ff.faces[slot] = officecanvas.SFNT(f.Font)
		}
	}
	sortStrings(order)
	out := make([]familyFaces, 0, len(order))
	for _, f := range order {
		out = append(out, *byFamily[f])
	}
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

func rgba(c css.Color) ooxml.RGBA { return ooxml.RGBA{R: c.R, G: c.G, B: c.B, A: c.A} }

// pptxShapes turns a recorded slide into DrawingML shapes, k EMU per device
// pixel and pt points per device pixel.
func pptxShapes(pg officecanvas.Page, k, pt float64, pics *pictures) (*ooxml.Fill, []ooxml.Shape, error) {
	var bg *ooxml.Fill
	if pg.Background != nil {
		bg = &ooxml.Fill{Kind: ooxml.FillSolid, Color: rgba(*pg.Background)}
	}
	out, err := pptxShapeList(pg.Shapes, k, pt, pics)
	return bg, out, err
}

// pptxShapeList converts shapes in order, a group (an SVG drawn as vector
// shapes) into a p:grpSp of its members.
func pptxShapeList(shapes []officecanvas.Shape, k, pt float64, pics *pictures) ([]ooxml.Shape, error) {
	emu := func(v float64) int64 { return int64(math.Round(v * k)) }
	var out []ooxml.Shape
	for _, s := range shapes {
		sh := ooxml.Shape{X: emu(s.Bounds.X), Y: emu(s.Bounds.Y), W: max(emu(s.Bounds.W), 1), H: max(emu(s.Bounds.H), 1)}
		switch s.Kind {
		case officecanvas.KindGroup:
			members, err := pptxShapeList(s.Group, k, pt, pics)
			if err != nil {
				return nil, err
			}
			if len(members) == 0 {
				continue
			}
			sh.Group = members
		case officecanvas.KindImage:
			n, err := pics.add(s.Image)
			if err != nil {
				return nil, err
			}
			sh.Image, sh.Crop = n, s.Crop
		case officecanvas.KindText:
			for _, r := range s.Runs {
				sh.Text = append(sh.Text, ooxml.TextRun{
					Text: r.Text, Font: r.Face.Family, Size: math.Max(math.Round(r.Size*pt*100)/100, 1),
					Bold: r.Face.Bold, Italic: r.Face.Italic, Color: rgba(r.Color), Link: r.Link, Spacing: r.Spacing * pt,
				})
			}
		case officecanvas.KindGradient, officecanvas.KindFill:
			if s.Kind == officecanvas.KindGradient && s.Gradient.Radial {
				// A DrawingML radial gradient is a circle grown to the box,
				// a CSS one an ellipse to a corner: drawn as a picture of
				// itself, it is the one the slide shows.
				img := s.Render(k / emuPerPixel) // a pixel per CSS pixel: a gradient is smooth
				if img == nil {
					continue
				}
				n, err := pics.add(img)
				if err != nil {
					return nil, err
				}
				sh.Image = n
				break
			}
			if !s.Rect {
				sh.Geometry.Paths = pathsEMU(s.Path, s.Bounds, k)
			}
			if s.Kind == officecanvas.KindFill {
				sh.Fill = ooxml.Fill{Kind: ooxml.FillSolid, Color: rgba(s.Color)}
			} else {
				g := s.Gradient
				f := ooxml.Fill{Kind: ooxml.FillGradient, Angle: g.Angle, Radial: g.Radial, Center: g.Center}
				for _, st := range g.Stops {
					f.Stops = append(f.Stops, ooxml.GradientStop{Pos: st.Pos, Color: rgba(st.Color)})
				}
				if len(f.Stops) < 2 {
					continue
				}
				sh.Fill = f
			}
		}
		out = append(out, sh)
	}
	return out, nil
}

func pathsEMU(p *paint.Path, b paint.Rect, k float64) [][]ooxml.PathCmd {
	pt := func(q paint.Point) ooxml.Point {
		return ooxml.Point{X: int64(math.Round((q.X - b.X) * k)), Y: int64(math.Round((q.Y - b.Y) * k))}
	}
	var out [][]ooxml.PathCmd
	var cur []ooxml.PathCmd
	for _, op := range p.Ops {
		switch op.Kind {
		case paint.PathMoveTo:
			if len(cur) > 0 {
				out = append(out, cur)
			}
			cur = []ooxml.PathCmd{{Op: 'M', P: [3]ooxml.Point{pt(op.P[0])}}}
		case paint.PathLineTo:
			cur = append(cur, ooxml.PathCmd{Op: 'L', P: [3]ooxml.Point{pt(op.P[0])}})
		case paint.PathCubeTo:
			cur = append(cur, ooxml.PathCmd{Op: 'C', P: [3]ooxml.Point{pt(op.P[0]), pt(op.P[1]), pt(op.P[2])}})
		case paint.PathClose:
			cur = append(cur, ooxml.PathCmd{Op: 'Z'})
		}
	}
	if len(cur) > 0 {
		out = append(out, cur)
	}
	return out
}

func odfRGBA(c css.Color) odf.RGBA { return odf.RGBA{R: c.R, G: c.G, B: c.B, A: c.A} }

// odpShapes turns a recorded slide into ODF draw shapes, px CSS pixels and pt
// points per device pixel. A gradient, which an ODF gradient could only say
// with two colours, is drawn as a picture of itself.
func odpShapes(pg officecanvas.Page, px, pt float64, pics *pictures) (*odf.RGBA, []odf.Shape, error) {
	var bg *odf.RGBA
	if pg.Background != nil {
		c := odfRGBA(*pg.Background)
		bg = &c
	}
	out, err := odpShapeList(pg.Shapes, px, pt, pics)
	return bg, out, err
}

// odpShapeList converts shapes in order, a group (an SVG drawn as vector
// shapes) into a draw:g of its members.
func odpShapeList(shapes []officecanvas.Shape, px, pt float64, pics *pictures) ([]odf.Shape, error) {
	var out []odf.Shape
	for _, s := range shapes {
		sh := odf.Shape{X: s.Bounds.X * px, Y: s.Bounds.Y * px, W: s.Bounds.W * px, H: s.Bounds.H * px}
		switch s.Kind {
		case officecanvas.KindGroup:
			members, err := odpShapeList(s.Group, px, pt, pics)
			if err != nil {
				return nil, err
			}
			if len(members) == 0 {
				continue
			}
			sh.Group = members
		case officecanvas.KindImage:
			n, err := pics.add(crop(s.Image, s.Crop))
			if err != nil {
				return nil, err
			}
			sh.Kind, sh.Image = odf.ShapeImage, n
		case officecanvas.KindGradient:
			img := s.Render(px) // a pixel per CSS pixel: a gradient is smooth
			if img == nil {
				continue
			}
			n, err := pics.add(img)
			if err != nil {
				return nil, err
			}
			sh.Kind, sh.Image = odf.ShapeImage, n
		case officecanvas.KindText:
			sh.Kind = odf.ShapeText
			for _, r := range s.Runs {
				sh.Text = append(sh.Text, odf.TextRun{
					Text: r.Text, Font: r.Face.Family, Size: math.Max(math.Round(r.Size*pt*100)/100, 1),
					Bold: r.Face.Bold, Italic: r.Face.Italic, Color: odfRGBA(r.Color), Link: r.Link, Spacing: r.Spacing * pt,
				})
			}
		case officecanvas.KindFill:
			sh.Fill = odfRGBA(s.Color)
			if !s.Rect {
				sh.Kind, sh.NonZero = odf.ShapePath, !s.EvenOdd
				// In hundredths of a pixel, then back: pathsEMU rounds.
				for _, path := range pathsEMU(s.Path, s.Bounds, px*100) {
					var cmds []odf.PathCmd
					for _, c := range path {
						cmds = append(cmds, odf.PathCmd{Op: c.Op, P: [3][2]float64{
							{float64(c.P[0].X) / 100, float64(c.P[0].Y) / 100}, {float64(c.P[1].X) / 100, float64(c.P[1].Y) / 100}, {float64(c.P[2].X) / 100, float64(c.P[2].Y) / 100}}})
					}
					sh.Paths = append(sh.Paths, cmds)
				}
			}
		}
		out = append(out, sh)
	}
	return out, nil
}

// slideName is a slide's name: its first heading, else empty (the writers
// number it).
func joinNotes(notes []string) string { return strings.Join(notes, "\n\n") }
