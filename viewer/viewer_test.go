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
	bare := Document(Options{Bare: true})
	if strings.Contains(bare, "<script>") || !strings.Contains(bare, "<title>Slides</title>") || strings.Contains(bare, "<html lang") {
		t.Error("bare page: no script, default title, no lang")
	}
}
