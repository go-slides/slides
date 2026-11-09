package export

import (
	"archive/zip"
	"bytes"
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-webengine/engine"

	"github.com/go-slides/slides/doc"
)

const spinSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 100" width="200" height="100">
<rect width="200" height="100" fill="#eef"/>
<g id="arm"><rect x="46" y="10" width="8" height="40" fill="#c00"/>
<animateTransform attributeName="transform" type="rotate" from="0 50 30" to="360 50 30" dur="2s" repeatCount="indefinite"/></g>
<circle id="dot" cx="150" cy="30" r="10" fill="#080">
<animate attributeName="opacity" values="1;0.2;1" dur="1s" repeatCount="indefinite"/></circle>
<rect id="ball" x="10" y="70" width="16" height="16" fill="#00c">
<animateTransform attributeName="transform" type="translate" from="0 0" to="150 0" dur="3s" begin="0.5s" repeatCount="2" fill="freeze"/></rect>
</svg>`

const morphSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">
<path id="p" d="M10 10 L90 10 L90 90 Z" fill="#c00"><animate attributeName="d" values="M10 10 L90 10 L90 90 Z;M10 10 L90 50 L50 90 Z;M10 10 L90 10 L90 90 Z" dur="2s" repeatCount="indefinite"/></path>
</svg>`

// animFixture writes the images in a directory and returns the deck made of
// markdown, with the options that read them.
func animFixture(t testing.TB, files map[string]string, md string, allow bool) (*doc.Doc, Options) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	opts := Options{BaseURL: "file://" + filepath.ToSlash(dir) + "/", Scale: 1}
	if allow {
		opts.FileRoot = dir
	}
	return build(t, md), opts
}

func pptxOfDeck(t *testing.T, d *doc.Doc, opts Options) ([]byte, *PPTXWriter, *SVGAnimations) {
	t.Helper()
	d2, anims, err := PrepareSVG(context.Background(), d, opts)
	if err != nil {
		t.Fatal(err)
	}
	w := NewPPTX(d2)
	w.SVG = anims
	if err := EachSteps(context.Background(), d2, opts, func(_ int, steps []*engine.PageLayout) error { return w.AddSteps(steps) }); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), w, anims
}

// A local image is not read without --allow-local-files, and the export says
// which file, in which slide, and which option (go-slides/slides#97).
func TestLocalImageBlockedIsSaid(t *testing.T) {
	d, opts := animFixture(t, map[string]string{"spin.svg": spinSVG}, "# One\n\n![](spin.svg)\n\n---\n\n![](missing.svg) ![](spin.svg)\n", false)
	_, anims, err := PrepareSVG(context.Background(), d, opts)
	if err != nil {
		t.Fatal(err)
	}
	w := strings.Join(anims.Warnings, "\n")
	for _, want := range []string{CodeLocalFileBlocked + `: "spin.svg" (slides 1, 2)`, "--allow-local-files", CodeLocalFileMissing + `: "missing.svg" (slide 2)`} {
		if !strings.Contains(w, want) {
			t.Errorf("no %q in\n%s", want, w)
		}
	}
	if anims.Animated() {
		t.Error("an image that was not read is animated")
	}
	// allowed: no blocked warning, and the image is found
	d, opts = animFixture(t, map[string]string{"spin.svg": spinSVG}, "# One\n\n![](spin.svg)\n", true)
	_, anims, err = PrepareSVG(context.Background(), d, opts)
	if err != nil || len(anims.Warnings) != 0 || !anims.Animated() {
		t.Fatalf("allowed: %v %v animated %v", err, anims.Warnings, anims.Animated())
	}
	// a file outside the root is blocked as well
	outside := filepath.Join(t.TempDir(), "o.svg")
	if err := os.WriteFile(outside, []byte(spinSVG), 0o644); err != nil {
		t.Fatal(err)
	}
	d, opts = animFixture(t, nil, "![](file://"+outside+")\n", true)
	_, anims, _ = PrepareSVG(context.Background(), d, opts)
	if anims.Animated() || len(anims.Warnings) != 1 || !strings.Contains(anims.Warnings[0], CodeLocalFileBlocked) {
		t.Errorf("outside the root: %v", anims.Warnings)
	}
}

