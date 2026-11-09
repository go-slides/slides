package export

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/go-webengine/engine"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/doc"
	"github.com/go-slides/slides/vfs"
)

func videoDoc(t *testing.T, src string, files ...string) (*doc.Doc, []string) {
	t.Helper()
	m := map[string][]byte{}
	for _, f := range files {
		b, err := os.ReadFile("../testdata/video/" + f)
		if err != nil {
			t.Fatal(err)
		}
		m[f] = b
	}
	cfg := slides.Config{Parse: deck.Options{FS: vfs.NewMap(m)}}
	b, err := slides.Render([]byte(src), cfg, deck.FrameBox)
	if err != nil {
		t.Fatal(err)
	}
	return b.Doc(), b.Warnings
}

func zipNames(t *testing.T, data []byte) map[string]bool {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, f := range zr.File {
		out[f.Name] = true
	}
	return out
}

// The PPTX holds the video as a media part, related twice from the slide, and
// the picture that was its poster carries a:videoFile and p14:media.
func TestVideoInPPTX(t *testing.T) {
	d, warn := videoDoc(t, "# Clip\n\n![video](bframes.mp4#t=0.2)\n", "bframes.mp4")
	if len(warn) > 0 {
		t.Fatalf("warnings: %v", warn)
	}
	if len(d.Media) != 1 {
		t.Fatalf("Media = %d entries", len(d.Media))
	}
	w := NewPPTX(d)
	if err := EachLayout(context.Background(), d, Options{BaseURL: "file:///deck/"}, func(_ int, l *engine.PageLayout) error { return w.AddLayout(l) }); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatal(err)
	}
	names := zipNames(t, buf.Bytes())
	if !names["ppt/media/slidesvideo1.mp4"] {
		t.Fatalf("no media part: %v", names)
	}
	xml := zipPart(t, buf.Bytes(), "ppt/slides/slide1.xml")
	for _, want := range []string{`<a:videoFile r:link="rIdVideoV1x0"/>`, `<p14:media xmlns:p14="http://schemas.microsoft.com/office/powerpoint/2010/main" r:embed="rIdVideoM1x0"/>`} {
		if !strings.Contains(xml, want) {
			t.Errorf("slide1.xml lacks %s", want)
		}
	}
	rels := zipPart(t, buf.Bytes(), "ppt/slides/_rels/slide1.xml.rels")
	if !strings.Contains(rels, `Target="../media/slidesvideo1.mp4"`) || !strings.Contains(rels, relVideo) || !strings.Contains(rels, relMedia) {
		t.Errorf("rels: %s", rels)
	}
	if ct := zipPart(t, buf.Bytes(), "[Content_Types].xml"); !strings.Contains(ct, `Extension="mp4" ContentType="video/mp4"`) {
		t.Errorf("content types: %s", ct)
	}
	mp4, _ := os.ReadFile("../testdata/video/bframes.mp4")
	if got := zipPart(t, buf.Bytes(), "ppt/media/slidesvideo1.mp4"); got != string(mp4) {
		t.Error("the media part is not the file")
	}
}

// The ODP holds the video under Video/, in the manifest, as a draw:plugin
// before the poster's draw:image.
func TestVideoInODP(t *testing.T) {
	d, warn := videoDoc(t, "# Clip\n\n![video](bframes.mp4)\n", "bframes.mp4")
	if len(warn) > 0 {
		t.Fatalf("warnings: %v", warn)
	}
	w := NewODP(d)
	if err := EachLayout(context.Background(), d, Options{BaseURL: "file:///deck/"}, func(_ int, l *engine.PageLayout) error { return w.AddLayout(l) }); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := w.Write(&buf); err != nil {
		t.Fatal(err)
	}
	if !zipNames(t, buf.Bytes())["Video/slidesvideo1.mp4"] {
		t.Fatal("no Video/ part")
	}
	if c := zipPart(t, buf.Bytes(), "content.xml"); !strings.Contains(c, `<draw:plugin xlink:href="Video/slidesvideo1.mp4"`) || !strings.Contains(c, `draw:mime-type="video/mp4"/><draw:image `) {
		t.Errorf("content.xml has no plugin before the image")
	}
	if m := zipPart(t, buf.Bytes(), "META-INF/manifest.xml"); !strings.Contains(m, `manifest:full-path="Video/slidesvideo1.mp4" manifest:media-type="video/mp4"`) {
		t.Errorf("manifest: %s", m)
	}
}

// The PDF links the poster to the file.
func TestVideoInPDF(t *testing.T) {
	d, _ := videoDoc(t, "# Clip\n\n![video](bframes.mp4)\n", "bframes.mp4")
	var buf bytes.Buffer
	if err := VectorPDF(context.Background(), &buf, d, Options{NoFonts: true, BaseURL: "file:///deck/"}); err != nil {
		t.Fatal(err)
	}
	r := readPDF(t, buf.Bytes())
	annots := r.Page(1).V.Key("Annots")
	found := false
	for i := 0; i < annots.Len(); i++ {
		if annots.Index(i).Key("A").Key("URI").RawString() == "bframes.mp4" {
			found = true
		}
	}
	if !found {
		t.Error("the PDF has no link to bframes.mp4")
	}
}

// An entry that is stored holds its sizes in its header: a stored entry that
// defers them to a data descriptor cannot be read by a reader that does not
// scan for the descriptor, and LibreOffice is one ("source file could not be
// loaded"). The media parts are added after the writer's own entries, and are
// the ones this guards.
func TestVideoPackagesHaveNoStoredEntryWithADescriptor(t *testing.T) {
	d, _ := videoDoc(t, "# Clip\n\n![video](bframes.mp4)\n", "bframes.mp4")
	for name, write := range map[string]func() ([]byte, error){
		"pptx": func() ([]byte, error) {
			w := NewPPTX(d)
			if err := EachLayout(context.Background(), d, Options{BaseURL: "file:///deck/"}, func(_ int, l *engine.PageLayout) error { return w.AddLayout(l) }); err != nil {
				return nil, err
			}
			var b bytes.Buffer
			err := w.Write(&b)
			return b.Bytes(), err
		},
		"odp": func() ([]byte, error) {
			w := NewODP(d)
			if err := EachLayout(context.Background(), d, Options{BaseURL: "file:///deck/"}, func(_ int, l *engine.PageLayout) error { return w.AddLayout(l) }); err != nil {
				return nil, err
			}
			var b bytes.Buffer
			err := w.Write(&b)
			return b.Bytes(), err
		},
	} {
		data, err := write()
		if err != nil {
			t.Fatal(err)
		}
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range zr.File {
			if f.Method == zip.Store && f.Flags&0x8 != 0 {
				t.Errorf("%s: %s is stored with a data descriptor", name, f.Name)
			}
		}
	}
}
