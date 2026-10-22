package main

import (
	"archive/zip"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const calloutDeck = "# Code\n\n```go\nfunc main() { // <1>\n\tx := load() // <2>\n}\n```\n\n1. The entry point.\n2. Reads the input.\n"

// In the exports the discs are drawn, not written: the PDF's text (read by
// pdftotext, when it is installed) has the code without a number among its
// lines, and the PowerPoint keeps them as editable shapes and text, not as
// pictures.
func TestCalloutsInExports(t *testing.T) {
	dir := t.TempDir()
	md := write(t, dir, "d.md", calloutDeck)

	out := filepath.Join(dir, "o.pdf")
	if _, stderr, err := run(t, "build", md, "--pdf", "-o", out); err != nil {
		t.Fatal(err, stderr)
	}
	if bin, err := exec.LookPath("pdftotext"); err == nil {
		txt := filepath.Join(dir, "o.txt")
		if b, err := exec.Command(bin, out, txt).CombinedOutput(); err != nil {
			t.Fatalf("%v\n%s", err, b)
		}
		b, _ := os.ReadFile(txt)
		text := string(b)
		for _, want := range []string{"func main() {", "x := load()", "The entry point.", "Reads the input."} {
			if !strings.Contains(text, want) {
				t.Errorf("%q is not in the PDF's text:\n%s", want, text)
			}
		}
		for _, w := range strings.Fields(text) {
			if w == "1" || w == "2" || w == "<1>" || w == "//" {
				t.Errorf("a disc or its marker in the PDF's text: %q in\n%s", w, text)
			}
		}
	} else {
		t.Log("pdftotext is not installed: the PDF's text is not read")
	}

	pptx := filepath.Join(dir, "o.pptx")
	if _, stderr, err := run(t, "build", md, "--pptx", "-o", pptx); err != nil {
		t.Fatal(err, stderr)
	}
	zr, err := zip.OpenReader(pptx)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	seen := false
	for _, f := range zr.File {
		if f.Name != "ppt/slides/slide1.xml" {
			continue
		}
		seen = true
		rc, _ := f.Open()
		xml, _ := io.ReadAll(rc)
		rc.Close()
		s := string(xml)
		if strings.Contains(s, "<p:pic>") {
			t.Error("a disc is drawn as a picture: it must stay an editable shape")
		}
		if !strings.Contains(s, "<a:t>2</a:t>") || !strings.Contains(s, "Reads the input.") {
			t.Error("the discs' numbers and the notes are editable text of the slide")
		}
		if strings.Contains(s, "&lt;1&gt;") {
			t.Error("a marker is left in the code")
		}
	}
	if !seen {
		t.Fatal("no slide1.xml in the PowerPoint")
	}
}