func TestPPTXCarriesAnimatedSVGNatively(t *testing.T) {
	d, opts := animFixture(t, map[string]string{"spin.svg": spinSVG}, "# One\n\n![](spin.svg)\n", true)
	data, w, anims := pptxOfDeck(t, d, opts)
	slide := zipPart(t, data, "ppt/slides/slide1.xml")
	wellFormed(t, "slide1.xml", slide)
	for _, want := range []string{
		`nodeType="afterEffect"`, `nodeType="withEffect"`,
		`<p:animRot by="21600000">`, `repeatCount="indefinite"`, // the arm
		`<p:attrName>style.opacity</p:attrName>`,                                                                     // the dot
		`<p:animMotion origin="layout" path="M 0 0 L `, `repeatCount="2000"`, `<p:cond delay="500"/>`, `fill="hold"`, // the ball
	} {
		if !strings.Contains(slide, want) {
			t.Errorf("slide1.xml lacks %s", want)
		}
	}
	for _, want := range anims.Describe("pptx") {
		if !strings.Contains(want, "natively") {
			t.Errorf("%s", want)
		}
	}
	if len(w.Warnings) != 0 {
		t.Errorf("warnings: %v", w.Warnings)
	}
}

func TestPPTXFallsBackToFramesWhereNativeCannot(t *testing.T) {
	d, opts := animFixture(t, map[string]string{"morph.svg": morphSVG}, "# One\n\n![](morph.svg)\n", true)
	data, w, anims := pptxOfDeck(t, d, opts)
	found := false
	for _, name := range zipEntryNames(t, data) {
		if strings.HasPrefix(name, "ppt/media/") && strings.HasSuffix(name, ".gif") {
			found = true
			g := zipPart(t, data, name)
			if !strings.HasPrefix(g, "GIF89a") {
				t.Errorf("%s is not a GIF: %q", name, g[:6])
			}
		}
	}
	if !found {
		t.Fatal("no GIF in the deck")
	}
	slide := zipPart(t, data, "ppt/slides/slide1.xml")
	if strings.Contains(slide, "<p:timing>") {
		t.Error("a GIF carries its own animation: no timing")
	}
	line := strings.Join(anims.Describe("pptx"), "\n")
	if !strings.Contains(line, "animated as frames") || !strings.Contains(line, "animates d") {
		t.Errorf("describe: %s", line)
	}
	if len(w.Warnings) != 1 || !strings.Contains(w.Warnings[0], "animated GIF of") {
		t.Errorf("warnings: %v", w.Warnings)
	}
}

func TestPDFSaysAnimatedImagesStayStill(t *testing.T) {
	d, opts := animFixture(t, map[string]string{"spin.svg": spinSVG}, "# One\n\n![](spin.svg)\n", true)
	_, anims, err := PrepareSVG(context.Background(), d, opts)
	if err != nil {
		t.Fatal(err)
	}
	got := anims.Describe("pdf")
	if len(got) != 1 || !strings.Contains(got[0], "initial state") || !strings.Contains(got[0], "spin.svg") {
		t.Errorf("describe: %v", got)
	}
}

func zipEntryNames(t *testing.T, data []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	return out
}

func odpOfDeck(t *testing.T, d *doc.Doc, opts Options) ([]byte, *ODPWriter, *SVGAnimations) {
	t.Helper()
	d2, anims, err := PrepareSVG(context.Background(), d, opts)
	if err != nil {
		t.Fatal(err)
	}
	w := NewODP(d2)
	w.SVG = anims
	if err := EachSteps(context.Background(), d2, opts, func(_ int, steps []*engine.PageLayout) error { return w.AddSteps(steps) }); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), w, anims
}

func TestODPCarriesAnimatedSVGNatively(t *testing.T) {
	d, opts := animFixture(t, map[string]string{"spin.svg": spinSVG}, "# One\n\n![](spin.svg)\n", true)
	data, w, _ := odpOfDeck(t, d, opts)
	content := zipPart(t, data, "content.xml")
	wellFormed(t, "content.xml", content)
	for _, want := range []string{
		`<anim:animateTransform smil:dur="2s" smil:fill="remove" smil:repeatCount="indefinite" smil:targetElement="s1-`, `smil:by="360" smil:attributeName="transform" svg:type="rotate"`, // the arm
		`smil:attributeName="opacity" smil:values="1;0.2;1" smil:keyTimes="0;0.5;1"`,                                                              // the dot
		`<anim:animateMotion smil:begin="0.5s" smil:dur="3s" smil:fill="hold" smil:repeatCount="2" smil:targetElement="s1-`, `svg:path="M 0 0 L `, // the ball
		`presentation:node-type="after-previous"`, `presentation:node-type="with-previous"`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("content.xml lacks %s", want)
		}
	}
	// every target is a shape of the page, with an id LibreOffice resolves, and
	// the effects come after the shapes, as LibreOffice writes them
	for _, id := range []string{`xml:id="s1-1" draw:id="s1-1"`} {
		if !strings.Contains(content, id) {
			t.Errorf("no %s", id)
		}
	}
	if strings.Index(content, "<anim:par") < strings.LastIndex(content, `draw:id="s1-`) {
		t.Error("the animation comes before the shapes it names")
	}
	if len(w.Warnings) != 0 {
		t.Errorf("warnings: %v", w.Warnings)
	}
}

