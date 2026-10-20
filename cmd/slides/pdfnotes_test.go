package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rsc.io/pdf"

	"github.com/go-slides/slides/deck"
)

// dumpPDF writes the outline and the annotations of a PDF as text: one line
// a bookmark, indented by depth, "*" when it is bold; one line a note.
func dumpPDF(t *testing.T, b []byte) string {
	t.Helper()
	r, err := pdf.NewReader(strings.NewReader(string(b)), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	var walk func(item pdf.Value, depth int)
	walk = func(item pdf.Value, depth int) {
		for ; item.Kind() != pdf.Null; item = item.Key("Next") {
			bold := ""
			if item.Key("F").Int64()&2 != 0 {
				bold = " *"
			}
			fmt.Fprintf(&sb, "outline %s%s%s\n", strings.Repeat("  ", depth), item.Key("Title").Text(), bold)
			walk(item.Key("First"), depth+1)
		}
	}
	walk(r.Trailer().Key("Root").Key("Outlines").Key("First"), 0)
	for i := 1; i <= r.NumPage(); i++ {
		annots := r.Page(i).V.Key("Annots")
		for j := 0; j < annots.Len(); j++ {
			a := annots.Index(j)
			if a.Key("Subtype").Name() != "Text" {
				continue
			}
			rect := a.Key("Rect")
			c := a.Key("C")
			fmt.Fprintf(&sb, "note page=%d contents=%q T=%q Subj=%q Name=%s Rect=[%g %g %g %g] C=[%g %g %g] CA=%g\n",
				i, a.Key("Contents").Text(), a.Key("T").Text(), a.Key("Subj").Text(), a.Key("Name").Name(),
				rect.Index(0).Float64(), rect.Index(1).Float64(), rect.Index(2).Float64(), rect.Index(3).Float64(),
				c.Index(0).Float64(), c.Index(1).Float64(), c.Index(2).Float64(), a.Key("CA").Float64())
		}
	}
	return sb.String()
}

// TestPDFNotesOracle compares the outline and the note annotations of the PDFs
// the build writes with those marp-cli 4.5.1 writes for the same deck
// (testdata/oracle/gen-cli.js): the bookmarks' titles, nesting and weight, the
// notes' contents, author, icon, colour, opacity and place.
func TestPDFNotesOracle(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "oracle")
	for _, c := range []struct {
		name string
		args []string
	}{
		{"both", []string{"--pdf-outlines", "--pdf-notes"}},
		{"pages", []string{"--pdf-outlines", "--no-pdf-outlines.headings"}},
		{"headings", []string{"--pdf-outlines", "--no-pdf-outlines.pages"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join(dir, "pdf-"+c.name+".pdf"))
			if err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(t.TempDir(), "o.pdf")
			args := append([]string{"build", filepath.Join(dir, "pdf.src.md"), "--pdf", "-o", out}, c.args...)
			if _, stderr, err := run(t, args...); err != nil {
				t.Fatal(err, stderr)
			}
			got, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if g, w := dumpPDF(t, got), dumpPDF(t, want); g != w {
				t.Errorf("outline and notes differ from marp-cli's.\nours:\n%s\nmarp-cli:\n%s", g, w)
			}
		})
	}
}

// Without --pdf-notes there are no annotations, without --pdf-outlines=false
// there is an outline; --pdf-outlines=false and marp-cli's --no-pdf-outlines
// drop it; --pdf-notes alone asks for a PDF.
func TestPDFOptions(t *testing.T) {
	dir := t.TempDir()
	md := write(t, dir, "d.md", "# One\n\n<!-- a note -->\n\n---\n\n## Two\n")
	build := func(name string, args ...string) string {
		t.Helper()
		out := filepath.Join(dir, name)
		if _, stderr, err := run(t, append([]string{"build", md, "-o", out}, args...)...); err != nil {
			t.Fatal(err, stderr)
		}
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		return dumpPDF(t, b)
	}
	if d := build("a.pdf"); !strings.Contains(d, "outline Page 1 *") || strings.Contains(d, "note ") {
		t.Errorf("default: the outline and no notes:\n%s", d)
	}
	if d := build("b.pdf", "--pdf-outlines=false"); d != "" {
		t.Errorf("--pdf-outlines=false:\n%s", d)
	}
	if d := build("c.pdf", "--no-pdf-outlines", "--pdf-notes"); !strings.HasPrefix(d, "note page=1 contents=\"a note\"") || strings.Contains(d, "outline") {
		t.Errorf("--no-pdf-outlines --pdf-notes:\n%s", d)
	}
	// no -o: the PDF is chosen by --pdf-notes alone, written next to the deck
	if _, stderr, err := run(t, "build", md, "--pdf-notes"); err != nil {
		t.Fatal(err, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "d.pdf")); err != nil {
		t.Error("--pdf-notes alone should write d.pdf:", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "d.html")); err == nil {
		t.Error("--pdf-notes alone should not write HTML")
	}
}

// TestNotesOracle compares the text --notes writes with marp-cli's.
func TestNotesOracle(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "oracle", "notes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct{ Name, MD, Text string }
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			dir := t.TempDir()
			md := write(t, dir, "d.md", c.MD)
			stdout, stderr, err := run(t, "build", md, "--notes", "-o", "-")
			if err != nil {
				t.Fatal(err, stderr)
			}
			if stdout != c.Text {
				t.Errorf("notes = %q, marp-cli writes %q", stdout, c.Text)
			}
			if c.Text == "" && !strings.Contains(stderr, "contains no notes") {
				t.Errorf("a deck without notes should say so: %q", stderr)
			}
			// the default output is d.txt next to the deck, chosen by -o too
			if _, _, err := run(t, "build", md, "--notes"); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(dir, "d.txt"))
			if err != nil || string(got) != c.Text {
				t.Errorf("d.txt = %q, %v", got, err)
			}
			if _, _, err := run(t, "build", md, "-o", filepath.Join(dir, "x.txt")); err != nil {
				t.Fatal(err)
			}
			if got, err := os.ReadFile(filepath.Join(dir, "x.txt")); err != nil || string(got) != c.Text {
				t.Errorf("-o x.txt = %q, %v", got, err)
			}
		})
	}
}

func FuzzNotesText(f *testing.F) {
	f.Add("# a\n\n<!-- n -->\n\n---\n\nb\n")
	f.Add("<!-- ")
	f.Fuzz(func(t *testing.T, md string) {
		d, err := deck.Parse([]byte(md), deck.Options{})
		if err != nil {
			return
		}
		_ = notesText(d)
	})
}
