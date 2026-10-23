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
		if !strings.Contains(p, "pre.line-numbers .ln {") || !strings.Contains(p, `<span class="ln" aria-hidden="true">1</span>`) {
			t.Errorf("%s: style or numbers missing", theme)
		}
		if !strings.Contains(p, "color: color-mix(in srgb,") {
			t.Errorf("%s: the numbers are not muted by a mixed colour", theme)
		}
		// highlighted by go-rouge in its hljs classes, inside the numbered block
		if !strings.Contains(p, `<span class="hljs-keyword">func</span>`) {
			t.Errorf("%s: the code is not highlighted", theme)
		}
	}
}

// In the exports the numbers are not in the code's text: the PDF's text
// (read by pdftotext, when it is installed) has none of them. A reader who
// selects the code in the HTML page does not get them: user-select is none.
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

	// In the PowerPoint and the OpenDocument file the numbers are editable
	// text, each a run of its own at the start of its line's paragraph, and
	// nothing of the block is a picture: a muted colour, not an opacity,
	// which the office canvas would have drawn as one.
	for _, c := range []struct{ flag, entry, picture string }{
		{"--pptx", "ppt/slides/slide1.xml", "<p:pic>"},
		{"--odp", "content.xml", "<draw:image"},
	} {
		out := filepath.Join(dir, "o"+strings.TrimPrefix(c.flag, "--")[:0]+"."+strings.TrimPrefix(c.flag, "--"))
		if _, stderr, err := run(t, "build", md, c.flag, "-o", out); err != nil {
			t.Fatal(err, stderr)
		}
		xml := zipEntry(t, out, c.entry)
		if strings.Contains(xml, c.picture) {
			t.Errorf("%s: a picture in the slide", c.flag)
		}
		for _, n := range []string{"1", "2", "3"} {
			if !strings.Contains(xml, ">"+n+"<") && !strings.Contains(xml, ">"+n+" <") {
				t.Errorf("%s: number %s is not a text run of its own", c.flag, n)
			}
		}
	}
}

// zipEntry reads one file of a zip archive.
func zipEntry(t *testing.T, archive, name string) string {
	t.Helper()
	zr, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
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
	t.Fatalf("no %s in %s", name, archive)
	return ""
}