func TestODPFramesAreAGIFWithItsMediaType(t *testing.T) {
	d, opts := animFixture(t, map[string]string{"morph.svg": morphSVG}, "# One\n\n![](morph.svg)\n", true)
	data, w, _ := odpOfDeck(t, d, opts)
	var gif string
	for _, name := range zipEntryNames(t, data) {
		if strings.HasPrefix(name, "Pictures/") && strings.HasSuffix(name, ".gif") {
			gif = name
		}
	}
	if gif == "" {
		t.Fatalf("no GIF in %v", zipEntryNames(t, data))
	}
	if m := zipPart(t, data, "META-INF/manifest.xml"); !strings.Contains(m, gif+`" manifest:media-type="image/gif"`) {
		t.Errorf("manifest: %s", m)
	}
	if len(w.Warnings) != 1 || !strings.Contains(w.Warnings[0], "animated GIF of") {
		t.Errorf("warnings: %v", w.Warnings)
	}
}

// A slide with fragments and an animated drawing has both: what starts with the
// slide comes first, then a click for each fragment.
func TestSVGAnimationAndFragmentsShareTheTimeline(t *testing.T) {
	d, opts := animFixture(t, map[string]string{"spin.svg": spinSVG}, "# One\n\n* alpha\n* beta\n\n![](spin.svg)\n", true)
	data, _, _ := pptxOfDeck(t, d, opts)
	slide := zipPart(t, data, "ppt/slides/slide1.xml")
	wellFormed(t, "slide1.xml", slide)
	auto := strings.Index(slide, `nodeType="afterEffect"`)
	click := strings.Index(slide, `nodeType="clickEffect"`)
	if auto < 0 || click < 0 || auto > click {
		t.Errorf("auto effects at %d, click effects at %d", auto, click)
	}
	odp, _, _ := odpOfDeck(t, d, opts)
	content := zipPart(t, odp, "content.xml")
	wellFormed(t, "content.xml", content)
	a, c := strings.Index(content, `presentation:node-type="after-previous"`), strings.Index(content, `presentation:node-type="on-click"`)
	if a < 0 || c < 0 || a > c {
		t.Errorf("auto effects at %d, click effects at %d", a, c)
	}
}

func TestCSSAnimationsAreNative(t *testing.T) {
	css := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200">
<style>
@keyframes spin { from { transform: rotate(0deg) } to { transform: rotate(360deg) } }
.s { animation: spin 2s linear infinite; transform-origin: center; transform-box: fill-box }
</style>
<rect class="s" x="30" y="30" width="60" height="30" fill="#06c"/></svg>`
	d, opts := animFixture(t, map[string]string{"css.svg": css}, "![](css.svg)\n", true)
	data, _, anims := pptxOfDeck(t, d, opts)
	slide := zipPart(t, data, "ppt/slides/slide1.xml")
	if !strings.Contains(slide, `<p:animRot by="21600000">`) {
		t.Errorf("no spin: %s", slide)
	}
	if got := anims.Describe("pptx"); len(got) != 1 || !strings.Contains(got[0], "natively") {
		t.Errorf("describe: %v", got)
	}
}

// Rotation about a point that is not the element's centre is a movement of the
// centre as well: the shape turns and travels, and both are carried.
func TestRotationAboutAnotherPointIsMotionAndRotation(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200"><rect id="r" x="100" y="20" width="20" height="20" fill="#c00">
<animateTransform attributeName="transform" type="rotate" from="0 100 100" to="360 100 100" dur="4s" repeatCount="indefinite"/></rect></svg>`
	d, opts := animFixture(t, map[string]string{"orbit.svg": svg}, "![w:400](orbit.svg)\n", true)
	data, _, _ := pptxOfDeck(t, d, opts)
	slide := zipPart(t, data, "ppt/slides/slide1.xml")
	for _, want := range []string{`<p:attrName>ppt_x</p:attrName>`, `<p:attrName>ppt_y</p:attrName>`, `<p:animRot by="21600000">`} {
		if !strings.Contains(slide, want) {
			t.Errorf("slide lacks %s", want)
		}
	}
	if strings.Contains(slide, "<p:animMotion") {
		t.Error("a circle is not a straight line")
	}
}

