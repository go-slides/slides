package slides

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-slides/slides/export"
	"github.com/go-webengine/engine"
)

// The golden files of testdata/golden were written by the build BEFORE the data
// providers (package emoji, package mathfont) existed, from testdata/golden/
// providers.md: a deck with emoji (Unicode, shortcodes, a ZWJ sequence, a flag,
// a keycap, one in a code span) and formulas. The default build, which embeds
// Twemoji and STIX Two Math, must still write the same bytes, in the three
// formats. Set GOLDEN_UPDATE=1 to rewrite them (only ever from a commit whose
// output is the reference).
const goldenDir = "testdata/golden"

func goldenBuild(t *testing.T) (html string, pdf, pptx []byte) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(goldenDir, "providers.md"))
	if err != nil {
		t.Fatal(err)
	}
	html, b, err := HTML(src, Config{})
	if err != nil {
		t.Fatal(err)
	}
	d := b.Doc()
	opts := export.Options{NoFonts: true}
	var pb bytes.Buffer
	pw := export.NewPDFWith(d, export.DefaultPDFOptions)
	if err := export.EachLayout(context.Background(), d, opts, func(_ int, l *engine.PageLayout) error { return pw.AddLayout(l) }); err != nil {
		t.Fatal(err)
	}
	if err := pw.Write(&pb); err != nil {
		t.Fatal(err)
	}
	var xb bytes.Buffer
	xw := export.NewPPTX(d)
	if err := export.EachSteps(context.Background(), d, opts, func(_ int, s []*engine.PageLayout) error { return xw.AddSteps(s) }); err != nil {
		t.Fatal(err)
	}
	if err := xw.Write(&xb); err != nil {
		t.Fatal(err)
	}
	return html, pb.Bytes(), xb.Bytes()
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join(goldenDir, name)
	if os.Getenv("GOLDEN_UPDATE") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s: %d bytes, the reference has %d (first difference at %d)", name, len(got), len(want), firstDiff(got, want))
	}
}

func firstDiff(a, b []byte) int {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// TestGoldenDefaultBuild: HTML, PDF and PPTX of the default build are the bytes
// the build wrote before the providers.
func TestGoldenDefaultBuild(t *testing.T) {
	html, pdf, pptx := goldenBuild(t)
	// the reference is not a deck that skipped the data: it holds the images and the formulas
	for _, want := range []string{`data-marp-twemoji=""`, `class="slides-math"`, `data:image/svg+xml;base64,`} {
		if !strings.Contains(html, want) {
			t.Fatalf("the golden deck lacks %s", want)
		}
	}
	checkGolden(t, "providers.html", []byte(html))
	checkGolden(t, "providers.pdf", pdf)
	checkGolden(t, "providers.pptx", pptx)
}
