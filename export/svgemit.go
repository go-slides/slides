package export

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/go-gfx/oksvg/svganim"
	"github.com/go-webengine/engine/officecanvas"
	"github.com/go-webengine/engine/paint"
)

// svgSink gathers, while a slide's shapes are made, what the animated SVGs of
// the slide need written after them: an effect for each animated element, in
// the shape list's terms. The writer reads it once the list is complete.
type svgSink struct {
	effects []svgFx
	// notes say, once per image, how it was carried (see Describe).
	notes map[*animSVG]string
}

// svgFx is the animation of one shape of a slide.
type svgFx struct {
	index int // the shape's place in the slide's top-level shape list
	a     *animSVG
	t     *svgTarget
	box   paint.Rect // the shape's box, in device pixels
	sx    float64    // device pixels per viewBox unit
}

// svgLayer is a piece of a drawing, as an officecanvas shape.
type svgLayer struct {
	shape  officecanvas.Shape
	target *svgTarget // nil for the pieces that do not move
}

func unionRect(a, b paint.Rect) paint.Rect {
	x0, y0 := math.Min(a.X, b.X), math.Min(a.Y, b.Y)
	x1, y1 := math.Max(a.X+a.W, b.X+b.W), math.Max(a.Y+a.H, b.Y+b.H)
	return paint.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// svgLayers splits a drawing into layers, in painting order: each animated
// element's shapes are a layer of their own, the others' runs are one layer
// each. ok is false when the drawing cannot be carried natively at this size.
func svgLayers(a *animSVG, p *svgPlan, s officecanvas.Shape) (layers []svgLayer, sx float64, ok bool, why string) {
	members := s.Group
	if s.Kind != officecanvas.KindGroup {
		members = []officecanvas.Shape{s}
	}
	if s.ViewBox.W <= 0 || s.ViewBox.H <= 0 || s.Dst.W <= 0 || s.Dst.H <= 0 {
		return nil, 0, false, "its box is not known"
	}
	sx = s.Dst.W / s.ViewBox.W
	if sy := s.Dst.H / s.ViewBox.H; math.Abs(sx-sy) > 0.005*sx {
		return nil, 0, false, "the image is stretched unevenly (object-fit or both sizes set), which turns a rotation into a skew"
	}
	owner := func(m officecanvas.Shape) *svgTarget {
		for _, k := range m.Keys {
			if t := p.byKey[k]; t != nil {
				return t
			}
		}
		return nil
	}
	var cur []officecanvas.Shape
	var curT *svgTarget
	flush := func() {
		if len(cur) == 0 {
			return
		}
		l := svgLayer{target: curT}
		if len(cur) == 1 {
			l.shape = cur[0]
		} else {
			b := cur[0].Bounds
			for _, m := range cur[1:] {
				b = unionRect(b, m.Bounds)
			}
			l.shape = officecanvas.Shape{Kind: officecanvas.KindGroup, Bounds: b, Group: slices.Clone(cur)}
		}
		layers = append(layers, l)
		cur = nil
	}
	for i, m := range members {
		o := owner(m)
		if i > 0 && o != curT {
			flush()
		}
		curT = o
		cur = append(cur, m)
	}
	flush()
	// every target is found, and its shapes are as many as the plan counted
	got := map[*svgTarget]int{}
	for _, l := range layers {
		if l.target != nil {
			n := 1
			if l.shape.Kind == officecanvas.KindGroup {
				n = len(l.shape.Group)
			}
			got[l.target] += n
		}
	}
	for _, t := range p.targets {
		if got[t] != t.members {
			return nil, 0, false, fmt.Sprintf("the shapes of element #%d are not in one piece in the slide", t.key)
		}
	}
	return layers, sx, true, ""
}

// svgFrames is the animated GIF of a drawing at a size, made once.
type svgFrames struct {
	data   []byte
	frames int
	fps    float64
	loops  bool
	w, h   int
	err    error
	note   string
}

type gifKey struct{ w, h int }

// frames renders the drawing at the size (dst, in device pixels) as an
// animated GIF, bounded: a width of at most maxGIFWidth, and all the frames
// together at most maxGIFPix pixels.
func (a *animSVG) frames(dstW, dstH float64) *svgFrames {
	span := a.anim.Span()
	ts, step := a.anim.FrameTimes(span, gifFPS)
	w, h := int(math.Round(dstW)), int(math.Round(dstH))
	if w > maxGIFWidth {
		h = int(math.Round(float64(h) * maxGIFWidth / float64(w)))
		w = maxGIFWidth
	}
	if budget := maxGIFPix / float64(len(ts)); float64(w*h) > budget {
		k := math.Sqrt(budget / float64(w*h))
		w, h = int(float64(w)*k), int(float64(h)*k)
	}
	if w < 8 || h < 8 {
		return &svgFrames{err: fmt.Errorf("the drawing would be %dx%d pixels over %d frames: too small to be seen", w, h, len(ts))}
	}
	key := gifKey{w, h}
	a.gifMu.Lock()
	if f, ok := a.gifs[key]; ok {
		a.gifMu.Unlock()
		return f
	}
	a.gifMu.Unlock()
	f := &svgFrames{w: w, h: h, frames: len(ts), fps: 1 / step, loops: span.Loops}
	switch {
	case span.Approx:
		f.note = "the loops have no common period within the limit: the longest is shown"
	case span.Loops && span.Delay:
		f.note = "an animation starts after a delay: each loop of the GIF plays the delay again"
	}
	var imgs []*image.RGBA
	bbox := bboxOf(a.anim, a.svg)
	for _, t := range ts {
		doc, err := a.anim.Apply(a.svg, t, bbox)
		if err != nil {
			f.err = err
			break
		}
		img, err := svganim.Rasterize(doc, w, h)
		if err != nil {
			f.err = err
			break
		}
		imgs = append(imgs, img)
	}
	if f.err == nil {
		f.data, f.err = svganim.GIF(imgs, int(step*1000), span.Loops)
	}
	a.gifMu.Lock()
	if a.gifs == nil {
		a.gifs = map[gifKey]*svgFrames{}
	}
	a.gifs[key] = f
	a.gifMu.Unlock()
	return f
}

// Describe is the line an export says about each animated image: how it was
// carried in format ("pptx", "odp"), and, when it fell back to frames, why.
func (a *SVGAnimations) Describe(format string, notes ...string) []string {
	if a == nil {
		return nil
	}
	var out []string
	for _, s := range a.order {
		where := fmt.Sprintf("%s (slide%s %s)", s.name, plural(len(s.slides)), joinInts(s.slides))
		p := s.getPlan()
		switch format {
		case "pptx", "odp":
			// what the writer found at the slide's size is more exact than the plan
			if i := slices.IndexFunc(notes, func(n string) bool { return strings.HasPrefix(n, CodeSVGAnimation+": "+s.name+" ") }); i >= 0 {
				out = append(out, notes[i])
				continue
			}
			label := strings.ToUpper(format)
			if p.native {
				var warn string
				if len(s.anim.Warnings) > 0 {
					warn = "; not carried: " + strings.Join(s.anim.Warnings, "; ")
				}
				out = append(out, fmt.Sprintf("%s: %s: animated natively in %s: %d element%s (%s)%s",
					CodeSVGAnimation, where, label, len(p.targets), plural(len(p.targets)), strings.Join(p.kinds, ", "), warn))
				continue
			}
			out = append(out, fmt.Sprintf("%s: %s: animated as frames in %s, an animated GIF: %s",
				CodeSVGAnimation, where, label, strings.Join(p.why, "; ")))
		default: // pdf, images
			label := map[string]string{"pdf": "a PDF", "image": "an image"}[format]
			if label == "" {
				label = "an image"
			}
			out = append(out, fmt.Sprintf("%s: %s is animated, which %s cannot show: it is drawn at its initial state (use HTML, PPTX or ODP to see it move)",
				CodeSVGAnimation, where, label))
		}
	}
	sort.Strings(out)
	return out
}

// originOf is the index in the recorded page of the shape made from shape si of
// an expanded list.
func originOf(origin []int, si int) int {
	if origin == nil {
		return si
	}
	return origin[si]
}

// note records, once for the image, what the slide being made had to do for it.
func (s *svgSink) note(a *animSVG, msg string) {
	if s.notes == nil {
		s.notes = map[*animSVG]string{}
	}
	if _, ok := s.notes[a]; !ok {
		s.notes[a] = msg
	}
}

// expandSVG replaces each animated SVG drawing of a slide's top-level shapes
// by what carries it: one shape per layer (an element that moves is a shape of
// its own, with the effect recorded for it), or, when the drawing cannot be
// carried natively, one picture, its animated GIF. origin[i] is the index in
// shapes of what out[i] was made from (nil: the same), fx the effects by index
// in out.
func (p *pictures) expandSVG(shapes []officecanvas.Shape) (out []officecanvas.Shape, origin []int, fx map[int]*svgFx) {
	if p == nil || !p.svg.Animated() || p.nested > 0 || p.sink == nil {
		return shapes, nil, nil
	}
	found := false
	for _, s := range shapes {
		if p.svg.lookup(s.Source) != nil {
			found = true
			break
		}
	}
	if !found {
		return shapes, nil, nil
	}
	fx = map[int]*svgFx{}
	for i, s := range shapes {
		a := p.svg.lookup(s.Source)
		if a == nil {
			out, origin = append(out, s), append(origin, i)
			continue
		}
		pl := a.getPlan()
		why := strings.Join(pl.why, "; ")
		if pl.native && s.Kind != officecanvas.KindImage {
			layers, sx, ok, w := svgLayers(a, pl, s)
			if ok {
				for _, l := range layers {
					out, origin = append(out, l.shape), append(origin, i)
					if l.target != nil {
						fx[len(out)-1] = &svgFx{a: a, t: l.target, box: l.shape.Bounds, sx: sx}
					}
				}
				continue
			}
			why = w
		}
		f := a.frames(s.Dst.W, s.Dst.H)
		if f.err != nil || s.Dst.W <= 0 || s.Dst.H <= 0 {
			reason := why
			if f.err != nil {
				reason += ": " + f.err.Error()
			}
			p.sink.note(a, fmt.Sprintf("%s: %s is kept as a still picture, which says nothing moves: %s", CodeSVGAnimation, a.name, reason))
			out, origin = append(out, s), append(origin, i)
			continue
		}
		first, err := gif.Decode(bytes.NewReader(f.data))
		if err != nil {
			p.sink.note(a, fmt.Sprintf("%s: %s is kept as a still picture: %v", CodeSVGAnimation, a.name, err))
			out, origin = append(out, s), append(origin, i)
			continue
		}
		msg := fmt.Sprintf("%s: %s is an animated GIF of %d frames at %.0f fps, %dx%d pixels, %d KiB: %s", CodeSVGAnimation, a.name, f.frames, f.fps, f.w, f.h, len(f.data)>>10, why)
		if f.note != "" {
			msg += " (" + f.note + ")"
		}
		p.sink.note(a, msg)
		out = append(out, officecanvas.Shape{Kind: officecanvas.KindImage, Bounds: s.Dst, Image: &gifPicture{Image: first, data: f.data}})
		origin = append(origin, i)
	}
	return out, origin, fx
}

// sortedNotes are the notes, in a stable order.
func (s *svgSink) sortedNotes() []string {
	out := make([]string, 0, len(s.notes))
	for _, n := range s.notes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
