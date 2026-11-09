package export

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// The effects of the animated SVGs of a PowerPoint slide (ECMA-376 Part 1,
// 19.5): each animated element is a shape that starts, with the slide, an
// effect of the behaviours of its animations.
//
//   - p:animMotion (19.5.4): a path, in fractions of the slide, relative to
//     the shape, at constant speed: for a movement that is a straight line;
//   - p:animRot (19.5.3) by an angle (60000ths of a degree), about the
//     centre of the shape, and p:animScale (19.5.5) by a ratio (100000ths):
//     for a rotation or scale that is linear in time;
//   - p:anim (19.5.1) with a time-value list (p:tavLst) of the attributes
//     ppt_x and ppt_y (the centre, as a fraction of the slide, given as a
//     formula on its current value), ppt_w and ppt_h (the size), style.rotation
//     and style.opacity: for every other course in time (the samples were
//     taken at the animation's own times, so that easing and several
//     keyframes are in them);
//   - p:animClr (19.5.2) from one colour to another, on fillcolor or
//     stroke.color.
//
// Each carries its timing in the p:cTn of its p:cBhvr: dur, the delay as a
// start condition, repeatCount (thousandths; "indefinite"), autoRev, fill
// (hold to keep the last state, remove to go back).

// pptxFxXML is the effects of a slide's animated shapes: the content of one
// p:par of the main sequence, which starts when the slide does. spid gives a
// shape's id by its place in the slide's shape list. nid hands out the
// p:cTn ids.
func pptxFxXML(fxs []svgFx, spid func(index int) int, pageW, pageH float64, nid func() string) string {
	var b strings.Builder
	b.WriteString(`<p:par><p:cTn id="` + nid() + `" fill="hold"><p:stCondLst><p:cond delay="indefinite"/><p:cond evt="onBegin" delay="0"><p:tn val="2"/></p:cond></p:stCondLst><p:childTnLst>`)
	b.WriteString(`<p:par><p:cTn id="` + nid() + `" fill="hold"><p:stCondLst><p:cond delay="0"/></p:stCondLst><p:childTnLst>`)
	for i, fx := range fxs {
		kind := "withEffect"
		if i == 0 {
			kind = "afterEffect"
		}
		class := "emph"
		if len(fx.t.beh) > 0 && fx.t.beh[0].kind == behMotion {
			class = "path"
		}
		b.WriteString(`<p:par><p:cTn id="` + nid() + `" presetID="0" presetClass="` + class + `" presetSubtype="0" fill="hold" nodeType="` + kind + `"><p:stCondLst><p:cond delay="0"/></p:stCondLst><p:childTnLst>`)
		sp := strconv.Itoa(spid(fx.index))
		for _, beh := range fx.t.beh {
			b.WriteString(pptxBehXML(beh, fx, sp, pageW, pageH, nid))
		}
		b.WriteString(`</p:childTnLst></p:cTn></p:par>`)
	}
	b.WriteString(`</p:childTnLst></p:cTn></p:par></p:childTnLst></p:cTn></p:par>`)
	return b.String()
}

// cBhvr is a behaviour's p:cBhvr: its clock, its target and the attributes it
// sets.
func cBhvr(t svgTiming, sp string, attrs []string, nid func() string) string {
	var b strings.Builder
	b.WriteString(`<p:cBhvr><p:cTn id="` + nid() + `" dur="` + strconv.Itoa(t.durMS()) + `"`)
	if c := t.cycles(); math.IsInf(c, 1) {
		b.WriteString(` repeatCount="indefinite"`)
	} else if c != 1 {
		b.WriteString(` repeatCount="` + strconv.Itoa(int(math.Round(c*1000))) + `"`)
	}
	if t.autoRev {
		b.WriteString(` autoRev="1"`)
	}
	if t.freeze {
		b.WriteString(` fill="hold"`)
	} else {
		b.WriteString(` fill="remove"`)
	}
	if ms := int(math.Round(t.begin * 1000)); ms > 0 {
		b.WriteString(`><p:stCondLst><p:cond delay="` + strconv.Itoa(ms) + `"/></p:stCondLst></p:cTn>`)
	} else {
		b.WriteString(`/>`)
	}
	b.WriteString(`<p:tgtEl><p:spTgt spid="` + sp + `"/></p:tgtEl>`)
	if len(attrs) > 0 {
		b.WriteString(`<p:attrNameLst>`)
		for _, a := range attrs {
			b.WriteString(`<p:attrName>` + a + `</p:attrName>`)
		}
		b.WriteString(`</p:attrNameLst>`)
	}
	b.WriteString(`</p:cBhvr>`)
	return b.String()
}