func TestWhatNativeCannotSayBecomesFrames(t *testing.T) {
	for name, tc := range map[string]struct{ svg, why string }{
		"skew":                      {`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="20" height="20"><animateTransform attributeName="transform" type="skewX" from="0" to="30" dur="1s"/></rect></svg>`, "skewed"},
		"size":                      {`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><circle r="5" cx="50" cy="50" stroke="#000" stroke-width="2"><animate attributeName="r" from="5" to="30" dur="1s"/></circle></svg>`, "has a stroke"},
		"rounded corners":           {`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="20" height="20" rx="4"><animate attributeName="width" from="20" to="60" dur="1s"/></rect></svg>`, "rounded corners"},
		"x of a rect":               {`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="20" height="20"><animate attributeName="x" from="0" to="60" dur="1s"/></rect></svg>`, "animates x"},
		"dash":                      {`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><path d="M10 10H90" stroke="#000" stroke-dasharray="80" stroke-width="4"><animate attributeName="stroke-dashoffset" from="80" to="0" dur="1s"/></path></svg>`, "animates stroke-dashoffset"},
		"nested":                    {`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><g><animateTransform attributeName="transform" type="rotate" from="0" to="90" dur="1s"/><rect width="9" height="9"><animate attributeName="opacity" from="1" to="0" dur="1s"/></rect></g></svg>`, "holds another animated element"},
		"two clocks":                {`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="9" height="9"><animateTransform attributeName="transform" type="rotate" from="0" to="90" dur="1s"/><animateTransform attributeName="transform" type="translate" from="0" to="9" dur="2s" additive="sum"/></rect></svg>`, "different clocks"},
		"opacity of several shapes": {`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><g id="g"><rect width="9" height="9"/><rect x="20" width="9" height="9"/><animate attributeName="opacity" from="1" to="0" dur="1s"/></g></svg>`, "several shapes"},
		"reflect":                   {`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="9" height="9"><animateTransform attributeName="transform" type="scale" from="1 1" to="1 -1" dur="1s"/></rect></svg>`, "reflected"},
		"event":                     {`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect width="9" height="9"><animate attributeName="opacity" from="1" to="0" dur="1s"/><animate attributeName="opacity" from="1" to="0" dur="2s" begin="1s"/></rect></svg>`, "opacity is animated more than once"},
	} {
		t.Run(name, func(t *testing.T) {
			d, opts := animFixture(t, map[string]string{"x.svg": tc.svg}, "![w:300](x.svg)\n", true)
			data, _, anims := pptxOfDeck(t, d, opts)
			if line := strings.Join(anims.Describe("pptx"), "\n"); !strings.Contains(line, "frames") || !strings.Contains(line, tc.why) {
				t.Errorf("describe: %s, want %q", line, tc.why)
			}
			if slide := zipPart(t, data, "ppt/slides/slide1.xml"); strings.Contains(slide, "<p:timing>") {
				t.Error("frames carry no timing")
			}
			gifs := 0
			for _, n := range zipEntryNames(t, data) {
				if strings.HasSuffix(n, ".gif") {
					gifs++
				}
			}
			if gifs != 1 {
				t.Errorf("%d GIFs", gifs)
			}
		})
	}
}

func TestAStretchedImageFallsBackToFrames(t *testing.T) {
	d, opts := animFixture(t, map[string]string{"spin.svg": spinSVG}, "![w:300 h:300](spin.svg)\n", true)
	_, w, _ := pptxOfDeck(t, d, opts)
	if len(w.Warnings) != 1 || !strings.Contains(w.Warnings[0], "stretched unevenly") {
		t.Errorf("warnings: %v", w.Warnings)
	}
}

func TestInitialStateIsWhatAPDFOrAnImageShows(t *testing.T) {
	// the arm at t=0 is vertical; a quarter turn later it is horizontal: the
	// image of the prepared deck is the first
	d, opts := animFixture(t, map[string]string{"spin.svg": spinSVG}, "![w:400](spin.svg)\n", true)
	d2, anims, err := PrepareSVG(context.Background(), d, opts)
	if err != nil || !anims.Animated() {
		t.Fatal(err)
	}
	if strings.Contains(d2.Slides[0].HTML, `src="spin.svg"`) || !strings.Contains(d2.Slides[0].HTML, "data:image/svg+xml;base64,") {
		t.Errorf("the image was not replaced by its initial state: %s", d2.Slides[0].HTML)
	}
	if !strings.Contains(d.Slides[0].HTML, `src="spin.svg"`) {
		t.Error("the deck given was changed")
	}
	imgs, err := Rasterize(context.Background(), d2, opts)
	if err != nil || len(imgs) != 1 {
		t.Fatal(err)
	}
}

