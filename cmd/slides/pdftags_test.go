package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-pdfkit/reader"
)

// A PDF is tagged unless --pdf-tags=false (or --no-pdf-tags) says not, or
// --pdf-raster draws each page as one picture.
func TestPDFTagsFlag(t *testing.T) {
	dir := t.TempDir()
	md := write(t, dir, "d.md", "# Title\n\nText.\n")
	tagged := func(args ...string) bool {
		t.Helper()
		out := filepath.Join(dir, "o.pdf")
		if _, stderr, err := run(t, append([]string{"build", md, "--pdf", "-o", out}, args...)...); err != nil {
			t.Fatal(err, stderr)
		}
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := reader.Open(b)
		if err != nil {
			t.Fatal(err)
		}
		cat, err := doc.Catalog()
		if err != nil {
			t.Fatal(err)
		}
		_, ok := cat["StructTreeRoot"]
		return ok
	}
	if !tagged() {
		t.Error("a PDF is tagged by default")
	}
	for _, args := range [][]string{{"--pdf-tags=false"}, {"--no-pdf-tags"}, {"--pdf-raster"}} {
		if tagged(args...) {
			t.Errorf("%v: tagged", args)
		}
	}
}
