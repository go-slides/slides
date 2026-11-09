package export

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/go-gfx/oksvg"
	"github.com/go-gfx/oksvg/svganim"
)

// How an animated SVG is carried: the plan.
//
// Native means every animated element of the drawing becomes a shape of its
// own and its animation one the slide format says exactly:
//
//   - the element's net movement (every transform animation on it: translate,
//     rotate, scale, animateMotion, the CSS transform) is sampled and taken
//     apart into the movement of its centre, a rotation about the centre and
//     a scale about the centre; each is a behaviour of PowerPoint (p:animMotion,
//     p:animRot, p:animScale, or p:anim with a time-value list when the
//     samples are not a straight line) and of OpenDocument (anim:animateMotion,
//     anim:animateTransform, anim:animate). When the net movement is not such
//     a composition (a skew, a reflection, a non-uniform scale under a
//     rotation, a rotation about a point the sampling cannot follow) it is
//     not native;
//   - opacity: p:anim style.opacity, a time-value list, on a one-shape element;
//   - fill and stroke colours: p:animClr between two colours, on a one-shape
//     element that has only that paint.
//
// Anything else (a path that morphs, a stroke that draws itself, a size
// change of a rect, events, several overlapping animations of one property)
// makes the whole image frames: rendered at the animation's own times and
// encoded as an animated GIF.

// Tolerances of the sampling, and its bounds.
const (
	tolPx       = 0.25  // a slide pixel
	tolAngle    = 0.25  // degrees
	tolScale    = 0.002 // a ratio
	maxTargets  = 64    // animated elements a native plan takes
	maxTracks   = 256   // animations a native plan takes
	sampleGrid  = 64    // uniform samples of one iteration
	maxGIFPix   = 16e6  // pixels over all the frames of a GIF
	maxGIFWidth = 1024  // pixels
	gifFPS      = 12    // frames per second
)

type behKind int

const (
	behMotion behKind = iota
	behRotate
	behScale
	behOpacity
	behFill
	behStroke
)

func (k behKind) String() string {
	return [...]string{"translate/motion", "rotate", "scale", "opacity", "fill", "stroke"}[k]
}

// svgTiming is the timing every behaviour of one effect shares.
type svgTiming struct {
	begin, dur float64 // seconds
	count      float64 // iterations of the simple duration; +Inf: indefinite
	autoRev    bool    // each cycle runs forward, then back
	freeze     bool
	discrete   bool
}

// cycles is the repeat count in PowerPoint's terms: a cycle is a forward run
// and, with autoRev, the run back.
func (t svgTiming) cycles() float64 {
	if t.autoRev {
		return t.count / 2
	}
	return t.count
}

// cycleMS is the duration of one cycle's forward run, in milliseconds.
func (t svgTiming) durMS() int { return int(math.Round(t.dur * 1000)) }

// svgBeh is one behaviour of a target: its samples over one iteration of the
// timing, normalised to 0..1, in viewBox units for lengths. motion: (dx, dy);
// rotate: (degrees, 0); scale: (sx, sy); opacity: (o, 0); colours: (r, g, b)
// as 0..255.
type svgBeh struct {
	kind behKind
	t    svgTiming
	ts   []float64
	v    [][3]float64
}

// straight reports two samples that say it all: the behaviour is a straight
// line over the iteration, at constant speed.
func (b svgBeh) straight() bool { return len(b.ts) == 2 && b.ts[0] == 0 && b.ts[1] == 1 }

// svgTarget is an animated element: its centre in viewBox units and what it
// does.
type svgTarget struct {
	key     int
	id      string
	cx, cy  float64
	members int
	beh     []svgBeh
}

// svgPlan is how an image is carried.
type svgPlan struct {
	native  bool
	why     []string // for a frames plan: what native could not say
	targets []*svgTarget
	byKey   map[int]*svgTarget
	kinds   []string
	// viewBox is the drawing's, in its own units.
	viewBox [4]float64
}

// getPlan makes the plan once.
func (s *animSVG) getPlan() *svgPlan {
	s.planOnce.Do(func() { s.plan = s.buildPlan() })
	return s.plan
}

