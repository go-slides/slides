package export

import (
	"math"
	"strconv"
	"strings"
)

// The effects of the animated SVGs of an OpenDocument page: each animated
// element is a shape whose animations start with the page, in the form
// LibreOffice writes for an effect that starts after the previous one at the
// beginning of a page, and reads back (OpenDocument 1.3 Part 3, 15.2 and 19.4:
// anim:animate, anim:animateMotion, anim:animateTransform, anim:animateColor,
// with the attributes of SMIL Animation).
//
//   - anim:animateMotion with svg:path, in fractions of the page, for a
//     movement that is a straight line;
//   - anim:animateTransform of svg:type rotate (smil:by in degrees) or scale
//     (smil:by, the growth of the scale factor, 1 being from 1 to 2), for a
//     rotation or scale that is linear in time;
//   - anim:animate of the attributes x and y ("x+0.1": the shape's position
//     moved by a fraction of the page), width and height ("width*1.5"), rotate
//     (degrees), opacity, with smil:values and smil:keyTimes, for the others;
//   - anim:animateColor of fill-color or stroke-color, from one colour to
//     another.

// odpFxXML is the effects of a page's animated shapes: a group of the main
// sequence that starts with the page. id gives the draw:id of a shape by its
// place in the page's shape list.
func odpFxXML(fxs []svgFx, id func(index int) string, pageW, pageH float64) string {
	var b strings.Builder
	b.WriteString(`<anim:par smil:begin="0s" smil:fill="hold"><anim:par smil:begin="0s" smil:fill="hold">`)
	for i, fx := range fxs {
		kind := "with-previous"
		if i == 0 {
			kind = "after-previous"
		}
		class := "emphasis"
		if len(fx.t.beh) > 0 && fx.t.beh[0].kind == behMotion {
			class = "motion-path"
		}
		b.WriteString(`<anim:par smil:begin="0s" smil:fill="hold" presentation:node-type="` + kind + `" presentation:preset-class="` + class + `" presentation:preset-id="">`)
		target := id(fx.index)
		for _, beh := range fx.t.beh {
			b.WriteString(odpBehXML(beh, fx, target, pageW, pageH))
		}
		b.WriteString(`</anim:par>`)
	}
	b.WriteString(`</anim:par></anim:par>`)
	return b.String()
}

// odpClock is the attributes every behaviour carries: its start, duration,
// repeat, reversal and what it leaves behind, then its target.
func odpClock(t svgTiming, target string) string {
	var b strings.Builder
	if t.begin > 0 {
		b.WriteString(` smil:begin="` + strconv.FormatFloat(t.begin, 'f', -1, 64) + `s"`)
	}
	b.WriteString(` smil:dur="` + strconv.FormatFloat(t.dur, 'f', -1, 64) + `s"`)
	if t.freeze {
		b.WriteString(` smil:fill="hold"`)
	} else {
		b.WriteString(` smil:fill="remove"`)
	}
	if c := t.cycles(); math.IsInf(c, 1) {
		b.WriteString(` smil:repeatCount="indefinite"`)
	} else if c != 1 {
		b.WriteString(` smil:repeatCount="` + strconv.FormatFloat(c, 'f', -1, 64) + `"`)
	}
	if t.autoRev {
		b.WriteString(` smil:autoReverse="true"`)
	}
	b.WriteString(` smil:targetElement="` + target + `"`)
	return b.String()
}

func semi(vals []string) string { return strings.Join(vals, ";") }

func odpBehXML(b svgBeh, fx svgFx, target string, pageW, pageH float64) string {
	clock := odpClock(b.t, target)
	fx0 := func(v float64) float64 { return round6(v * fx.sx / pageW) }
	fy0 := func(v float64) float64 { return round6(v * fx.sx / pageH) }
	keyTimes := func() string {
		kt := make([]string, len(b.ts))
		for i, v := range b.ts {
			kt[i] = num(round6(v))
		}
		return ` smil:keyTimes="` + semi(kt) + `"`
	}
	calc := ""
	if b.t.discrete {
		calc = ` smil:calcMode="discrete"`
	}
	animate := func(attr string, vals []string) string {
		return `<anim:animate` + clock + ` smil:attributeName="` + attr + `" smil:values="` + semi(vals) + `"` + keyTimes() + calc + `/>`
	}
	switch b.kind {
	case behMotion:
		last := b.v[len(b.v)-1]
		if b.straight() {
			return `<anim:animateMotion` + clock + ` svg:path="M 0 0 L ` + num(fx0(last[0])) + ` ` + num(fy0(last[1])) + `"/>`
		}
		var xs, ys []string
		for _, v := range b.v {
			xs = append(xs, "x"+signed(fx0(v[0])))
			ys = append(ys, "y"+signed(fy0(v[1])))
		}
		return animate("x", xs) + animate("y", ys)
	case behRotate:
		if b.straight() {
			return `<anim:animateTransform` + clock + ` smil:by="` + num(round6(b.v[1][0])) + `" smil:attributeName="transform" svg:type="rotate"/>`
		}
		var vs []string
		for _, v := range b.v {
			vs = append(vs, num(round6(v[0])))
		}
		return animate("rotate", vs)
	case behScale:
		if b.straight() {
			return `<anim:animateTransform` + clock + ` smil:by="` + num(round6(b.v[1][0]-1)) + `,` + num(round6(b.v[1][1]-1)) + `" smil:attributeName="transform" svg:type="scale"/>`
		}
		var ws, hs []string
		for _, v := range b.v {
			ws = append(ws, "width*"+num(round6(v[0])))
			hs = append(hs, "height*"+num(round6(v[1])))
		}
		return animate("width", ws) + animate("height", hs)
	case behOpacity:
		var vs []string
		for _, v := range b.v {
			vs = append(vs, num(round6(v[0])))
		}
		return animate("opacity", vs)
	case behFill, behStroke:
		attr := "fill-color"
		if b.kind == behStroke {
			attr = "stroke-color"
		}
		hex := func(v [3]float64) string {
			const digits = "0123456789abcdef"
			out := []byte{'#'}
			for _, c := range v {
				n := int(math.Round(c))
				out = append(out, digits[n>>4], digits[n&15])
			}
			return string(out)
		}
		return `<anim:animateColor` + clock + ` smil:attributeName="` + attr + `" smil:from="` + hex(b.v[0]) + `" smil:to="` + hex(b.v[1]) + `" anim:color-interpolation="rgb" anim:color-interpolation-direction="clockwise"/>`
	}
	return ""
}
