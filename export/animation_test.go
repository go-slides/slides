package export

import (
	"encoding/xml"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/go-pdfkit/pdfkit"

	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/doc"
)

// pptxElements are the transition children CT_SlideTransition allows, with the
// attributes each takes and their values (ECMA-376 Part 1, pml.xsd: a missing
// attribute name is one the element does not take; nil values are free).
var pptxElements = map[string]map[string][]string{
	"blinds": {"dir": {"horz", "vert"}}, "checker": {"dir": {"horz", "vert"}},
	"circle": {}, "dissolve": {}, "comb": {"dir": {"horz", "vert"}},
	"cover": {"dir": {"l", "u", "r", "d", "lu", "ru", "ld", "rd"}},
	"cut":   {"thruBlk": {"0", "1", "true", "false"}}, "diamond": {},
	"fade":      {"thruBlk": {"0", "1", "true", "false"}},
	"newsflash": {}, "plus": {},
	"pull":   {"dir": {"l", "u", "r", "d", "lu", "ru", "ld", "rd"}},
	"push":   {"dir": {"l", "u", "r", "d"}},
	"random": {}, "randomBar": {"dir": {"horz", "vert"}},
	"split":  {"orient": {"horz", "vert"}, "dir": {"in", "out"}},
	"strips": {"dir": {"lu", "ru", "ld", "rd"}}, "wedge": {},
	"wheel": {"spokes": nil}, "wipe": {"dir": {"l", "u", "r", "d"}},
	"zoom": {"dir": {"in", "out"}},
}

// smilTaxonomy is SMIL 2.0 12.8.1 Table 1: each transition type and its
// subtypes, which OpenDocument 1.3 names for smil:type and smil:subtype.
var smilTaxonomy = map[string][]string{
	"barWipe": {"leftToRight", "topToBottom"}, "boxWipe": {"topLeft", "topRight", "bottomRight", "bottomLeft", "topCenter", "rightCenter", "bottomCenter", "leftCenter"},
	"fourBoxWipe": {"cornersIn", "cornersOut"}, "barnDoorWipe": {"vertical", "horizontal", "diagonalBottomLeft", "diagonalTopLeft"},
	"diagonalWipe": {"topLeft", "topRight"}, "bowTieWipe": {"vertical", "horizontal"}, "miscDiagonalWipe": {"doubleBarnDoor", "doubleDiamond"},
	"veeWipe": {"down", "left", "up", "right"}, "barnVeeWipe": {"down", "left", "up", "right"},
	"zigZagWipe": {"leftToRight", "topToBottom"}, "barnZigZagWipe": {"vertical", "horizontal"},
	"irisWipe": {"rectangle", "diamond"}, "triangleWipe": {"up", "right", "down", "left"}, "arrowHeadWipe": {"up", "right", "down", "left"},
	"pentagonWipe": {"up", "down"}, "hexagonWipe": {"horizontal", "vertical"}, "ellipseWipe": {"circle", "horizontal", "vertical"},
	"eyeWipe": {"horizontal", "vertical"}, "roundRectWipe": {"horizontal", "vertical"}, "starWipe": {"fourPoint", "fivePoint", "sixPoint"},
	"miscShapeWipe": {"heart", "keyhole"}, "clockWipe": {"clockwiseTwelve", "clockwiseThree", "clockwiseSix", "clockwiseNine"},
	"pinWheelWipe":    {"twoBladeVertical", "twoBladeHorizontal", "fourBlade"},
	"singleSweepWipe": {"clockwiseTop", "clockwiseRight", "clockwiseBottom", "clockwiseLeft", "clockwiseTopLeft", "counterClockwiseBottomLeft", "clockwiseBottomRight", "counterClockwiseTopRight"},
	"fanWipe":         {"centerTop", "centerRight", "top", "right", "bottom", "left"},
	"doubleFanWipe":   {"fanOutVertical", "fanOutHorizontal", "fanInVertical", "fanInHorizontal"},
	"doubleSweepWipe": {"parallelVertical", "parallelDiagonal", "oppositeVertical", "oppositeHorizontal", "parallelDiagonalTopLeft", "parallelDiagonalBottomLeft"},
	"saloonDoorWipe":  {"top", "left", "bottom", "right"}, "windshieldWipe": {"right", "up", "vertical", "horizontal"},
	"pushWipe": {"fromLeft", "fromTop", "fromRight", "fromBottom"}, "slideWipe": {"fromLeft", "fromTop", "fromRight", "fromBottom"},
	"fade": {"crossfade", "fadeToColor", "fadeFromColor"},
}