func (s *animSVG) buildPlan() *svgPlan {
	p := &svgPlan{byKey: map[int]*svgTarget{}}
	fail := func(format string, args ...any) *svgPlan {
		p.native = false
		p.why = append(p.why, fmt.Sprintf(format, args...))
		return p
	}
	an := s.anim
	targets := an.Targets()
	if len(targets) > maxTargets || len(an.Tracks) > maxTracks {
		return fail("it animates %d elements with %d animations: more than a slide takes (%d and %d)", len(targets), len(an.Tracks), maxTargets, maxTracks)
	}
	icon, err := oksvg.ReadIconStream(bytes.NewReader(rootFixed(s.initial)), oksvg.IgnoreErrorMode)
	if err != nil || icon.ViewBox.W <= 0 || icon.ViewBox.H <= 0 {
		return fail("the drawing cannot be read as shapes")
	}
	shapes, exact := icon.Shapes()
	if !exact {
		return fail("the drawing has no exact vector form (a clip, a fading or radial gradient)")
	}
	p.viewBox = [4]float64{icon.ViewBox.X, icon.ViewBox.Y, icon.ViewBox.W, icon.ViewBox.H}
	for i, a := range targets {
		for _, b := range targets[i+1:] {
			if an.Ancestor(a, b) || an.Ancestor(b, a) {
				return fail("an animated element holds another animated element (#%d and #%d)", a, b)
			}
		}
	}
	bbox := bboxOf(an, s.svg)
	for _, key := range targets {
		t := &svgTarget{key: key, id: an.Elements[key].ID}
		x, y, w, h, ok := svganim.ElementBounds(shapes, key)
		if !ok {
			return fail("%s draws nothing", describeElement(an, key))
		}
		t.cx, t.cy = x+w/2, y+h/2
		for _, sh := range shapes {
			if slices.Contains(sh.Keys, key) {
				t.members++
			}
		}
		var tf, geo, others []*svganim.Track
		for i := range an.Tracks {
			tr := &an.Tracks[i]
			if tr.Element != key {
				continue
			}
			if tr.Name == "transform" || tr.Name == "motion" {
				tf = append(tf, tr)
			} else if isGeometry(an, key, tr.Name) {
				geo = append(geo, tr)
			} else {
				others = append(others, tr)
			}
		}
		if len(tf) > 0 {
			if why := s.transformBehaviours(t, tf, bbox); why != "" {
				return fail("%s: %s", describeElement(an, key), why)
			}
		}
		if len(geo) > 0 {
			if len(tf) > 0 {
				return fail("%s: its size and its movement are animated together", describeElement(an, key))
			}
			if why := s.geometryBehaviours(t, geo, bbox); why != "" {
				return fail("%s: %s", describeElement(an, key), why)
			}
		}
		seen := map[string]bool{}
		for _, tr := range others {
			if seen[tr.Name] {
				return fail("%s: %s is animated more than once", describeElement(an, key), tr.Name)
			}
			seen[tr.Name] = true
			if why := s.paintBehaviour(t, tr, bbox); why != "" {
				return fail("%s: %s", describeElement(an, key), why)
			}
		}
		p.targets = append(p.targets, t)
		p.byKey[key] = t
	}
	p.native = true
	seen := map[string]bool{}
	for _, t := range p.targets {
		for _, b := range t.beh {
			if k := b.kind.String(); !seen[k] {
				seen[k] = true
				p.kinds = append(p.kinds, k)
			}
		}
	}
	return p
}

func describeElement(an *svganim.Animations, key int) string {
	e := an.Elements[key]
	if e.ID != "" {
		return "<" + e.Name + " id=" + e.ID + ">"
	}
	return fmt.Sprintf("<%s> (#%d)", e.Name, key)
}

// timingOf reads a track's timing, or says why PowerPoint and OpenDocument
// cannot have it.
func timingOf(tr *svganim.Track) (svgTiming, bool, string) {
	t := svgTiming{begin: tr.Begin, dur: tr.Dur, freeze: tr.Freeze}
	if math.IsInf(tr.Dur, 1) || tr.Dur <= 0 {
		return t, false, "it has no simple duration (a set)"
	}
	if tr.Begin < 0 {
		return t, false, "it begins before the slide does (a negative delay)"
	}
	ad := tr.ActiveDur()
	t.count = ad / tr.Dur
	if math.IsInf(ad, 1) {
		t.count = math.Inf(1)
	}
	rev := false
	switch tr.Direction {
	case svganim.Reverse:
		rev = true
	case svganim.Alternate:
		t.autoRev = true
	case svganim.AlternateReverse:
		t.autoRev, rev = true, true
	}
	if t.autoRev && !math.IsInf(t.count, 1) && math.Abs(t.count/2-math.Round(t.count/2)) > 1e-9 {
		return t, rev, "it alternates for an odd number of iterations"
	}
	if !math.IsInf(t.count, 1) && math.Abs(t.count-math.Round(t.count)) > 1e-9 {
		return t, rev, "it repeats a fractional number of times"
	}
	t.discrete = tr.Calc == svganim.Discrete
	return t, rev, ""
}

