package viewer

import (
	"strings"
	"testing"
)

func TestDocument(t *testing.T) {
	page := Document(Options{
		Meta:   Meta{Title: `A "deck"`, Description: "d", Author: "a", Keywords: "k", URL: "https://x/", Image: "https://x/i.png", Lang: "fr"},
		CSS:    "section{}",
		Slides: []string{"<svg data-marpit-svg></svg>", "<svg data-marpit-svg></svg>"},
	})
	for _, want := range []string{
		`<html lang="fr">`, `<title>A &#34;deck&#34;</title>`, `<meta name="description" content="d">`,
		`<meta property="og:image" content="https://x/i.png">`, `<link rel="canonical" href="https://x/">`,
		"<style>section{}</style>", "<script>", `<div class="marpit">`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("missing %q", want)
		}
	}
	fit := Document(Options{Bare: true, Slides: []string{`<h1 data-auto-scaling=""><span data-fit-content="">x</span></h1>`}})
	if !strings.Contains(fit, "slides-fitting") {
		t.Error("a page with a fitting heading carries the fit script, bare or not")
	}
	bare := Document(Options{Bare: true})
	if strings.Contains(bare, "<script>") || strings.Contains(bare, "<title>") || strings.Contains(bare, "<html lang") {
		t.Error("bare page: no script, no title (as marp-cli's), no lang")
	}
	// marp-cli writes the keywords from the second on, og:image:alt and the
	// large Twitter card only with a title and an image.
	if strings.Contains(page, `name="keywords"`) || !strings.Contains(page, `content="summary_large_image"`) || !strings.Contains(page, `og:image:alt`) {
		t.Error("one keyword is not written; a title and an image make the large card and the image alt")
	}
	two := Document(Options{Meta: Meta{Title: "t", Keywords: "a,b"}})
	if !strings.Contains(two, `<meta name="keywords" content="a,b">`) || !strings.Contains(two, `content="summary"`) || strings.Contains(two, "og:image:alt") {
		t.Error("two keywords are written; no image, the small card")
	}
}