// Every built-in transition has an entry, and no entry is for a name that is
// not one: the table is the one place the mapping lives.
func TestTransitionTableCoversTheBuiltins(t *testing.T) {
	var names []string
	for n := range transitionEffects {
		names = append(names, n)
	}
	slices.Sort(names)
	if !slices.Equal(names, deck.BuiltinTransitionNames) {
		t.Errorf("table: %v\nbuilt-ins: %v", names, deck.BuiltinTransitionNames)
	}
	if len(names) != 33 {
		t.Errorf("%d transitions, marp-cli has 33", len(names))
	}
}

// Each PPTX effect is an element and attributes of CT_SlideTransition.
func TestPPTXEffectsAreInTheSchema(t *testing.T) {
	check := func(name string, e PPTXEffect) {
		attrs, ok := pptxElements[e.Element]
		if !ok {
			t.Errorf("%s: p:%s is not a child of p:transition", name, e.Element)
			return
		}
		for _, a := range e.Attr {
			vals, ok := attrs[a[0]]
			if !ok {
				t.Errorf("%s: p:%s has no attribute %s", name, e.Element, a[0])
				continue
			}
			if vals == nil {
				if _, err := strconv.ParseUint(a[1], 10, 32); err != nil {
					t.Errorf("%s: %s=%q is not an unsigned int", name, a[0], a[1])
				}
			} else if !slices.Contains(vals, a[1]) {
				t.Errorf("%s: %s=%q is not one of %v", name, a[0], a[1], vals)
			}
		}
	}
	for n, e := range transitionEffects {
		check(n, e.PPTX)
	}
	check("fade", fadeEffect.PPTX)
}

// Each ODP effect is a type and subtype of SMIL's taxonomy.
func TestODPEffectsAreInTheTaxonomy(t *testing.T) {
	check := func(name string, e ODPEffect) {
		subs, ok := smilTaxonomy[e.Type]
		if !ok || !slices.Contains(subs, e.Subtype) {
			t.Errorf("%s: %s/%s is not in SMIL 12.8.1", name, e.Type, e.Subtype)
		}
	}
	for n, e := range transitionEffects {
		check(n, e.ODP)
	}
	check("fade", fadeEffect.ODP)
}