// grid is the times to sample an iteration at: evenly, and at every key time.
func grid(tracks []*svganim.Track) []float64 {
	g := make([]float64, 0, sampleGrid+1)
	for i := 0; i <= sampleGrid; i++ {
		g = append(g, float64(i)/sampleGrid)
	}
	for _, tr := range tracks {
		g = append(g, tr.KeyTimes...)
	}
	sort.Float64s(g)
	out := g[:0]
	for _, v := range g {
		if v < 0 || v > 1 || (len(out) > 0 && v-out[len(out)-1] < 1e-6) {
			continue
		}
		out = append(out, v)
	}
	return out
}

// tAt is the document time of position p of the first iteration, after the
// direction. It stays just inside the iteration.
func tAt(t svgTiming, rev bool, p float64) float64 {
	if rev {
		p = 1 - p
	}
	return t.begin + math.Min(p, 1-1e-9)*t.dur
}

func unwrapAngle(prev, a float64) float64 {
	for a-prev > 180 {
		a -= 360
	}
	for a-prev < -180 {
		a += 360
	}
	return a
}

// transformBehaviours samples the net transform of the element and splits it
// into motion, rotation and scale about the element's centre.
func (s *animSVG) transformBehaviours(t *svgTarget, tf []*svganim.Track, bbox svganim.BBoxFunc) string {
	an := s.anim
	tm, rev, why := timingOf(tf[0])
	if why != "" {
		return why
	}
	for _, tr := range tf[1:] {
		o, orev, why := timingOf(tr)
		if why != "" {
			return why
		}
		if o != tm || orev != rev {
			return "its transforms run on different clocks"
		}
	}
	if tm.discrete {
		return "it moves in jumps (calcMode discrete)"
	}
	ctm := an.ParentCTM(t.key)
	ctmInv, ok := ctm.Inverse()
	if !ok {
		return "its parent is flattened by a transform"
	}
	local := func(st map[int]*svganim.State) svganim.Matrix {
		if s := st[t.key]; s != nil && s.Transform != nil {
			return *s.Transform
		}
		return an.OwnTransform(t.key)
	}
	ref := local(an.Eval(0, bbox))
	refInv, ok := ref.Inverse()
	if !ok {
		return "its transform is singular"
	}
	// the net movement at each sample, in the root user space
	g := grid(tf)
	type sample struct {
		d      [2]float64
		th     float64
		sx, sy float64
	}
	samples := make([]sample, len(g))
	var prevTh float64
	for i, p := range g {
		L := local(an.Eval(tAt(tm, rev, p), bbox))
		X := ctm.Mul(L).Mul(refInv).Mul(ctmInv)
		ix, iy := X.Apply(t.cx, t.cy)
		a, b, c, d := X[0], X[1], X[2], X[3]
		sx := math.Hypot(a, b)
		if sx < 1e-9 {
			return "it is scaled down to nothing"
		}
		sy := (a*d - b*c) / sx
		if sy <= 0 {
			return "it is reflected"
		}
		if math.Abs(a*c+b*d)/(sx*sy) > 1e-3 {
			return "it is skewed"
		}
		th := math.Atan2(b, a) * 180 / math.Pi
		if i > 0 {
			th = unwrapAngle(prevTh, th)
			if math.Abs(th-prevTh) > 90 {
				return "it turns faster than the sampling can follow"
			}
		}
		prevTh = th
		samples[i] = sample{[2]float64{ix - t.cx, iy - t.cy}, th, sx, sy}
	}
	if !(samples[0].d[0] == 0 && samples[0].d[1] == 0) {
		s0 := samples[0]
		if math.Hypot(s0.d[0], s0.d[1]) > 1e-6 || math.Abs(s0.th) > 1e-6 || math.Abs(s0.sx-1) > 1e-6 || math.Abs(s0.sy-1) > 1e-6 {
			return "it begins away from where it is drawn (the first value is not the drawn state)"
		}
	}
	// scale of the drawing in slide pixels per viewBox unit is applied by the
	// writers; the tolerances here are in viewBox units for lengths, given by
	// the 1:1 reading, and tightened by the writers' own check (svgEffects).
	var motion, rot, scl bool
	for _, sm := range samples {
		if math.Hypot(sm.d[0], sm.d[1]) > 1e-4 {
			motion = true
		}
		if math.Abs(sm.th) > tolAngle/4 {
			rot = true
		}
		if math.Abs(sm.sx-1) > tolScale/4 || math.Abs(sm.sy-1) > tolScale/4 {
			scl = true
		}
	}
	if rot && scl {
		for _, sm := range samples {
			if math.Abs(sm.sx-sm.sy) > tolScale {
				return "it is scaled unevenly while it turns"
			}
		}
	}
	mk := func(kind behKind, val func(sample) [3]float64, tol func([3]float64, [3]float64) float64, limit float64) {
		ts := make([]float64, len(g))
		vs := make([][3]float64, len(g))
		for i := range g {
			ts[i], vs[i] = g[i], val(samples[i])
		}
		ts, vs = simplify(ts, vs, tol, limit)
		t.beh = append(t.beh, svgBeh{kind: kind, t: tm, ts: ts, v: vs})
	}
	dist := func(a, b [3]float64) float64 { return math.Hypot(a[0]-b[0], a[1]-b[1]) }
	if motion {
		// 1/20 of a viewBox unit is far below a slide pixel for any drawing
		// the exports place; the writers refuse a result that strays more
		mk(behMotion, func(s sample) [3]float64 { return [3]float64{s.d[0], s.d[1]} }, dist, 1e-3*math.Max(s.viewSpan(), 1))
	}
	if rot {
		mk(behRotate, func(s sample) [3]float64 { return [3]float64{s.th} }, func(a, b [3]float64) float64 { return math.Abs(a[0] - b[0]) }, tolAngle)
	}
	if scl {
		mk(behScale, func(s sample) [3]float64 { return [3]float64{s.sx, s.sy} }, func(a, b [3]float64) float64 { return math.Hypot(a[0]-b[0], a[1]-b[1]) }, tolScale)
	}
	return ""
}

