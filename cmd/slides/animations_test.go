package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rsc.io/pdf"
)

const animationsDeck = `---
marp: true
---

# One

* a
* b

---

<!-- transition: cube -->

# Two

* c

---

# Three
`

func pages(t *testing.T, path string) *pdf.Reader {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := pdf.NewReader(strings.NewReader(string(b)), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// --pdf-fragments writes a page for each step of a fragmented slide; without
// it the PDF has a page a slide, as marp-cli's. The transition of a slide
// comes in on the next one's page either way, with one warning for what it
// is not exactly.
func TestPDFFragments(t *testing.T) {
	dir := t.TempDir()
	md := write(t, dir, "d.md", animationsDeck)
	plain := filepath.Join(dir, "plain.pdf")
	_, stderr, err := run(t, "build", md, "-o", plain, "--lang", "C")
	if err != nil {
		t.Fatal(err, stderr)
	}
	if n := pages(t, plain).NumPage(); n != 3 {
		t.Errorf("%d pages without --pdf-fragments, want one a slide", n)
	}
	if got := strings.Count(stderr, "warning: PDF has no exact equivalent"); got != 1 || !strings.Contains(stderr, "cube → Push") {
		t.Errorf("the substitution warning: %q", stderr)
	}
	if tr := pages(t, plain).Page(3).V.Key("Trans").Key("S").Name(); tr != "Push" {
		t.Errorf("page 3's transition is %q", tr)
	}

	steps := filepath.Join(dir, "steps.pdf")
	if _, stderr, err := run(t, "build", md, "-o", steps, "--pdf-fragments", "--lang", "C"); err != nil {
		t.Fatal(err, stderr)
	}
	// 1+2 pages for slide one, 1+1 for two, 1 for three
	r := pages(t, steps)
	if r.NumPage() != 6 {
		t.Fatalf("%d pages with --pdf-fragments, want 6", r.NumPage())
	}
	if tr := r.Page(5).V.Key("Trans"); tr.Key("S").Name() != "Fade" || tr.Key("D").Float64() != 0.2 {
		t.Errorf("page 5, the step of slide two, comes in with %v", tr)
	}
	if tr := r.Page(6).V.Key("Trans").Key("S").Name(); tr != "Push" {
		t.Errorf("page 6 (slide 3) comes in with %q", tr)
	}

	// the vector PDF only
	_, stderr, err = run(t, "build", md, "-o", filepath.Join(dir, "r.pdf"), "--pdf-fragments", "--pdf-raster", "--lang", "C")
	if err == nil || !strings.Contains(stderr+err.Error(), "--pdf-fragments needs the vector PDF") {
		t.Errorf("--pdf-fragments --pdf-raster: %v %q", err, stderr)
	}
}

// PPTX and ODP name the substitutions once each, and write the deck.
func TestOfficeTransitionsWarn(t *testing.T) {
	dir := t.TempDir()
	md := write(t, dir, "d.md", animationsDeck)
	for _, f := range []struct{ ext, warn string }{
		{"pptx", "warning: PPTX has no exact equivalent for 1 transition(s), which became the nearest: cube → push dir=l"},
		{"odp", "warning: ODP has no exact equivalent for 1 transition(s), which became the nearest: cube → pushWipe/fromRight"},
	} {
		out := filepath.Join(dir, "d."+f.ext)
		_, stderr, err := run(t, "build", md, "-o", out, "--lang", "C")
		if err != nil {
			t.Fatal(f.ext, err, stderr)
		}
		if strings.Count(stderr, f.warn) != 1 {
			t.Errorf("%s: %q", f.ext, stderr)
		}
		if _, err := os.Stat(out); err != nil {
			t.Error(err)
		}
	}
	// HTML has its own transitions: no warning
	_, stderr, err := run(t, "build", md, "-o", filepath.Join(dir, "d.html"), "--lang", "C")
	if err != nil || strings.Contains(stderr, "no exact equivalent") {
		t.Errorf("html: %v %q", err, stderr)
	}
}
