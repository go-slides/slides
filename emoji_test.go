package slides

import (
	"strings"
	"testing"

	"github.com/go-slides/slides/deck"
)

func TestEmojiPage(t *testing.T) {
	page, b, err := HTML([]byte("# Hi :smile: 😄\n"), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(page, `data-marp-twemoji=""`); n != 2 {
		t.Errorf("%d emoji images in the page, want 2", n)
	}
	if strings.Contains(page, "cdn.jsdelivr.net") {
		t.Error("the page points at a CDN")
	}
	if !strings.Contains(b.Packed.CSS, "img[data-marp-twemoji]") {
		t.Error("the emoji rule is not in the style sheet")
	}
}

func TestNoEmojiPage(t *testing.T) {
	for _, cfg := range []Config{{NoEmoji: true}, {Parse: deck.Options{NoEmoji: true}}} {
		page, b, err := HTML([]byte("# Hi :smile: 😄\n"), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(page, "data-marp-twemoji") || strings.Contains(b.Packed.CSS, "twemoji") {
			t.Errorf("emoji machinery despite NoEmoji: %v", cfg)
		}
		if !strings.Contains(page, ":smile: 😄") {
			t.Errorf("the text was changed: %v", cfg)
		}
	}
}