// viewSpan is the larger side of the drawing's viewBox.
func (s *animSVG) viewSpan() float64 {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(rootFixed(s.initial)), oksvg.IgnoreErrorMode)
	if err != nil {
		return 1
	}
	return math.Max(icon.ViewBox.W, icon.ViewBox.H)
}

// simplify drops the samples a straight line between the kept ones reproduces
// within limit (Ramer-Douglas-Peucker over time and value). dist measures the
// difference of two values.
func simplify(ts []float64, vs [][3]float64, dist func(a, b [3]float64) float64, limit float64) ([]float64, [][3]float64) {
	n := len(ts)
	if n <= 2 {
		return ts, vs
	}
	keep := make([]bool, n)
	keep[0], keep[n-1] = true, true
	var rec func(lo, hi int)
	rec = func(lo, hi int) {
		worst, at := 0.0, -1
		for i := lo + 1; i < hi; i++ {
			f := (ts[i] - ts[lo]) / (ts[hi] - ts[lo])
			var lerp [3]float64
			for k := range lerp {
				lerp[k] = vs[lo][k] + (vs[hi][k]-vs[lo][k])*f
			}
			if d := dist(lerp, vs[i]); d > worst {
				worst, at = d, i
			}
		}
		if at >= 0 && worst > limit {
			keep[at] = true
			rec(lo, at)
			rec(at, hi)
		}
	}
	rec(0, n-1)
	var ot []float64
	var ov [][3]float64
	for i := range ts {
		if keep[i] {
			ot, ov = append(ot, ts[i]), append(ov, vs[i])
		}
	}
	return ot, ov
}

