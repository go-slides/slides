//go:build slides_novideo

package slides

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/vfs"
)

// Built with slides_novideo, no video provider is linked: a video in a deck is
// refused with video.unavailable, and the rest of the deck renders.
func TestNoVideoBuildRefusesAVideoWithACode(t *testing.T) {
	clip, err := os.ReadFile("testdata/video/bframes.mp4")
	if err != nil {
		t.Fatal(err)
	}
	fs := vfs.NewMap(map[string][]byte{"bframes.mp4": clip})
	b, err := Render([]byte("# One\n\n![video](bframes.mp4)\n"), Config{Parse: deck.Options{FS: fs}}, deck.FrameSVG)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range b.Warnings {
		if strings.HasPrefix(w, "video.unavailable") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no video.unavailable in %q", b.Warnings)
	}
}

// TestNoVideoBuildLinksNoAvkit: the module a WebAssembly host builds with the
// tag holds none of go-avkit, nor the container libraries under it.
func TestNoVideoBuildLinksNoAvkit(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "-tags", "slides_novideo", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		for _, bad := range []string{"go-avkit", "mp4ff", "astits", "ebml-go", "pion/opus", "go-mp3"} {
			if strings.Contains(line, bad) {
				t.Errorf("slides_novideo still links %s", line)
			}
		}
	}
}
