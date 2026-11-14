//go:build !slides_novideo

package slides

import (
	"os"
	"strings"
	"testing"

	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/diag"
	"github.com/go-slides/slides/vfs"
	"github.com/go-slides/slides/viewer"
)

func videoBuild(t *testing.T, src string, frame deck.Frame, files ...string) *Build {
	t.Helper()
	m := map[string][]byte{}
	for _, f := range files {
		b, err := os.ReadFile("testdata/video/" + f)
		if err != nil {
			t.Fatal(err)
		}
		m[f] = b
	}
	b, err := Render([]byte(src), Config{Parse: deck.Options{FS: vfs.NewMap(m)}}, frame)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestVideoHTML(t *testing.T) {
	b := videoBuild(t, "![my clip loop muted autoplay](bframes.mp4#t=0.3)\n", deck.FrameSVG, "bframes.mp4")
	h := b.Slides[0].HTML
	for _, want := range []string{`<video class="slides-video" controls playsinline`, `poster="data:image/jpeg;base64,`, `src="bframes.mp4"`, ` muted`, ` autoplay`, ` loop`, `aria-label="my clip"`} {
		if !strings.Contains(h, want) {
			t.Errorf("HTML lacks %q", want)
		}
	}
	if len(b.Warnings) > 0 || len(b.Media) != 1 {
		t.Errorf("warnings %v, media %d", b.Warnings, len(b.Media))
	}
	if !viewer.UsesVideo(h) {
		t.Error("UsesVideo")
	}
}

func TestVideoAutoplayNeedsMuted(t *testing.T) {
	b := videoBuild(t, "![autoplay](bframes.mp4)\n", deck.FrameSVG, "bframes.mp4")
	if strings.Contains(b.Slides[0].HTML, " autoplay") || len(b.Warnings) != 1 || diag.WarningCode(b.Warnings[0]) != "video.syntax" {
		t.Errorf("autoplay without muted: %v", b.Warnings)
	}
}

func TestVideoRefusals(t *testing.T) {
	for dest, code := range map[string]string{
		"../x.mp4":          "video.path",
		"/etc/x.mp4":        "video.path",
		"ftp://h/x.mp4":     "video.path",
		"bframes.mp4#t=abc": "video.syntax",
		"missing.mp4":       "file.missing",
		"notavideo.mp4":     "video.syntax",
	} {
		b := videoBuild(t, "![video]("+dest+")\n", deck.FrameSVG, "bframes.mp4", "notavideo.mp4")
		if !strings.Contains(b.Slides[0].HTML, "video not shown") || len(b.Warnings) == 0 || diag.WarningCode(b.Warnings[0]) != code {
			t.Errorf("%s: want %s, warnings %v", dest, code, b.Warnings)
		}
	}
}

func TestVideoOtherCodecKeepsVideoWithCard(t *testing.T) {
	b := videoBuild(t, "![video](hvc1.mp4)\n", deck.FrameSVG, "hvc1.mp4")
	if !strings.Contains(b.Slides[0].HTML, "<video") || len(b.Warnings) != 1 || diag.WarningCode(b.Warnings[0]) != "video.codec" {
		t.Errorf("hvc1: %v", b.Warnings)
	}
}

func TestVideoURLIsLinked(t *testing.T) {
	b := videoBuild(t, "![video](https://example.com/a.webm)\n", deck.FrameBox)
	if !strings.Contains(b.Slides[0].HTML, `href="https://example.com/a.webm"`) || len(b.Media) != 0 {
		t.Errorf("url: %s", b.Slides[0].HTML)
	}
}