// paintBehaviour reads an opacity, fill or stroke animation.
func (s *animSVG) paintBehaviour(t *svgTarget, tr *svganim.Track, bbox svganim.BBoxFunc) string {
	an := s.anim
	tm, rev, why := timingOf(tr)
	if why != "" {
		return why
	}
	switch tr.Name {
	case "opacity":
		if t.members != 1 {
			return "its opacity is animated on a drawing of several shapes (a slide fades one shape)"
		}
		if tr.Sum {
			return "its opacity is added to"
		}
		if v, ok := baseNumber(an, t.key, "opacity", 1); !ok || math.Abs(v-1) > 1e-9 {
			return "it is not fully opaque to begin with"
		}
		g := grid([]*svganim.Track{tr})
		ts := make([]float64, len(g))
		vs := make([][3]float64, len(g))
		for i, p := range g {
			st := an.Eval(tAt(tm, rev, p), bbox)[t.key]
			o := 1.0
			if st != nil {
				if v, ok := st.Attrs["opacity"]; ok && len(v.Nums) == 1 {
					o = v.Nums[0]
				}
			}
			ts[i], vs[i] = p, [3]float64{math.Max(0, math.Min(1, o))}
		}
		if !tm.discrete {
			ts, vs = simplify(ts, vs, func(a, b [3]float64) float64 { return math.Abs(a[0] - b[0]) }, 0.005)
		}
		if first := vs[0][0]; math.Abs(first-1) > 1e-9 && tm.begin == 0 {
			// the drawn state is the state at t=0, already applied
			_ = first
		}
		t.beh = append(t.beh, svgBeh{kind: behOpacity, t: tm, ts: ts, v: vs})
		return ""
	case "fill", "stroke":
		if t.members != 1 {
			return "its " + tr.Name + " is animated on a drawing of several shapes"
		}
		if len(an.Elements) > 0 {
			other := "stroke"
			if tr.Name == "stroke" {
				other = "fill"
			}
			if !paintIsNone(an, t.key, other, tr.Name == "stroke") {
				return "its " + tr.Name + " is animated while it also has a " + other + ", which a one-shape slide element cannot tell apart"
			}
		}
		if tr.Sum || tm.discrete {
			return "its " + tr.Name + " is added to or stepped"
		}
		if len(tr.Values) != 2 || tr.Values[0].Color == nil || tr.Values[1].Color == nil {
			return "its " + tr.Name + " does not go from one colour to another"
		}
		if !linearEasing(tr) {
			return "its " + tr.Name + " eases"
		}
		c0, c1 := tr.Values[0].Color, tr.Values[1].Color
		beh := svgBeh{kind: behFill, t: tm, ts: []float64{0, 1}}
		if tr.Name == "stroke" {
			beh.kind = behStroke
		}
		beh.v = [][3]float64{{float64(c0.R), float64(c0.G), float64(c0.B)}, {float64(c1.R), float64(c1.G), float64(c1.B)}}
		if rev {
			beh.v[0], beh.v[1] = beh.v[1], beh.v[0]
		}
		t.beh = append(t.beh, beh)
		return ""
	}
	return "it animates " + tr.Name + " (a " + tr.Source.String() + " animation of a property a slide has no effect for)"
}

// baseNumber is the number an attribute has on the element, or def.
func baseNumber(an *svganim.Animations, key int, name string, def float64) (float64, bool) {
	if v, ok := an.Elements[key].Attrs[name]; ok {
		p := svganim.ParseValue(v)
		if len(p.Nums) == 1 {
			return p.Nums[0], true
		}
		return 0, false
	}
	return def, true
}

// paintIsNone reports that the named paint of the element is none, or, when
// wantFillNone is false, that it is not set (the stroke's initial value).
func paintIsNone(an *svganim.Animations, key int, name string, nameIsFill bool) bool {
	for k := key; k >= 0; k = an.Elements[k].Parent {
		at := an.Elements[k].Attrs
		if v, ok := at[name]; ok {
			return strings.TrimSpace(v) == "none"
		}
		for _, d := range strings.Split(at["style"], ";") {
			if a, b, ok := strings.Cut(d, ":"); ok && strings.TrimSpace(a) == name {
				return strings.TrimSpace(b) == "none"
			}
		}
	}
	// not set anywhere: a fill is black, a stroke is none
	return name == "stroke"
}

// linearEasing reports a track whose every interval is linear.
func linearEasing(tr *svganim.Track) bool {
	for _, e := range tr.Easing {
		if !e.IsLinear() {
			return false
		}
	}
	return true
}

// isGeometry reports an animation of a size a slide shape can follow by
// scaling: the radius of a circle, the width and height of a rectangle.
func isGeometry(an *svganim.Animations, key int, name string) bool {
	switch an.Elements[key].Name {
	case "circle":
		return name == "r"
	case "rect":
		return name == "width" || name == "height"
	}
	return false
}

