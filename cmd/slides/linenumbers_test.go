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

const numberedDeck = "# Code\n\n```go {lineNumbers}\nfunc main() {\n\treturn\n}\n```\n"

// A page without the option carries nothing of it; with it, the style, once.
func TestLineNumbersPage(t *testing.T) {
	dir := t.TempDir()
	build := func(md string, args ...string) string {
		t.Helper()
		p := write(t, dir, "d.md", md)
		out := filepath.Join(dir, "o.html")
		if _, stderr, err := run(t, append([]string{"build", p, "-o", out}, args...)...); err != nil {
			t.Fatal(err, stderr)
		}
		b, _ := os.ReadFile(out)
		return string(b)
	}
	if p := build("# Code\n\n```go\nx\n```\n"); strings.Contains(p, "line-numbers") || strings.Contains(p, "ln-gutter") {
		t.Error("a deck without the option has no trace of it")
	}
	for _, theme := range []string{"default", "gaia", "uncover"} {
		p := build(numberedDeck, "--theme", theme)
		if strings.Count(p, "pre.line-numbers .ln-gutter") != 1 || !strings.Contains(p, `<span class="ln-gutter" aria-hidden="true">1`) {
			t.Errorf("%s: style or gutter missing", theme)
		}
		// highlighted by go-rouge in its hljs classes, inside the numbered block
		if !strings.Contains(p, `<span class="hljs-keyword">func</span>`) {
			t.Errorf("%s: the code is not highlighted", theme)
		}
	}
}

// In the exports the numbers are not interleaved with the code: the PDF's
// text (read by pdftotext, when it is installed) has none of them, in the
// PPTX they are paragraphs apart from the code's.
// They are still text; a reader who selects the code in the HTML page does not
// get them: user-select is none.
func TestLineNumbersInExports(t *testing.T) {
	dir := t.TempDir()
	md := write(t, dir, "d.md", numberedDeck)

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
		lines := strings.Fields(string(b))
		first := -1
		for i, w := range lines {
			if w == "func" {
				first = i
				break
			}
		}
		if first < 0 {
			t.Fatalf("no code in %q", lines)
		}
		// The gutter is user-select:none, which go-webengine v0.13.0 draws
		// in an empty ActualText span: drawn, not in the text at all.
		for _, w := range lines {
			if w == "1" || w == "2" || w == "3" {
				t.Errorf("a line number in the PDF's text: %q", lines)
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
	for _, f := range zr.File {
		if f.Name != "ppt/slides/slide1.xml" {
			continue
		}
		rc, _ := f.Open()
		xml, _ := io.ReadAll(rc)
		rc.Close()
		for _, para := range strings.Split(string(xml), "</a:p>") {
			if strings.Contains(para, "<a:t>func</a:t>") && strings.Contains(para, "<a:t>1</a:t>") {
				t.Errorf("a line number in the code's paragraph: %s", para)
			}
		}
		if !strings.Contains(string(xml), "<a:t>1</a:t>") {
			t.Error("the numbers are in the slide, as text of their own")
		}
	}
}
