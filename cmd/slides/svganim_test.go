package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const animatedSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><rect id="r" x="40" y="10" width="20" height="40" fill="#c00">
<animateTransform attributeName="transform" type="rotate" from="0 50 50" to="360 50 50" dur="2s" repeatCount="indefinite"/></rect></svg>`

// An image a deck names that the options do not allow is said, with its file
// and the option that allows it, in every export that draws it
// (go-slides/slides#97); with the option, the animation is carried.
func TestLocalSVGBlockedWithoutTheOptionAndAnimatedWithIt(t *testing.T) {
	dir := t.TempDir()
	deck := filepath.Join(dir, "deck.md")
	if err := os.WriteFile(deck, []byte("# One\n\n![w:300](anim.svg)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "anim.svg"), []byte(animatedSVG), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, format := range []string{"--pptx", "--odp", "--pdf"} {
		_, stderr, err := run(t, "build", format, "-o", filepath.Join(dir, "out"+format[1:]), deck)
		if err != nil {
			t.Fatalf("%s: %v\n%s", format, err, stderr)
		}
		for _, want := range []string{"local-file-blocked", `"anim.svg"`, "--allow-local-files"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("%s: stderr lacks %q:\n%s", format, want, stderr)
			}
		}
	}
	for format, want := range map[string]string{
		"--pptx": "animated natively in PPTX",
		"--odp":  "animated natively in ODP",
		"--pdf":  "drawn at its initial state",
	} {
		_, stderr, err := run(t, "build", format, "--allow-local-files", "-o", filepath.Join(dir, "ok"+format[1:]), deck)
		if err != nil {
			t.Fatalf("%s: %v\n%s", format, err, stderr)
		}
		if !strings.Contains(stderr, want) || strings.Contains(stderr, "local-file-blocked") {
			t.Errorf("%s: stderr:\n%s\nwant %q and no blocked file", format, stderr, want)
		}
		// one line for the image
		if n := strings.Count(stderr, "svg-animation:"); n != 1 {
			t.Errorf("%s: %d svg-animation lines, want 1:\n%s", format, n, stderr)
		}
	}
}