func TestRewriteImages(t *testing.T) {
	got, err := rewriteImages(`<p>a <img alt="x &amp; y" src="a.svg" width=5> <IMG src='b.png'/> <img> <span src="a.svg"></span></p>`, func(src string) (string, bool) {
		return "X-" + src, src == "a.svg"
	})
	if err != nil {
		t.Fatal(err)
	}
	want := `<p>a <img alt="x &amp; y" src="X-a.svg" width="5"> <IMG src='b.png'/> <img> <span src="a.svg"></span></p>`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestLoadImageKinds(t *testing.T) {
	ctx := context.Background()
	if data, st := loadImage(ctx, Options{}, "data:image/svg+xml;base64,PHN2Zy8+"); st != imgOK || string(data) != "<svg/>" {
		t.Errorf("base64: %q %v", data, st)
	}
	if data, st := loadImage(ctx, Options{}, "data:image/svg+xml,%3Csvg%2F%3E"); st != imgOK || string(data) != "<svg/>" {
		t.Errorf("percent: %q %v", data, st)
	}
	fetched := ""
	opts := Options{Fetcher: func(_ context.Context, u string) ([]byte, string, error) {
		fetched = u
		return []byte("<svg/>"), "image/svg+xml", nil
	}}
	if _, st := loadImage(ctx, opts, "https://example.com/a.svg"); st != imgOK || fetched != "https://example.com/a.svg" {
		t.Errorf("fetcher: %v %q", st, fetched)
	}
	if _, st := loadImage(ctx, Options{}, ""); st != imgSkipped {
		t.Errorf("empty: %v", st)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.svg"), []byte("<svg/>"), 0o644)
	if err := os.Symlink("/etc/hosts", filepath.Join(dir, "link.svg")); err == nil {
		if _, st := loadImage(ctx, Options{BaseURL: "file://" + dir + "/", FileRoot: dir}, "link.svg"); st != imgBlocked {
			t.Errorf("a link out of the root: %v", st)
		}
	}
	if _, st := loadImage(ctx, Options{BaseURL: "file://" + dir + "/", FileRoot: dir}, "../x.svg"); st == imgOK {
		t.Errorf("a path out of the root was read")
	}
}

func TestFramesAreBounded(t *testing.T) {
	d, opts := animFixture(t, map[string]string{"morph.svg": morphSVG}, "![](morph.svg)\n", true)
	_, anims, err := PrepareSVG(context.Background(), d, opts)
	if err != nil {
		t.Fatal(err)
	}
	a := anims.order[0]
	f := a.frames(4000, 4000)
	if f.err != nil || f.w > maxGIFWidth || float64(f.w*f.h*f.frames) > maxGIFPix {
		t.Errorf("%dx%d over %d frames: %v", f.w, f.h, f.frames, f.err)
	}
	if f := a.frames(5, 5); f.err == nil {
		t.Error("a 5x5 drawing is too small to see: it must say so")
	}
}

// at is the value of a behaviour's samples at position p of its iteration.
func (b svgBeh) at(p float64) [3]float64 {
	for i := 1; i < len(b.ts); i++ {
		if p <= b.ts[i] {
			f := (p - b.ts[i-1]) / (b.ts[i] - b.ts[i-1])
			var out [3]float64
			for k := range out {
				out[k] = b.v[i-1][k] + (b.v[i][k]-b.v[i-1][k])*f
			}
			return out
		}
	}
	return b.v[len(b.v)-1]
}

// The samples the plan takes are the element's true course: a square turning about a
// distant point goes where geometry puts it.
func TestPlanSamplesAreTheTrueCourse(t *testing.T) {
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 400">
<rect id="r" x="100" y="20" width="20" height="20"><animateTransform attributeName="transform" type="rotate" from="0 100 100" to="360 100 100" dur="4s" repeatCount="indefinite"/></rect>
<rect id="s" x="150" y="300" width="40" height="40"><animateTransform attributeName="transform" type="scale" from="1" to="2" dur="2s"/></rect></svg>`
	d, opts := animFixture(t, map[string]string{"x.svg": svg}, "![w:400](x.svg)\n", true)
	_, anims, err := PrepareSVG(context.Background(), d, opts)
	if err != nil {
		t.Fatal(err)
	}
	p := anims.order[0].getPlan()
	if !p.native {
		t.Fatal(p.why)
	}
	byID := map[string]*svgTarget{}
	for _, tg := range p.targets {
		byID[tg.id] = tg
	}
	r, s := byID["r"], byID["s"]
	var motion, rot svgBeh
	for _, b := range r.beh {
		switch b.kind {
		case behMotion:
			motion = b
		case behRotate:
			rot = b
		}
	}
	// a quarter turn about (100,100): the centre (110,30) goes to (170,110)
	if got := motion.at(0.25); math.Abs(got[0]-60) > 0.5 || math.Abs(got[1]-80) > 0.5 {
		t.Errorf("the centre moved by %v at a quarter turn, want (60,80)", got)
	}
	if got := rot.at(0.25)[0]; math.Abs(got-90) > 0.3 {
		t.Errorf("turned %v degrees, want 90", got)
	}
	if got := rot.at(1)[0]; math.Abs(got-360) > 0.3 {
		t.Errorf("turned %v degrees by the end, want 360 (unwrapped)", got)
	}
	var mv, sc svgBeh
	for _, b := range s.beh {
		switch b.kind {
		case behMotion:
			mv = b
		case behScale:
			sc = b
		}
	}
	// scaled about the origin: the centre (170,320) goes to (340,640)
	if got := mv.at(1); math.Abs(got[0]-170) > 0.5 || math.Abs(got[1]-320) > 0.5 {
		t.Errorf("the centre moved by %v, want (170,320)", got)
	}
	if got := sc.at(1); math.Abs(got[0]-2) > 1e-6 || math.Abs(got[1]-2) > 1e-6 {
		t.Errorf("scale %v, want 2", got)
	}
	if !mv.straight() || !sc.straight() {
		t.Error("a linear growth is two samples")
	}
}

// The radius of a circle and the size of a rectangle are a scale of the shape
// about its centre, with the centre's movement for a corner that stays put.
func TestSizeAnimationsAreScales(t *testing.T) {
	const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 400">
<circle id="c" cx="100" cy="100" r="20"><animate attributeName="r" from="20" to="60" dur="2s" repeatCount="indefinite"/></circle>
<rect id="r" x="200" y="100" width="40" height="20"><animate attributeName="width" values="40;80;40" dur="2s" repeatCount="indefinite"/><animate attributeName="height" values="20;60;20" dur="2s" repeatCount="indefinite"/></rect></svg>`
	d, opts := animFixture(t, map[string]string{"x.svg": svg}, "![w:400](x.svg)\n", true)
	data, _, anims := pptxOfDeck(t, d, opts)
	p := anims.order[0].getPlan()
	if !p.native {
		t.Fatal(p.why)
	}
	byID := map[string]*svgTarget{}
	for _, tg := range p.targets {
		byID[tg.id] = tg
	}
	var cs svgBeh
	for _, b := range byID["c"].beh {
		if b.kind == behScale {
			cs = b
		}
		if b.kind == behMotion {
			t.Error("a circle grows about its centre: no movement")
		}
	}
	if got := cs.at(1); math.Abs(got[0]-3) > 1e-6 || math.Abs(got[1]-3) > 1e-6 || !cs.straight() {
		t.Errorf("circle scale %v straight %v, want 3", got, cs.straight())
	}
	var rm, rs svgBeh
	for _, b := range byID["r"].beh {
		switch b.kind {
		case behMotion:
			rm = b
		case behScale:
			rs = b
		}
	}
	// at the middle the rectangle is 80x60: scale (2,3), its centre moved by (20,20)
	if got := rs.at(0.5); math.Abs(got[0]-2) > 0.01 || math.Abs(got[1]-3) > 0.01 {
		t.Errorf("rect scale %v, want (2,3)", got)
	}
	if got := rm.at(0.5); math.Abs(got[0]-20) > 0.5 || math.Abs(got[1]-20) > 0.5 {
		t.Errorf("rect centre moved by %v, want (20,20)", got)
	}
	slide := zipPart(t, data, "ppt/slides/slide1.xml")
	if !strings.Contains(slide, "<p:animScale>") || !strings.Contains(slide, "ppt_w") {
		t.Errorf("no scale in the slide")
	}
}