// geometryBehaviours reads the animation of a circle's radius or a
// rectangle's size as the scale of the shape about its centre, and, for a
// rectangle, the movement of the centre that its fixed corner makes (a
// rectangle that grows to the right and down has its centre go there too). It
// is exact for a shape with no stroke (a stroke would scale with it) and no
// rounded corners.
func (s *animSVG) geometryBehaviours(t *svgTarget, geo []*svganim.Track, bbox svganim.BBoxFunc) string {
	an := s.anim
	tm, rev, why := timingOf(geo[0])
	if why != "" {
		return why
	}
	for _, tr := range geo[1:] {
		o, orev, why := timingOf(tr)
		if why != "" {
			return why
		}
		if o != tm || orev != rev {
			return "its sizes run on different clocks"
		}
	}
	if tm.discrete {
		return "its size changes in jumps (calcMode discrete)"
	}
	e := an.Elements[t.key]
	if !paintIsNone(an, t.key, "stroke", false) {
		return "it has a stroke, which a scaled shape would scale too"
	}
	if e.Attrs["rx"] != "" || e.Attrs["ry"] != "" || e.Attrs["transform"] != "" {
		return "it has rounded corners or a transform of its own"
	}
	num := func(st map[int]*svganim.State, name string) (float64, bool) {
		if s := st[t.key]; s != nil {
			if v, ok := s.Attrs[name]; ok {
				if len(v.Nums) != 1 || (v.Skel[1] != "" && v.Skel[1] != "px") {
					return 0, false
				}
				return v.Nums[0], true
			}
		}
		v := svganim.ParseValue(e.Attrs[name])
		if len(v.Nums) != 1 || (v.Skel[1] != "" && v.Skel[1] != "px") {
			return 0, false
		}
		return v.Nums[0], true
	}
	names := []string{"r"}
	if e.Name == "rect" {
		names = []string{"width", "height"}
	}
	st0 := an.Eval(0, bbox)
	ref := make([]float64, len(names))
	for i, n := range names {
		v, ok := num(st0, n)
		if !ok || v <= 0 {
			return "its " + n + " is not a plain length"
		}
		ref[i] = v
	}
	g := grid(geo)
	ts := make([]float64, len(g))
	scale := make([][3]float64, len(g))
	move := make([][3]float64, len(g))
	var moves, scales bool
	for i, p := range g {
		st := an.Eval(tAt(tm, rev, p), bbox)
		cur := make([]float64, len(names))
		for k, n := range names {
			v, ok := num(st, n)
			if !ok || v < 0 {
				return "its " + n + " is not a plain length"
			}
			cur[k] = v
		}
		ts[i] = p
		if e.Name == "circle" {
			scale[i] = [3]float64{cur[0] / ref[0], cur[0] / ref[0]}
		} else {
			scale[i] = [3]float64{cur[0] / ref[0], cur[1] / ref[1]}
			move[i] = [3]float64{(cur[0] - ref[0]) / 2, (cur[1] - ref[1]) / 2}
		}
		if math.Abs(scale[i][0]-1) > tolScale/4 || math.Abs(scale[i][1]-1) > tolScale/4 {
			scales = true
		}
		if math.Hypot(move[i][0], move[i][1]) > 1e-4 {
			moves = true
		}
		if scale[i][0] < 1e-3 || scale[i][1] < 1e-3 {
			return "it shrinks to nothing"
		}
	}
	if scale[0] != [3]float64{1, 1} && math.Abs(scale[0][0]-1)+math.Abs(scale[0][1]-1) > 1e-6 {
		return "it begins away from where it is drawn (the first value is not the drawn state)"
	}
	if moves {
		mts, mvs := simplify(slicesClone(ts), slicesClone(move), func(a, b [3]float64) float64 { return math.Hypot(a[0]-b[0], a[1]-b[1]) }, 1e-3*math.Max(s.viewSpan(), 1))
		t.beh = append(t.beh, svgBeh{kind: behMotion, t: tm, ts: mts, v: mvs})
	}
	if scales {
		sts, svs := simplify(slicesClone(ts), slicesClone(scale), func(a, b [3]float64) float64 { return math.Hypot(a[0]-b[0], a[1]-b[1]) }, tolScale)
		t.beh = append(t.beh, svgBeh{kind: behScale, t: tm, ts: sts, v: svs})
	}
	return ""
}

func slicesClone[T any](s []T) []T { return append([]T(nil), s...) }
