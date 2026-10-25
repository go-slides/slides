package export

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-pdfkit/reader"
	"github.com/go-webengine/engine"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
)

const taggedDeck = `---
title: Tagged
lang: en
paginate: true
---

# A deck

See [the site](https://example.com/), and $x^2$.

- one
- two

<!-- a note -->

---

## Code

` + "```go\nx := 1 // <1>\n```\n\n1. A variable.\n"

// structureOf reads a PDF's structure tree back: the elements' types,
// depth first, with a formula's or a figure's /Alt in brackets.
func structureOf(t *testing.T, b []byte) []string {
	t.Helper()
	doc, err := reader.Open(b)
	if err != nil {
		t.Fatal(err)
	}
	cat, _ := doc.Catalog()
	root, ok := doc.GetDict(cat, "StructTreeRoot")
	if !ok {
		return nil
	}
	var out []string
	var walk func(o reader.Object)
	walk = func(o reader.Object) {
		ref, ok := o.(reader.Ref)
		if !ok {
			return
		}
		v, _ := doc.Get(ref)
		e, _ := reader.ToDict(v)
		if typ, _ := reader.ToName(e["Type"]); typ != "StructElem" {
			return
		}
		s, _ := reader.ToName(e["S"])
		desc := string(s)
		if alt, ok := reader.TextString(e["Alt"]); ok {
			desc += "[" + alt + "]"
		}
		out = append(out, desc)
		if kids, ok := e["K"].(reader.Array); ok {
			for _, k := range kids {
				walk(k)
			}
		} else {
			walk(e["K"])
		}
	}
	walk(root["K"])
	return out
}

func taggedPDF(t *testing.T, opts PDFOptions) []byte {
	t.Helper()
	built, err := slides.Render([]byte(taggedDeck), slides.Config{}, deck.FrameBox)
	if err != nil {
		t.Fatal(err)
	}
	b := built.Doc()
	p := NewPDFWith(b, opts)
	if err := EachLayout(context.Background(), b, Options{}, func(_ int, l *engine.PageLayout) error { return p.AddLayout(l) }); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := p.Write(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A vector PDF is tagged by default: a section a slide, its heading,
// paragraph, link, formula (its TeX as its /Alt) and list, the code without
// its callout disc, the note's annotation; the page numbers are artifacts.
// The file declares PDF/UA-1 with the deck's title and language.
func TestVectorPDFIsTagged(t *testing.T) {
	b := taggedPDF(t, PDFOptions{Notes: true})
	got := strings.Join(structureOf(t, b), " ")
	want := "Document Sect H1 P Link Formula[x^2] L LI Lbl LBody LI Lbl LBody Sect H2 P Code L LI LBody Annot"
	if got != want {
		t.Errorf("structure\n got %s\nwant %s", got, want)
	}
	for _, s := range []string{"/Lang (en)", "/DisplayDocTitle true", "<pdfuaid:part>1</pdfuaid:part>", "/MarkInfo"} {
		if !bytes.Contains(b, []byte(s)) && !strings.Contains(string(inflate(t, b)), s) {
			t.Errorf("no %q", s)
		}
	}
}

// Untagged, the PDF carries no structure, as before.
func TestVectorPDFUntagged(t *testing.T) {
	b := taggedPDF(t, PDFOptions{Untagged: true})
	if s := structureOf(t, b); s != nil {
		t.Errorf("structure in an untagged PDF: %v", s)
	}
}

// With veraPDF installed, the tagged PDF passes its PDF/UA-1 profile.
func TestVectorPDFPassesVeraPDF(t *testing.T) {
	bin, err := exec.LookPath("verapdf")
	if err != nil {
		t.Skip("verapdf not installed")
	}
	in := filepath.Join(t.TempDir(), "deck.pdf")
	if err := os.WriteFile(in, taggedPDF(t, PDFOptions{Notes: true}), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command(bin, "--flavour", "ua1", "--format", "text", "-v", in).CombinedOutput()
	if !strings.HasPrefix(strings.TrimSpace(string(out)), "PASS") {
		t.Errorf("veraPDF:\n%s", out)
	}
}

// inflate is the file with its object streams' contents readable: the
// catalog may sit in one.
func inflate(t *testing.T, b []byte) []byte {
	t.Helper()
	doc, err := reader.Open(b)
	if err != nil {
		t.Fatal(err)
	}
	cat, _ := doc.Catalog()
	var buf []byte
	buf = reader.AppendObject(buf, cat)
	if md, ok := cat["Metadata"].(reader.Ref); ok {
		if o, err := doc.Get(md); err == nil {
			if s, ok := reader.ToStream(o); ok {
				if data, _, err := doc.DecodeStream(s); err == nil {
					buf = append(buf, data...)
				}
			}
		}
	}
	if vp, ok := doc.GetDict(cat, "ViewerPreferences"); ok {
		buf = reader.AppendObject(buf, vp)
	}
	if mi, ok := doc.GetDict(cat, "MarkInfo"); ok {
		buf = append(buf, "/MarkInfo"...)
		buf = reader.AppendObject(buf, mi)
	}
	return buf
}
