package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// exampleSlides is how many slides each deck in examples/ has. A deck that
// gains or loses a slide changes this table in the same commit, so a template
// or a directive that stops producing slides cannot go unseen.
var exampleSlides = map[string]int{
	"code":          6,
	"emoji-and-fit": 6,
	"hcl":           4,
	"images":        7,
	"kinds":         6,
	"math":          6,
	"presenting":    6,
	"talk":          10,
	"templates":     7,
	"theme-default": 4,
	"theme-gaia":    4,
	"theme-uncover": 5,
}

var metaRE = func(key string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^` + key + `\s*[:=]\s*\S`)
}

// TestExamples builds every deck of examples/ to HTML and to PDF, as the
// documentation's gallery and the playground take them, and checks that each
// is listed in examples/README.md and carries the title and description the
// gallery shows.
func TestExamples(t *testing.T) {
	decks, err := filepath.Glob("../../examples/*/deck.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(decks) != len(exampleSlides) {
		t.Fatalf("examples/ has %d decks, the table lists %d", len(decks), len(exampleSlides))
	}
	readme, err := os.ReadFile("../../examples/README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, deck := range decks {
		name := filepath.Base(filepath.Dir(deck))
		t.Run(name, func(t *testing.T) {
			want, ok := exampleSlides[name]
			if !ok {
				t.Fatalf("%s is not in the table", name)
			}
			if !strings.Contains(string(readme), "**"+name+"**") {
				t.Errorf("examples/README.md has no line for %s", name)
			}
			src, err := os.ReadFile(deck)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"title", "description"} {
				if !metaRE(key).Match(src) {
					t.Errorf("the front matter has no %s", key)
				}
			}
			dir := t.TempDir()
			page := filepath.Join(dir, "deck.html")
			_, stderr, err := run(t, "build", "--allow-local-files", "-o", page, deck)
			if err != nil {
				t.Fatal(err, stderr)
			}
			if !strings.Contains(stderr, fmt.Sprintf("(%d slides)", want)) {
				t.Errorf("want %d slides: %q", want, stderr)
			}
			html, err := os.ReadFile(page)
			if err != nil {
				t.Fatal(err)
			}
			for _, bad := range []string{"<no value>", "{{ ", "{{."} {
				if strings.Contains(string(html), bad) && name != "templates" {
					t.Errorf("the page contains %q", bad)
				}
			}
			pdf := filepath.Join(dir, "deck.pdf")
			if _, stderr, err := run(t, "build", "--allow-local-files", "-o", pdf, deck); err != nil {
				t.Fatal(err, stderr)
			}
			b, err := os.ReadFile(pdf)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(b), "%PDF-") {
				t.Errorf("not a PDF: %q", b[:min(8, len(b))])
			}
		})
	}
}