// Each PDF effect is one pdfkit writes, which refuses what Table 162 does not
// allow of the style.
func TestPDFEffectsAreWritable(t *testing.T) {
	d := pdfkit.New(pdfkit.Options{})
	page := func() *pdfkit.Page { return d.AddPage(pdfkit.A4) }
	check := func(name string, e PDFEffect) {
		if err := page().SetTransition(pdfkit.Transition{Style: e.Style, Direction: e.Direction, Vertical: e.Vertical, Outward: e.Outward}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for n, e := range transitionEffects {
		check(n, e.PDF)
	}
	check("fade", fadeEffect.PDF)
}

// The mapping coverage the documentation states.
func TestTransitionCoverage(t *testing.T) {
	for _, c := range []struct {
		format        string
		exact, approx int
	}{{"pptx", 8, 25}, {"odp", 11, 22}, {"pdf", 2, 31}} {
		exact, approx := Coverage(c.format)
		if exact != c.exact || approx != c.approx {
			t.Errorf("%s: %d exact, %d approximate; want %d and %d", c.format, exact, approx, c.exact, c.approx)
		}
	}
}

// A custom or unknown transition is a fade, Approx everywhere.
func TestCustomTransitionIsAFade(t *testing.T) {
	for _, tr := range []doc.Transition{{Name: "mine", Custom: true}, {Name: "fade", Custom: true}, {Name: "nonesuch"}} {
		e := EffectOf(tr)
		if e.PPTX.Element != "fade" || e.ODP.Type != "fade" || e.PDF.Style != pdfkit.TransitionFade ||
			e.PPTXFit != Approx || e.ODPFit != Approx || e.PDFFit != Approx {
			t.Errorf("%+v: %+v", tr, e)
		}
	}
}

func TestSpeedAndSeconds(t *testing.T) {
	for _, c := range []struct {
		dur  float64
		sec  float64
		spd  int
		name string
	}{{0, 0.5, 0, "default"}, {0.25, 0.25, 0, ""}, {0.5, 0.5, 0, ""}, {0.6, 0.6, 1, ""}, {1, 1, 1, ""}, {1.5, 1.5, 2, ""}} {
		tr := doc.Transition{Name: "fade", Duration: c.dur}
		if seconds(tr) != c.sec || speed(tr) != c.spd {
			t.Errorf("%v s: %v s, speed %d; want %v and %d", c.dur, seconds(tr), speed(tr), c.sec, c.spd)
		}
	}
}

// One warning, naming each substitution once, in order, or none when every
// transition is exact.
func TestSubstitutions(t *testing.T) {
	d := &doc.Doc{Slides: []doc.Slide{
		{},
		{Transition: &doc.Transition{Name: "fade"}},
		{Transition: &doc.Transition{Name: "cube"}},
		{Transition: &doc.Transition{Name: "cube"}},
		{Transition: &doc.Transition{Name: "mine", Custom: true}},
		{Transition: &doc.Transition{Name: "push"}},
	}}
	got := Substitutions(d, "pptx")
	want := "PPTX has no exact equivalent for 2 transition(s), which became the nearest: cube → push dir=l, mine (custom) → fade"
	if len(got) != 1 || got[0] != want {
		t.Errorf("pptx: %q", got)
	}
	got = Substitutions(d, "pdf")
	if len(got) != 1 || !strings.Contains(got[0], "push → Push") || !strings.Contains(got[0], "3 transition(s)") {
		t.Errorf("pdf: %q", got)
	}
	exact := &doc.Doc{Slides: []doc.Slide{{}, {Transition: &doc.Transition{Name: "fade"}}}}
	if got := Substitutions(exact, "pptx"); got != nil {
		t.Errorf("exact: %q", got)
	}
	if got := Substitutions(d, "html"); got != nil {
		t.Errorf("html: %q", got)
	}
	if got := Substitutions(d, "odp"); len(got) != 1 || !strings.Contains(got[0], "cube → pushWipe/fromRight") {
		t.Errorf("odp: %q", got)
	}
}

// The transition and timing the writers produce are well-formed XML.
func TestAnimationXMLIsWellFormed(t *testing.T) {
	wf := func(name, x string) {
		d := xml.NewDecoder(strings.NewReader(`<root xmlns:p="p" xmlns:anim="a" xmlns:smil="s" xmlns:presentation="x">` + x + `</root>`))
		for {
			if _, err := d.Token(); err != nil {
				if err.Error() != "EOF" {
					t.Errorf("%s: %v\n%s", name, err, x)
				}
				return
			}
		}
	}
	for n := range transitionEffects {
		wf("pptx "+n, pptxTransitionXML(doc.Transition{Name: n, Duration: 0.7}))
	}
	wf("timing", pptxTimingXML([][]pptxTarget{{{3, false}, {4, true}}, {{5, true}}}, nil))
	wf("odp", odpAnimationXML(2, odpAnim{transition: &doc.Transition{Name: "fade-out"}, clicks: [][]int{{0, 1}, {2}}}))
	if !strings.Contains(odpTransitionAttrs(doc.Transition{Name: "fade-out"}), `smil:fadeColor="#000000"`) {
		t.Error("fade-out has no colour to fade to")
	}
	if !strings.Contains(odpTransitionAttrs(doc.Transition{Name: "counterclockwise"}), `smil:direction="reverse"`) {
		t.Error("counterclockwise is not reversed")
	}
}

func TestCoverageNumbersMatchTheTable(t *testing.T) {
	// the number of effects whose fit is neither exact nor approximate
	for n, e := range transitionEffects {
		for _, f := range []Fit{e.PPTXFit, e.ODPFit, e.PDFFit} {
			if f != Exact && f != Approx {
				t.Errorf("%s: fit %v", n, f)
			}
		}
	}
	if None.String() != "none" || Exact.String() != "exact" || Approx.String() != "approximate" {
		t.Error("Fit names")
	}
}
