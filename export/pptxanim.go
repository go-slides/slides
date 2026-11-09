package export

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/go-ooxml/ooxml"

	"github.com/go-slides/slides/doc"
)

// The transition and the builds of a PowerPoint slide are the XML elements
// p:transition and p:timing of ECMA-376 Part 1 (19.3.1.50 and 19.5; the
// schema CT_SlideTransition, CT_SlideTiming), written after the slide's
// p:clrMapOvr, where CT_Slide puts them. go-ooxml has no field for either, so
// they are written into the slide parts of the deck it wrote; the writer there
// is where they belong, and this file is what moves.

// pptxAnim is what a slide gets: its transition, and its builds, one click
// each.
type pptxAnim struct {
	transition *doc.Transition
	clicks     [][]pptxTarget
	// fx are the animated SVG shapes' effects, which start with the slide; ids
	// the shape ids by place in the slide's shape list, and the page the size
	// of the slide in device pixels (svgemit.go, pptxsvg.go).
	fx           []svgFx
	ids          []int
	pageW, pageH float64
}

// pptxTarget is a shape an effect acts on: its id (p:cNvPr id) and whether it
// has text, which PowerPoint lists in the slide's build list.
type pptxTarget struct {
	id   int
	text bool
}

// pptxShapeCount is the number of ids a shape takes: itself and, for a group,
// its members, after it (go-ooxml numbers them in that order).
func pptxShapeCount(s ooxml.Shape) int {
	n := 1
	for _, m := range s.Group {
		n += pptxShapeCount(m)
	}
	return n
}

// pptxIDs are the ids go-ooxml gives a slide's top-level shapes: it numbers
// them from 2, a group's members after it.
func pptxIDs(shapes []ooxml.Shape) []int {
	ids := make([]int, len(shapes))
	next := 2
	for j, s := range shapes {
		ids[j] = next
		next += pptxShapeCount(s)
	}
	return ids
}

// pptxClicks turns the fragments' shapes (indexes of the recorded page) into
// click targets: shapes[j] was made from the page's shape from[j], and
// go-ooxml numbers a slide's shapes from 2, a group's members after it.
func pptxClicks(shapes []ooxml.Shape, from []int, fragments [][]int) [][]pptxTarget {
	// a page shape that became several (an animated drawing's layers) is
	// revealed with all of them
	pos := make(map[int][]int, len(from))
	for j, f := range from {
		pos[f] = append(pos[f], j)
	}
	ids := pptxIDs(shapes)
	var clicks [][]pptxTarget
	for _, frag := range fragments {
		var c []pptxTarget
		for _, f := range frag {
			for _, j := range pos[f] {
				c = append(c, pptxTarget{id: ids[j], text: len(shapes[j].Text) > 0})
			}
		}
		if len(c) > 0 {
			clicks = append(clicks, c)
		}
	}
	return clicks
}

// fragmentFadeMS is the length of a fragment's fade: the viewer's
// `transition: opacity .2s`.
const fragmentFadeMS = 200

// pptxTransitionXML is the p:transition of a slide: the nearest PowerPoint
// effect to Marp's (animation.go) at the nearest of its three speeds.
func pptxTransitionXML(t doc.Transition) string {
	e := EffectOf(t).PPTX
	var b strings.Builder
	b.WriteString(`<p:transition spd="` + [...]string{"fast", "med", "slow"}[speed(t)] + `"><p:` + e.Element)
	for _, a := range e.Attr {
		b.WriteString(` ` + a[0] + `="` + a[1] + `"`)
	}
	b.WriteString(`/></p:transition>`)
	return b.String()
}