func tm(t float64) string { return strconv.Itoa(int(math.Round(t * 100000))) }

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// signed writes "+0.25" or "-0.25".
func signed(v float64) string {
	s := strconv.FormatFloat(v, 'f', 6, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-0" {
		s = "0"
	}
	if !strings.HasPrefix(s, "-") {
		s = "+" + s
	}
	return s
}

func pptxBehXML(b svgBeh, fx svgFx, sp string, pageW, pageH float64, nid func() string) string {
	var x strings.Builder
	// a length in viewBox units, as a fraction of the slide
	fx0 := func(v float64) float64 { return v * fx.sx / pageW }
	fy0 := func(v float64) float64 { return v * fx.sx / pageH }
	tav := func(attr string, vals []string, calc string) string {
		var s strings.Builder
		s.WriteString(`<p:anim calcmode="` + calc + `" valueType="num">` + cBhvr(b.t, sp, []string{attr}, nid) + `<p:tavLst>`)
		for i, v := range vals {
			s.WriteString(`<p:tav tm="` + tm(b.ts[i]) + `"><p:val>` + v + `</p:val></p:tav>`)
		}
		s.WriteString(`</p:tavLst></p:anim>`)
		return s.String()
	}
	calc := "lin"
	if b.t.discrete {
		calc = "discrete"
	}
	switch b.kind {
	case behMotion:
		last := b.v[len(b.v)-1]
		if b.straight() {
			x.WriteString(`<p:animMotion origin="layout" path="M 0 0 L ` + num(round6(fx0(last[0]))) + ` ` + num(round6(fy0(last[1]))) + ` E" pathEditMode="relative" ptsTypes="AA">`)
			x.WriteString(cBhvr(b.t, sp, []string{"ppt_x", "ppt_y"}, nid) + `</p:animMotion>`)
			break
		}
		var xs, ys []string
		for _, v := range b.v {
			xs = append(xs, `<p:strVal val="#ppt_x`+signed(fx0(v[0]))+`"/>`)
			ys = append(ys, `<p:strVal val="#ppt_y`+signed(fy0(v[1]))+`"/>`)
		}
		x.WriteString(tav("ppt_x", xs, calc) + tav("ppt_y", ys, calc))
	case behRotate:
		if b.straight() {
			x.WriteString(`<p:animRot by="` + strconv.Itoa(int(math.Round(b.v[1][0]*60000))) + `">` + cBhvr(b.t, sp, []string{"r"}, nid) + `</p:animRot>`)
			break
		}
		var vs []string
		for _, v := range b.v {
			vs = append(vs, `<p:fltVal val="`+num(round6(v[0]))+`"/>`)
		}
		x.WriteString(tav("style.rotation", vs, calc))
	case behScale:
		if b.straight() {
			x.WriteString(`<p:animScale>` + cBhvr(b.t, sp, nil, nid) + `<p:by x="` + strconv.Itoa(int(math.Round(b.v[1][0]*100000))) + `" y="` + strconv.Itoa(int(math.Round(b.v[1][1]*100000))) + `"/></p:animScale>`)
			break
		}
		var ws, hs []string
		for _, v := range b.v {
			ws = append(ws, `<p:strVal val="#ppt_w*`+num(round6(v[0]))+`"/>`)
			hs = append(hs, `<p:strVal val="#ppt_h*`+num(round6(v[1]))+`"/>`)
		}
		x.WriteString(tav("ppt_w", ws, calc) + tav("ppt_h", hs, calc))
	case behOpacity:
		var vs []string
		for _, v := range b.v {
			vs = append(vs, `<p:fltVal val="`+num(round6(v[0]))+`"/>`)
		}
		x.WriteString(tav("style.opacity", vs, calc))
	case behFill, behStroke:
		attr := "fillcolor"
		if b.kind == behStroke {
			attr = "stroke.color"
		}
		hex := func(v [3]float64) string {
			return fmt.Sprintf("%02X%02X%02X", int(math.Round(v[0])), int(math.Round(v[1])), int(math.Round(v[2])))
		}
		x.WriteString(`<p:animClr clrSpc="rgb" dir="cw">` + cBhvr(b.t, sp, []string{attr}, nid) +
			`<p:from><a:srgbClr val="` + hex(b.v[0]) + `"/></p:from><p:to><a:srgbClr val="` + hex(b.v[1]) + `"/></p:to></p:animClr>`)
	}
	return x.String()
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }
