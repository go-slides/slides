package export

import (
	"fmt"
	"slices"
	"strings"

	"github.com/go-pdfkit/pdfkit"

	"github.com/go-slides/slides/doc"
)

// Slide transitions in the exports. marp-cli's 33 built-in transitions play in
// the HTML viewer; a PDF, a PowerPoint deck and an OpenDocument deck have
// their own, fewer, and each Marp transition is mapped to the closest one
// there. This file is the one place that mapping lives: the table below,
// tested against what each format's specification allows (animation_test.go).
//
//   - PPTX: the p:transition children of ECMA-376 Part 1 19.5 (PresentationML,
//     CT_SlideTransition), with spd for the speed.
//   - ODP: smil:type, smil:subtype and smil:direction on the page's
//     style:drawing-page-properties (OpenDocument 1.3 Part 3 20.242-20.245),
//     the names being SMIL 2.0's transition taxonomy (SMIL20 12.8.1).
//   - PDF: the /Trans dictionary of PDF 32000-1 12.4.4.1, Table 162.
//
// A mapping is Exact when the target effect is the same movement (a push is a
// push, a fade a fade); Approx when it is the nearest one the format has
// (Marp's cube, whose faces turn, is a push). Direction is part of it: PDF
// reads Push, Cover and Uncover in two directions only (left to right and top
// to bottom), so a Marp push, which moves right to left, is Approx in PDF.

// Fit says how well a format's transition stands for Marp's.
type Fit int

const (
	// None: the format has no transition for it (not used by the table: every
	// Marp transition has a nearest one).
	None Fit = iota
	// Exact: the same movement.
	Exact
	// Approx: the nearest the format has.
	Approx
)

func (f Fit) String() string {
	switch f {
	case Exact:
		return "exact"
	case Approx:
		return "approximate"
	}
	return "none"
}

// PPTXEffect is a p:transition child: Element with the attributes Attr, as
// name/value pairs in schema order.
type PPTXEffect struct {
	Element string
	Attr    [][2]string
}

// ODPEffect is a SMIL transition: smil:type, smil:subtype and, when Reverse,
// smil:direction="reverse".
type ODPEffect struct {
	Type, Subtype string
	Reverse       bool
}

// PDFEffect is a /Trans dictionary less its duration.
type PDFEffect struct {
	Style     pdfkit.TransitionStyle
	Direction int
	Vertical  bool
	Outward   bool
}

// Effect is what one Marp transition becomes in each format.
type Effect struct {
	PPTX    PPTXEffect
	PPTXFit Fit
	ODP     ODPEffect
	ODPFit  Fit
	PDF     PDFEffect
	PDFFit  Fit
}

func attrs(kv ...string) [][2]string {
	var out [][2]string
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, [2]string{kv[i], kv[i+1]})
	}
	return out
}

// fadeEffect stands in for a transition with no equivalent at all: a custom
// one (the deck's own keyframes) or a name nothing defines.
var fadeEffect = Effect{
	PPTXEffect{"fade", nil}, Approx,
	ODPEffect{"fade", "crossfade", false}, Approx,
	PDFEffect{Style: pdfkit.TransitionFade}, Approx,
}

// transitionEffects maps marp-cli's built-in transition names (the same
// names as deck.BuiltinTransitionNames) to their closest effects.
var transitionEffects = map[string]Effect{
	"clockwise": {
		PPTXEffect{"wheel", attrs("spokes", "1")}, Exact,
		ODPEffect{"clockWipe", "clockwiseTwelve", false}, Exact,
		PDFEffect{Style: pdfkit.TransitionWipe}, Approx},
	"counterclockwise": {
		PPTXEffect{"wheel", attrs("spokes", "1")}, Approx, // the schema has no reverse wheel
		ODPEffect{"clockWipe", "clockwiseTwelve", true}, Exact,
		PDFEffect{Style: pdfkit.TransitionWipe, Direction: 180}, Approx},
	"cover": {
		PPTXEffect{"cover", attrs("dir", "l")}, Approx, // Marp's also fades in
		ODPEffect{"slideWipe", "fromRight", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionCover}, Approx},
	"coverflow": {
		PPTXEffect{"push", attrs("dir", "l")}, Approx,
		ODPEffect{"pushWipe", "fromRight", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionPush}, Approx},
	"cube": {
		PPTXEffect{"push", attrs("dir", "l")}, Approx,
		ODPEffect{"pushWipe", "fromRight", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionPush}, Approx},
	"cylinder": {
		PPTXEffect{"push", attrs("dir", "l")}, Approx,
		ODPEffect{"pushWipe", "fromRight", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionPush}, Approx},
	"diamond": {
		PPTXEffect{"diamond", nil}, Exact,
		ODPEffect{"irisWipe", "diamond", false}, Exact,
		PDFEffect{Style: pdfkit.TransitionBox, Outward: true}, Approx},
	"drop": {
		PPTXEffect{"push", attrs("dir", "d")}, Approx,
		ODPEffect{"pushWipe", "fromTop", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionPush, Direction: 270}, Approx},
	"explode": {
		PPTXEffect{"zoom", attrs("dir", "out")}, Approx,
		ODPEffect{"fade", "crossfade", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionFade}, Approx},
	"fade": {
		PPTXEffect{"fade", nil}, Exact,
		ODPEffect{"fade", "crossfade", false}, Exact,
		PDFEffect{Style: pdfkit.TransitionFade}, Exact},
	"fade-out": {
		PPTXEffect{"fade", attrs("thruBlk", "1")}, Approx,
		ODPEffect{"fade", "fadeToColor", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionFade}, Approx},
	"fall": {
		PPTXEffect{"pull", attrs("dir", "d")}, Approx,
		ODPEffect{"slideWipe", "fromTop", true}, Approx,
		PDFEffect{Style: pdfkit.TransitionUncover, Direction: 270}, Approx},
	"flip": {
		PPTXEffect{"fade", nil}, Approx,
		ODPEffect{"fade", "crossfade", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionFade}, Approx},
	"glow": {
		PPTXEffect{"fade", attrs("thruBlk", "1")}, Approx,
		ODPEffect{"fade", "fadeToColor", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionFade}, Approx},
	"implode": {
		PPTXEffect{"zoom", attrs("dir", "in")}, Approx,
		ODPEffect{"ellipseWipe", "circle", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionBox}, Approx},
	"in-out": {
		PPTXEffect{"split", attrs("orient", "vert", "dir", "in")}, Approx,
		ODPEffect{"barnDoorWipe", "vertical", true}, Approx,
		PDFEffect{Style: pdfkit.TransitionSplit, Vertical: true}, Approx},
	"iris-in": {
		PPTXEffect{"circle", nil}, Exact,
		ODPEffect{"ellipseWipe", "circle", false}, Exact,
		PDFEffect{Style: pdfkit.TransitionBox, Outward: true}, Approx},
	"iris-out": {
		PPTXEffect{"circle", nil}, Approx, // the schema's circle only opens
		ODPEffect{"ellipseWipe", "circle", true}, Exact,
		PDFEffect{Style: pdfkit.TransitionBox}, Approx},
	"melt": {
		PPTXEffect{"wipe", attrs("dir", "d")}, Approx,
		ODPEffect{"barWipe", "topToBottom", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionWipe, Direction: 270}, Approx},
	"overlap": {
		PPTXEffect{"fade", nil}, Approx,
		ODPEffect{"fade", "crossfade", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionFade}, Approx},
	"pivot": {
		PPTXEffect{"wedge", nil}, Approx,
		ODPEffect{"singleSweepWipe", "clockwiseTopLeft", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionWipe}, Approx},
	"pull": {
		PPTXEffect{"pull", attrs("dir", "l")}, Exact,
		ODPEffect{"slideWipe", "fromLeft", true}, Exact,
		PDFEffect{Style: pdfkit.TransitionUncover}, Approx},
	"push": {
		PPTXEffect{"push", attrs("dir", "l")}, Exact,
		ODPEffect{"pushWipe", "fromRight", false}, Exact,
		PDFEffect{Style: pdfkit.TransitionPush}, Approx},
	"reveal": {
		PPTXEffect{"pull", attrs("dir", "l")}, Approx, // Marp's also fades the old slide
		ODPEffect{"slideWipe", "fromLeft", true}, Approx,
		PDFEffect{Style: pdfkit.TransitionUncover}, Approx},
	"rotate": {
		PPTXEffect{"fade", nil}, Approx,
		ODPEffect{"fade", "crossfade", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionFade}, Approx},
	"slide": {
		PPTXEffect{"push", attrs("dir", "l")}, Exact,
		ODPEffect{"pushWipe", "fromRight", false}, Exact,
		PDFEffect{Style: pdfkit.TransitionPush}, Approx},
	"star": {
		PPTXEffect{"plus", nil}, Approx,
		ODPEffect{"starWipe", "fivePoint", false}, Exact,
		PDFEffect{Style: pdfkit.TransitionBox, Outward: true}, Approx},
	"swap": {
		PPTXEffect{"fade", nil}, Approx,
		ODPEffect{"fade", "crossfade", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionFade}, Approx},
	"swipe": {
		PPTXEffect{"pull", attrs("dir", "lu")}, Approx,
		ODPEffect{"diagonalWipe", "topLeft", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionUncover}, Approx},
	"swoosh": {
		PPTXEffect{"zoom", attrs("dir", "in")}, Approx,
		ODPEffect{"ellipseWipe", "circle", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionBox, Outward: true}, Approx},
	"wipe": {
		PPTXEffect{"wipe", attrs("dir", "r")}, Exact,
		ODPEffect{"barWipe", "leftToRight", false}, Exact,
		PDFEffect{Style: pdfkit.TransitionWipe}, Exact},
	"wiper": {
		PPTXEffect{"wedge", nil}, Approx,
		ODPEffect{"windshieldWipe", "right", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionWipe}, Approx},
	"zoom": {
		PPTXEffect{"zoom", attrs("dir", "in")}, Approx, // Marp's also fades in
		ODPEffect{"irisWipe", "rectangle", false}, Approx,
		PDFEffect{Style: pdfkit.TransitionBox, Outward: true}, Approx},
}