// pptxTimingXML is the p:timing of a slide's builds: a main sequence with one
// click for each fragment, whose shapes fade in together, the first as the
// click's effect and the others with it, as PowerPoint writes an entrance
// effect (preset 10, Fade).
func pptxTimingXML(clicks [][]pptxTarget, auto func(nid func() string) string) string {
	var b strings.Builder
	id := 2
	nid := func() string { id++; return strconv.Itoa(id) }
	b.WriteString(`<p:timing><p:tnLst><p:par><p:cTn id="1" dur="indefinite" restart="never" nodeType="tmRoot"><p:childTnLst>`)
	b.WriteString(`<p:seq concurrent="1" nextAc="seek"><p:cTn id="2" dur="indefinite" nodeType="mainSeq"><p:childTnLst>`)
	if auto != nil {
		// what starts with the slide comes first in the sequence
		b.WriteString(auto(nid))
	}
	var texts []int
	for _, click := range clicks {
		b.WriteString(`<p:par><p:cTn id="` + nid() + `" fill="hold"><p:stCondLst><p:cond delay="indefinite"/></p:stCondLst><p:childTnLst>`)
		b.WriteString(`<p:par><p:cTn id="` + nid() + `" fill="hold"><p:stCondLst><p:cond delay="0"/></p:stCondLst><p:childTnLst>`)
		for j, t := range click {
			kind := "withEffect"
			if j == 0 {
				kind = "clickEffect"
			}
			sp := strconv.Itoa(t.id)
			b.WriteString(`<p:par><p:cTn id="` + nid() + `" presetID="10" presetClass="entr" presetSubtype="0" fill="hold"`)
			if t.text {
				b.WriteString(` grpId="0"`)
			}
			b.WriteString(` nodeType="` + kind + `"><p:stCondLst><p:cond delay="0"/></p:stCondLst><p:childTnLst>`)
			b.WriteString(`<p:set><p:cBhvr><p:cTn id="` + nid() + `" dur="1" fill="hold"><p:stCondLst><p:cond delay="0"/></p:stCondLst></p:cTn>`)
			b.WriteString(`<p:tgtEl><p:spTgt spid="` + sp + `"/></p:tgtEl><p:attrNameLst><p:attrName>style.visibility</p:attrName></p:attrNameLst></p:cBhvr>`)
			b.WriteString(`<p:to><p:strVal val="visible"/></p:to></p:set>`)
			b.WriteString(`<p:animEffect transition="in" filter="fade"><p:cBhvr><p:cTn id="` + nid() + `" dur="` + strconv.Itoa(fragmentFadeMS) + `"/>`)
			b.WriteString(`<p:tgtEl><p:spTgt spid="` + sp + `"/></p:tgtEl></p:cBhvr></p:animEffect>`)
			b.WriteString(`</p:childTnLst></p:cTn></p:par>`)
			if t.text {
				texts = append(texts, t.id)
			}
		}
		b.WriteString(`</p:childTnLst></p:cTn></p:par></p:childTnLst></p:cTn></p:par>`)
	}
	b.WriteString(`</p:childTnLst></p:cTn>`)
	b.WriteString(`<p:prevCondLst><p:cond evt="onPrev" delay="0"><p:tgtEl><p:sldTgt/></p:tgtEl></p:cond></p:prevCondLst>`)
	b.WriteString(`<p:nextCondLst><p:cond evt="onNext" delay="0"><p:tgtEl><p:sldTgt/></p:tgtEl></p:cond></p:nextCondLst></p:seq>`)
	b.WriteString(`</p:childTnLst></p:cTn></p:par></p:tnLst>`)
	if len(texts) > 0 {
		b.WriteString(`<p:bldLst>`)
		for _, t := range texts {
			b.WriteString(`<p:bldP spid="` + strconv.Itoa(t) + `" grpId="0"/>`)
		}
		b.WriteString(`</p:bldLst>`)
	}
	b.WriteString(`</p:timing>`)
	return b.String()
}

// slideTail is how go-ooxml ends every slide part.
const slideTail = `</p:clrMapOvr></p:sld>`

// maxPart bounds what is read of one part of a deck this package just wrote.
const maxPart = 256 << 20

// addPPTXAnimations writes each slide's transition and timing into the deck.
// Slide i (0-based) is the part ppt/slides/slide{i+1}.xml.
func addPPTXAnimations(deck []byte, anims map[int]pptxAnim) ([]byte, error) {
	done := map[int]bool{}
	slideOf := func(name string) (int, bool) {
		rest, ok := strings.CutPrefix(name, "ppt/slides/slide")
		if !ok {
			return 0, false
		}
		num, ok := strings.CutSuffix(rest, ".xml")
		n, err := strconv.Atoi(num)
		return n - 1, ok && err == nil // slides/_rels/... is not a slide part
	}
	want := func(name string) bool {
		n, ok := slideOf(name)
		_, has := anims[n]
		return ok && has
	}
	out, err := rewriteZip(deck, want, func(name string, body []byte) ([]byte, error) {
		n, _ := slideOf(name)
		a := anims[n]
		s := string(body)
		head, found := strings.CutSuffix(s, slideTail)
		if !found {
			return nil, fmt.Errorf("export: %s does not end as go-ooxml's slides do", name)
		}
		var tail strings.Builder
		tail.WriteString(`</p:clrMapOvr>`)
		if a.transition != nil {
			tail.WriteString(pptxTransitionXML(*a.transition))
		}
		if len(a.clicks) > 0 || len(a.fx) > 0 {
			var auto func(func() string) string
			if len(a.fx) > 0 {
				auto = func(nid func() string) string {
					return pptxFxXML(a.fx, func(i int) int { return a.ids[i] }, a.pageW, a.pageH, nid)
				}
			}
			tail.WriteString(pptxTimingXML(a.clicks, auto))
		}
		tail.WriteString(`</p:sld>`)
		done[n] = true
		return []byte(head + tail.String()), nil
	})
	if err != nil {
		return nil, err
	}
	if len(done) != len(anims) {
		return nil, fmt.Errorf("export: wrote the animations of %d slides of %d: the deck's slide parts are not where they were", len(done), len(anims))
	}
	return out, nil
}

// rewriteZip copies a zip archive, replacing the body of every entry that
// want accepts by what edit makes of it. Entries keep their order, and the
// others are copied as stored, so a mimetype entry stays first and
// uncompressed; only the entries edited are read.
func rewriteZip(data []byte, want func(name string) bool, edit func(name string, body []byte) ([]byte, error)) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range zr.File {
		if !want(f.Name) {
			if err := zw.Copy(f); err != nil {
				return nil, err
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(rc, maxPart+1))
		rc.Close()
		if err != nil {
			return nil, err
		}
		if len(body) > maxPart {
			return nil, fmt.Errorf("export: %s is larger than %d MiB", f.Name, maxPart>>20)
		}
		edited, err := edit(f.Name, body)
		if err != nil {
			return nil, err
		}
		h := f.FileHeader
		h.CompressedSize64, h.UncompressedSize64, h.CRC32 = 0, 0, 0
		w, err := zw.CreateHeader(&h)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(edited); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
