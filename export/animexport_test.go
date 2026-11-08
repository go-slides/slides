package export

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/go-webengine/engine"
	"github.com/go-webengine/engine/officecanvas"
	"github.com/go-webengine/engine/paint"
	"rsc.io/pdf"

	"github.com/go-slides/slides/doc"
)

// animDeck: slide 1 leaves with a one-second push, slide 2 has three
// fragments and leaves with a fade, slide 3 has nested ones and a cube, slide
// 4 a transition nothing defines, slide 5 none.
const animDeck = `---
marp: true
transition: push 1s
---

# One

Plain.

---

<!-- transition: fade -->

# Two

* alpha
* beta
* gamma

---

<!-- transition: cube -->

# Three

* red
  * deep one
* green

---

<!-- transition: nonesuch -->

# Four

Plain.

---

# Five

Plain.
`

func zipPart(t *testing.T, data []byte, name string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			b, err := io.ReadAll(rc)
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
	}
	t.Fatalf("no part %s", name)
	return ""
}

func wellFormed(t *testing.T, name, s string) {
	t.Helper()
	d := xml.NewDecoder(strings.NewReader(s))
	for {
		if _, err := d.Token(); err != nil {
			if err != io.EOF {
				t.Errorf("%s is not well-formed: %v", name, err)
			}
			return
		}
	}
}