// EffectOf is what the transition becomes in each format: the entry of a
// built-in name, or fade, Approx everywhere, for a custom or unknown one.
func EffectOf(t doc.Transition) Effect {
	if !t.Custom {
		if e, ok := transitionEffects[t.Name]; ok {
			return e
		}
	}
	return fadeEffect
}

// defaultTransitionSeconds is marp-cli's default duration of a transition.
const defaultTransitionSeconds = 0.5

func seconds(t doc.Transition) float64 {
	if t.Duration > 0 {
		return t.Duration
	}
	return defaultTransitionSeconds
}

// speed is the coarse speed PowerPoint (spd) and OpenDocument
// (presentation:transition-speed) keep: fast up to half a second, medium up
// to a second, slow beyond.
func speed(t doc.Transition) int {
	switch s := seconds(t); {
	case s <= 0.5:
		return 0
	case s <= 1:
		return 1
	}
	return 2
}

// Substitutions returns the warning of an export in format ("pptx", "odp" or
// "pdf"): one line listing each transition the deck uses that the format
// cannot say exactly, with what it was replaced by, or nil when every
// transition is exact. A custom transition — the deck's own keyframes, or a
// name nothing defines — is a fade.
func Substitutions(b *doc.Doc, format string) []string {
	var subs []string
	seen := map[string]bool{}
	for _, s := range b.Slides {
		t := s.Transition
		if t == nil {
			continue
		}
		e := EffectOf(*t)
		var fit Fit
		var to string
		switch format {
		case "pptx":
			fit, to = e.PPTXFit, e.PPTX.String()
		case "odp":
			fit, to = e.ODPFit, e.ODP.String()
		case "pdf":
			fit, to = e.PDFFit, e.PDF.String()
		default:
			return nil
		}
		if fit == Exact || seen[t.Name] {
			continue
		}
		seen[t.Name] = true
		name := t.Name
		if t.Custom {
			name += " (custom)"
		}
		subs = append(subs, name+" → "+to)
	}
	if len(subs) == 0 {
		return nil
	}
	slices.Sort(subs)
	return []string{fmt.Sprintf("%s has no exact equivalent for %d transition(s), which became the nearest: %s",
		strings.ToUpper(format), len(subs), strings.Join(subs, ", "))}
}

func (e PPTXEffect) String() string {
	s := e.Element
	for _, a := range e.Attr {
		s += " " + a[0] + "=" + a[1]
	}
	return s
}

func (e ODPEffect) String() string {
	s := e.Type + "/" + e.Subtype
	if e.Reverse {
		s += " reversed"
	}
	return s
}

func (e PDFEffect) String() string {
	s := string(e.Style)
	if e.Direction != 0 {
		s += fmt.Sprintf(" Di=%d", e.Direction)
	}
	return s
}

// Coverage counts the built-in transitions by how each format stands for them.
func Coverage(format string) (exact, approx int) {
	for _, e := range transitionEffects {
		var f Fit
		switch format {
		case "pptx":
			f = e.PPTXFit
		case "odp":
			f = e.ODPFit
		case "pdf":
			f = e.PDFFit
		}
		switch f {
		case Exact:
			exact++
		case Approx:
			approx++
		}
	}
	return
}
