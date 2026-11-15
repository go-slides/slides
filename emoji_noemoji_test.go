//go:build slides_noemoji

package slides

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/go-slides/slides/emoji"
)

const emojiDeck = "# Hello :smile: 😄\n\nA family 👨‍👩‍👧, `code 😄`, and $x^2$.\n"

// Built with slides_noemoji no emoji provider is linked: the emoji of a deck
// stay the text that was written, and the rest of the deck renders.
func TestNoEmojiBuildLeavesEmojiAsText(t *testing.T) {
	if emoji.Registered() {
		t.Fatal("a provider is registered in a build without the embedded one")
	}
	page, b, err := HTML([]byte(emojiDeck), Config{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page, `data-marp-twemoji=""/>`) {
		t.Error("an emoji image without a provider")
	}
	for _, want := range []string{":smile:", "😄", "👨‍👩‍👧", `class="slides-math"`} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	if len(b.Warnings) != 0 {
		t.Errorf("warnings: %q", b.Warnings)
	}
}

// TestNoEmojiBuildLinksNoTwemoji: the module a WebAssembly host builds with the
// tag holds none of go-icons/twemoji, and so none of its 2 MB.
func TestNoEmojiBuildLinksNoTwemoji(t *testing.T) {
	for _, goos := range []string{"", "js"} {
		cmd := exec.Command("go", "list", "-deps", "-tags", "slides_noemoji", ".")
		if goos != "" {
			cmd.Env = append(cmd.Environ(), "GOOS=js", "GOARCH=wasm")
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go list: %v\n%s", err, out)
		}
		linked := 0
		for _, line := range strings.Split(string(out), "\n") {
			if line == "github.com/go-slides/slides/emoji" {
				linked++
			}
			if strings.Contains(line, "go-icons/twemoji") || strings.HasSuffix(line, "/emoji/embedded") {
				t.Errorf("GOOS=%q: slides_noemoji still links %s", goos, line)
			}
		}
		if linked != 1 {
			t.Errorf("GOOS=%q: package emoji is not linked (the parsing stays)", goos)
		}
	}
}