func steppedPPTX(t *testing.T, d *doc.Doc) []byte {
	t.Helper()
	w := NewPPTX(d)
	if err := EachSteps(context.Background(), d, Options{Scale: 1}, func(_ int, steps []*engine.PageLayout) error { return w.AddSteps(steps) }); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// textOfShape is the text of the shape p:cNvPr id=id of a slide.
func textOfShape(slide, id string) string {
	i := strings.Index(slide, `<p:cNvPr id="`+id+`"`)
	if i < 0 {
		return ""
	}
	j := strings.Index(slide[i:], `</p:sp>`)
	var text strings.Builder
	for _, m := range regexp.MustCompile(`<a:t>([^<]*)</a:t>`).FindAllStringSubmatch(slide[i:i+j], -1) {
		text.WriteString(m[1])
	}
	return text.String()
}

// A PowerPoint deck has each slide's transition on the slide it comes in on,
// at the nearest speed, and one click-triggered fade for each fragment, on the
// shapes of that fragment alone and in fragment order.
func TestPPTXTransitionsAndBuilds(t *testing.T) {
	d := build(t, animDeck)
	if d.Slides[0].Transition != nil || d.Slides[1].Transition == nil || d.Slides[1].Transition.Duration != 1 {
		t.Fatalf("the doc's transitions: %+v", d.Slides)
	}
	if d.Slides[1].Fragments != 3 || d.Slides[2].Fragments != 3 || d.Slides[0].Fragments != 0 {
		t.Fatalf("fragments: %d %d %d", d.Slides[0].Fragments, d.Slides[1].Fragments, d.Slides[2].Fragments)
	}
	data := steppedPPTX(t, d)
	slide := func(n int) string {
		s := zipPart(t, data, "ppt/slides/slide"+strconv.Itoa(n)+".xml")
		wellFormed(t, "slide"+strconv.Itoa(n), s)
		return s
	}
	want := []string{"", `<p:transition spd="med"><p:push dir="l"/></p:transition>`, `<p:transition spd="fast"><p:fade/></p:transition>`,
		`<p:transition spd="fast"><p:push dir="l"/></p:transition>`, `<p:transition spd="fast"><p:fade/></p:transition>`}
	for i, w := range want {
		s := slide(i + 1)
		if w == "" && strings.Contains(s, "<p:transition") {
			t.Errorf("slide %d has a transition", i+1)
		}
		if w != "" && !strings.Contains(s, w) {
			t.Errorf("slide %d lacks %s", i+1, w)
		}
		// CT_Slide: cSld, clrMapOvr, transition, timing
		if i := strings.Index(s, "</p:clrMapOvr>"); w != "" && !strings.HasPrefix(s[i+len("</p:clrMapOvr>"):], "<p:transition") {
			t.Errorf("slide %d: the transition does not follow clrMapOvr", i+1)
		}
	}

	s2 := slide(2)
	if n := strings.Count(s2, `nodeType="clickEffect"`); n != 3 {
		t.Fatalf("slide 2 has %d clicks, want 3", n)
	}
	// each click's targets: the first fragment's say alpha, never beta
	ids := regexp.MustCompile(`<p:cNvPr id="(\d+)"`).FindAllStringSubmatch(s2, -1)
	have := map[string]bool{}
	for _, m := range ids {
		if have[m[1]] {
			t.Errorf("shape id %s twice", m[1])
		}
		have[m[1]] = true
	}
	timing := s2[strings.Index(s2, "<p:timing>"):]
	var texts []string
	for _, click := range strings.Split(timing, `<p:cond delay="indefinite"/>`)[1:] {
		var text string
		for _, m := range regexp.MustCompile(`<p:set><p:cBhvr>.*?spid="(\d+)"`).FindAllStringSubmatch(click, -1) {
			if !have[m[1]] {
				t.Errorf("a click targets shape %s, which the slide does not have", m[1])
			}
			text += textOfShape(s2, m[1])
		}
		texts = append(texts, text)
	}
	if len(texts) != 3 || texts[0] != "alpha" || texts[1] != "beta" || texts[2] != "gamma" {
		t.Errorf("the clicks reveal %q", texts)
	}
	// the text boxes are in the build list, once
	bld := s2[strings.Index(s2, "<p:bldLst>"):]
	if n := strings.Count(bld, "<p:bldP "); n != 3 {
		t.Errorf("%d build paragraphs, want the 3 text boxes", n)
	}
	// nested fragments: three clicks, in order
	s3 := slide(3)
	if n := strings.Count(s3, `nodeType="clickEffect"`); n != 3 {
		t.Errorf("slide 3 has %d clicks, want 3", n)
	}
	if strings.Contains(slide(1), "<p:timing>") || strings.Contains(slide(4), "<p:timing>") {
		t.Error("a slide without fragments has a timing")
	}
}

// Animations are only written for the slides that have them, and the deck says
// so when its parts are not where they were.
func TestAddPPTXAnimationsRefusals(t *testing.T) {
	d := build(t, animDeck)
	data := steppedPPTX(t, d)
	if _, err := addPPTXAnimations(data, map[int]pptxAnim{99: {transition: &doc.Transition{Name: "fade"}}}); err == nil {
		t.Error("an animation for a slide the deck does not have was accepted")
	}
	if _, err := addPPTXAnimations([]byte("not a zip"), map[int]pptxAnim{0: {}}); err == nil {
		t.Error("a deck that is not a zip was accepted")
	}
	// a deck whose slide does not end as go-ooxml's do
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("ppt/slides/slide1.xml")
	w.Write([]byte("<p:sld/>"))
	w, _ = zw.Create("ppt/slides/_rels/slide1.xml.rels")
	w.Write([]byte("<x/>"))
	zw.Close()
	if _, err := addPPTXAnimations(buf.Bytes(), map[int]pptxAnim{0: {transition: &doc.Transition{Name: "fade"}}}); err == nil {
		t.Error("a slide of another shape was accepted")
	}
}

func steppedODP(t *testing.T, d *doc.Doc) []byte {
	t.Helper()
	w := NewODP(d)
	if err := EachSteps(context.Background(), d, Options{Scale: 1}, func(_ int, steps []*engine.PageLayout) error { return w.AddSteps(steps) }); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// An OpenDocument deck has the transition in the page's style and its
// timing, and the fragments as the page's clicks, on shapes that have ids.
func TestODPTransitionsAndBuilds(t *testing.T) {
	d := build(t, animDeck)
	data := steppedODP(t, d)
	content := zipPart(t, data, "content.xml")
	wellFormed(t, "content.xml", content)
	if !strings.Contains(content, `xmlns:smil="urn:oasis:names:tc:opendocument:xmlns:smil-compatible:1.0"`) ||
		!strings.Contains(content, `xmlns:anim="urn:oasis:names:tc:opendocument:xmlns:animation:1.0"`) {
		t.Error("the smil and anim prefixes are not declared")
	}
	pages, err := pageSpans(content)
	if err != nil || len(pages) != 5 {
		t.Fatalf("%d pages, %v", len(pages), err)
	}
	style := func(n int) string {
		i := strings.Index(content, `style:name="dpa`+strconv.Itoa(n)+`"`)
		if i < 0 {
			return ""
		}
		return content[i : i+strings.Index(content[i:], `</style:style>`)]
	}
	if s := style(2); !strings.Contains(s, `smil:type="pushWipe" smil:subtype="fromRight"`) || !strings.Contains(s, `presentation:transition-speed="medium"`) || !strings.Contains(s, `draw:fill-color`) {
		t.Errorf("page 2's style: %s", s)
	}
	if style(1) != "" || style(5) == "" {
		t.Error("the first slide has a transition style, or the last has none")
	}
	if !strings.Contains(content[pages[1].start:pages[1].bodyStart], `draw:style-name="dpa2"`) {
		t.Error("page 2 does not use its style")
	}
	if !strings.Contains(content, `<anim:transitionFilter smil:dur="1s" smil:type="pushWipe" smil:subtype="fromRight"/>`) {
		t.Error("the transition's duration is not in the page's timing")
	}
	// page 2: three clicks, the first of alpha
	p2 := content[pages[1].start:pages[1].end]
	if n := strings.Count(p2, `presentation:node-type="on-click"`); n != 3 {
		t.Fatalf("page 2 has %d clicks", n)
	}
	var texts []string
	for _, click := range strings.Split(p2, `smil:begin="next"`)[1:] {
		var text string
		for _, m := range regexp.MustCompile(`<anim:set [^>]*smil:targetElement="([^"]+)"`).FindAllStringSubmatch(click, -1) {
			i := strings.Index(p2, `draw:id="`+m[1]+`"`)
			if i < 0 {
				t.Fatalf("a click targets %s, which the page does not have", m[1])
			}
			// the shape's own text: its element, from its start tag to its end
			start := strings.LastIndex(p2[:i], "<")
			end := tagEnd(p2[start:])
			if tagName(p2[start:]) == "draw:frame" {
				end = strings.Index(p2[start:], "</draw:frame>")
			}
			for _, tm := range regexp.MustCompile(`<text:span[^>]*>([^<]*)</text:span>`).FindAllStringSubmatch(p2[start:start+end], -1) {
				text += tm[1]
			}
		}
		texts = append(texts, text)
	}
	if len(texts) != 3 || texts[0] != "alpha" || texts[1] != "beta" || texts[2] != "gamma" {
		t.Errorf("the clicks reveal %q", texts)
	}
	for _, c := range []string{"s2-", "s3-"} {
		if !strings.Contains(content, `draw:id="`+c) {
			t.Errorf("no shape id %s…", c)
		}
	}
	if strings.Contains(content[pages[0].start:pages[0].end], "anim:") {
		t.Error("slide 1 has an animation")
	}
	// the mimetype entry is still first and stored
	zr, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if zr.File[0].Name != "mimetype" || zr.File[0].Method != zip.Store {
		t.Errorf("the first entry is %s, method %d", zr.File[0].Name, zr.File[0].Method)
	}
}

func TestEditODPContentRefusals(t *testing.T) {
	anim := map[int]odpAnim{0: {transition: &doc.Transition{Name: "fade"}}}
	for name, s := range map[string]string{
		"no page":      `<office:document-content></office:document-content>`,
		"no style":     `<office:document-content><draw:page draw:name="a"></draw:page></office:document-content>`,
		"style absent": `<office:document-content><draw:page draw:style-name="x"></draw:page></office:document-content>`,
		"no end":       `<office:document-content><draw:page draw:style-name="x">`,
		"no tag end":   `<office:document-content><draw:page draw:style-name="x`,
	} {
		out, err := editODPContent(s, anim)
		if name == "no page" {
			if err != nil || !strings.Contains(string(out), "<office:document-content xmlns:smil=") {
				t.Errorf("%s: %v", name, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	good := `<office:document-content><office:automatic-styles><style:style style:name="x" style:family="drawing-page"><style:drawing-page-properties a="b"/></style:style></office:automatic-styles><draw:page draw:style-name="x"><draw:frame draw:name="a"/></draw:page></office:document-content>`
	if _, err := editODPContent(good, anim); err != nil {
		t.Error(err)
	}
	if _, err := editODPContent(good, map[int]odpAnim{0: {clicks: [][]int{{5}}}}); err == nil {
		t.Error("a click on a shape the page does not have was accepted")
	}
	for name, s := range map[string]string{
		"no properties": strings.Replace(good, "drawing-page-properties", "other", 2),
		"not empty":     strings.Replace(good, `a="b"/>`, `a="b"></style:drawing-page-properties>`, 1),
		"style open":    strings.Replace(good, "</style:style>", "", 1),
		"no styles":     strings.Replace(good, "</office:automatic-styles>", "", 1),
		"no root":       strings.Replace(good, "office:document-content", "x", 2),
	} {
		if _, err := editODPContent(s, anim); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := addODPAnimations([]byte("not a zip"), anim); err == nil {
		t.Error("a deck that is not a zip was accepted")
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("other.xml")
	w.Write([]byte("<x/>"))
	zw.Close()
	if _, err := addODPAnimations(buf.Bytes(), anim); err == nil {
		t.Error("a deck without content.xml was accepted")
	}
}

func TestTopLevelAndTags(t *testing.T) {
	body := `<draw:frame a="x>y"><draw:text-box><text:p/></draw:text-box></draw:frame><anim:par/><presentation:notes><draw:frame/></presentation:notes><draw:g><draw:rect/></draw:g></draw:page>`
	tops, err := topLevel(body)
	if err != nil || len(tops) != 2 {
		t.Fatalf("%v %v", tops, err)
	}
	if tagName(body[tops[1]:]) != "draw:g" || tagName("<a>") != "a" || tagName("<a/>") != "a" || tagName("<a") != "a" {
		t.Error("tagName")
	}
	if _, err := topLevel(`<draw:frame a="x`); err == nil {
		t.Error("an open tag was accepted")
	}
	if tagEnd(`<a b='>'>`) != 8 || tagEnd("<a") != -1 {
		t.Error("tagEnd")
	}
	if v, ok := attrValue(`<draw:page draw:style-name="q">`, "draw:style-name"); !ok || v != "q" {
		t.Error("attrValue")
	}
	if _, ok := attrValue(`<draw:page>`, "x"); ok {
		t.Error("attrValue found a missing attribute")
	}
	if _, ok := attrValue(`<draw:page x="`, "x"); ok {
		t.Error("attrValue found an open value")
	}
}

// A shape belongs to the first step that draws it, and shapes every step draws
// to none.
func TestRevealedAt(t *testing.T) {
	sh := func(text string, y float64) officecanvas.Shape {
		return officecanvas.Shape{Kind: officecanvas.KindText, Runs: []officecanvas.Run{{Text: text}}, Bounds: paint.Rect{Y: y}}
	}
	a, b, c := sh("a", 0), sh("b", 1), sh("a", 0) // a and c are twins
	steps := []officecanvas.Page{
		{Shapes: []officecanvas.Shape{a}},
		{Shapes: []officecanvas.Shape{a, b}},
		{Shapes: []officecanvas.Shape{a, b, c}},
	}
	got := revealedAt(steps)
	if len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
		t.Errorf("revealedAt = %v", got)
	}
	frags := fragmentShapes(steps)
	if len(frags) != 2 || len(frags[0]) != 1 || frags[0][0] != 1 || frags[1][0] != 2 {
		t.Errorf("fragmentShapes = %v", frags)
	}
	// a slide without fragments
	if f := fragmentShapes(steps[:1]); len(f) != 0 {
		t.Errorf("one step: %v", f)
	}
}

// PDF: a /Trans on each page a transition comes in on, and with fragments one
// page for each step, the later ones fading in.
func TestPDFTransitionsAndSteps(t *testing.T) {
	d := build(t, animDeck)
	var plainData, steppedData []byte
	render := func(steps bool) *pdf.Reader {
		w := NewPDFWith(d, PDFOptions{OutlinePages: true, Untagged: true})
		var err error
		if steps {
			err = EachSteps(context.Background(), d, Options{Scale: 1}, func(_ int, st []*engine.PageLayout) error { return w.AddSteps(st) })
		} else {
			err = EachLayout(context.Background(), d, Options{Scale: 1}, func(_ int, l *engine.PageLayout) error { return w.AddLayout(l) })
		}
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := w.Write(&buf); err != nil {
			t.Fatal(err)
		}
		if steps {
			steppedData = buf.Bytes()
		} else {
			plainData = buf.Bytes()
		}
		r, err := pdf.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	plain := render(false)
	if plain.NumPage() != 5 {
		t.Fatalf("%d pages", plain.NumPage())
	}
	if plain.Page(1).V.Key("Trans").Kind() != pdf.Null {
		t.Error("the first page has a transition")
	}
	for i, style := range []string{"Push", "Fade", "Push", "Fade"} {
		tr := plain.Page(i + 2).V.Key("Trans")
		if tr.Key("S").Name() != style {
			t.Errorf("page %d: /S = %q, want %s", i+2, tr.Key("S").Name(), style)
		}
	}
	if got := plain.Page(2).V.Key("Trans").Key("D").Float64(); got != 1 {
		t.Errorf("page 2's /D = %v, the directive says 1s", got)
	}
	if got := plain.Page(3).V.Key("Trans").Key("D").Float64(); got != 0.5 {
		t.Errorf("page 3's /D = %v, marp-cli's default is 0.5s", got)
	}
	stepped := render(true)
	// 1 + (3+1) + (3+1) + 1 + 1
	if stepped.NumPage() != 11 {
		t.Fatalf("%d pages with steps", stepped.NumPage())
	}
	// The text of a page, read by pdftotext when it is installed: page 2 of
	// the plain PDF has all its fragments, and the pages of the steps one more
	// each.
	if bin, err := exec.LookPath("pdftotext"); err != nil {
		t.Log("pdftotext is not installed: the pages' text is not read")
	} else {
		text := func(data []byte, n int) string {
			f := filepath.Join(t.TempDir(), "d.pdf")
			if err := os.WriteFile(f, data, 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := exec.Command(bin, "-f", strconv.Itoa(n), "-l", strconv.Itoa(n), f, "-").Output()
			if err != nil {
				t.Fatal(err)
			}
			return string(out)
		}
		if got := text(plainData, 2); !strings.Contains(got, "alpha") || !strings.Contains(got, "gamma") {
			t.Errorf("without steps page 2 says %q", got)
		}
		var grew []bool
		for n := 2; n <= 5; n++ {
			s := text(steppedData, n)
			grew = append(grew, strings.Contains(s, "alpha"), strings.Contains(s, "beta"), strings.Contains(s, "gamma"))
		}
		want := []bool{false, false, false, true, false, false, true, true, false, true, true, true}
		for i := range want {
			if grew[i] != want[i] {
				t.Errorf("steps of slide 2 (alpha, beta, gamma by page): %v, want %v", grew, want)
				break
			}
		}
	}
	if stepped.Page(2).V.Key("Trans").Key("S").Name() != "Push" {
		t.Error("the first page of a slide lost its transition")
	}
	for n := 3; n <= 5; n++ {
		tr := stepped.Page(n).V.Key("Trans")
		if tr.Key("S").Name() != "Fade" || tr.Key("D").Float64() != 0.2 {
			t.Errorf("step page %d: %v", n, tr)
		}
	}
	// the slide after the steps comes in with the slide before's transition
	if stepped.Page(6).V.Key("Trans").Key("S").Name() != "Fade" || stepped.Page(6).V.Key("Trans").Key("D").Float64() != 0.5 {
		t.Errorf("slide 3's page: %v", stepped.Page(6).V.Key("Trans"))
	}
}
