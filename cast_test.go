package slides

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/diag"
	"github.com/go-slides/slides/vfs"
	"github.com/go-slides/slides/viewer"
)

const testCast = `{"version": 2, "width": 20, "height": 3}
[0.5, "o", "hello \u001b[31mred\u001b[0m\r\n"]
[1.5, "o", "second line"]
`

func castCfg(files map[string]string) Config {
	m := map[string][]byte{}
	for k, v := range files {
		m[k] = []byte(v)
	}
	return Config{Parse: deck.Options{FS: vfs.NewMap(m)}}
}

func TestCastImage(t *testing.T) {
	page, b, err := HTML([]byte("# T\n\n![A demo](demo.cast#t=1.0)\n"), castCfg(map[string]string{"demo.cast": testCast}))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Warnings) != 0 {
		t.Fatalf("warnings %v", b.Warnings)
	}
	for _, want := range []string{
		`class="slides-cast"`, `aria-label="A demo"`, `data-poster="1"`, `class="slides-cast-screen"`,
		`color:#e04b4b`, // red, in the screen's markup
		`<script type="application/json" class="slides-cast-data">`,
		`prefers-reduced-motion`, `slides-cast-ctl`, // the player and its style
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page lacks %q", want)
		}
	}
	// at 1.0 s the second event has not played
	if strings.Contains(page[:strings.Index(page, `slides-cast-data`)], "second line") {
		t.Error("the poster shows the end, not #t=1.0")
	}
	// the frames are valid JSON with the second line in them
	m := regexp.MustCompile(`(?s)class="slides-cast-data">(.*?)</script>`).FindStringSubmatch(page)
	if m == nil || !json.Valid([]byte(m[1])) || !strings.Contains(m[1], "second line") {
		t.Errorf("the frames: %.200s", m)
	}
	if strings.Contains(page, "<img") {
		t.Error("a recording is not an <img>")
	}
}

func TestCastDefaultIsTheLastFrame(t *testing.T) {
	b, err := Render([]byte("![cast](demo.cast)\n"), castCfg(map[string]string{"demo.cast": testCast}), deck.FrameBox)
	if err != nil {
		t.Fatal(err)
	}
	h := b.Slides[0].HTML
	if !strings.Contains(h, "second line") || strings.Contains(h, "slides-cast-data") || !strings.Contains(h, `aria-label="Terminal recording"`) {
		t.Errorf("%s", h)
	}
}

func TestCastErrorsAreCodedWarningsAndANote(t *testing.T) {
	cases := []struct{ name, src, file, code string }{
		{"missing", "![cast](nope.cast)", "", "file.missing"},
		{"syntax", "![cast](bad.cast)", "not a cast\n", "cast.syntax"},
		{"version", "![cast](v1.cast)", `{"version":1,"width":2,"height":2}`, "cast.version"},
		{"limit", "![cast](big.cast)", `{"version":2,"width":9999,"height":2}`, "cast.limit"},
		{"url", "![cast](https://example.com/a.cast)", "", "cast.path"},
		{"outside", "![cast](../secret.cast)", "", "cast.path"},
		{"absolute", "![cast](/etc/a.cast)", "", "cast.path"},
		{"bad t", "![cast](ok.cast#t=soon)", testCast, "cast.syntax"},
	}
	for _, c := range cases {
		files := map[string]string{"bad.cast": "not a cast\n", "v1.cast": `{"version":1,"width":2,"height":2}`, "big.cast": `{"version":2,"width":9999,"height":2}`, "ok.cast": testCast}
		b, err := Render([]byte(c.src+"\n"), castCfg(files), deck.FrameSVG)
		if err != nil {
			t.Fatalf("%s: a bad recording must not fail the deck: %v", c.name, err)
		}
		if len(b.Warnings) == 0 || diag.WarningCode(b.Warnings[0]) != c.code {
			t.Errorf("%s: warnings %q, want code %s", c.name, b.Warnings, c.code)
		}
		if !strings.Contains(b.Slides[0].HTML, "slides-cast-error") || strings.Contains(b.Slides[0].HTML, "slides-cast-data") {
			t.Errorf("%s: no note in the slide's place: %s", c.name, b.Slides[0].HTML)
		}
	}
}

// What a recording prints is text: no escape sequence, markup or script ends
// up as anything else in the page.
func TestCastHostileRecordingIsText(t *testing.T) {
	hostile := `{"version": 2, "width": 80, "height": 5, "title": "</script><b onmouseover=x>"}
[1, "o", "</script><script>alert(1)</script><img src=x onerror=alert(2)>\u001b]0;\u001b[31m</title>\u0007\u001b[8mhidden\u001b[0m"]
`
	page, _, err := HTML([]byte("![\"><svg onload=alert(3)>](h.cast)\n"), castCfg(map[string]string{"h.cast": hostile}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page, "<svg onload") || strings.Contains(page, `"><svg`) {
		t.Error("the alternative text broke out of its attribute")
	}
	// only our own script elements: the presenting page's, the player's and the data
	scripts := regexp.MustCompile(`(?i)<script[^>]*>`).FindAllString(page, -1)
	for _, s := range scripts {
		if s != "<script>" && s != `<script type="application/json" class="slides-cast-data">` {
			t.Errorf("a script element that is not ours: %s", s)
		}
	}
	if strings.Contains(page, "<img src=x") || strings.Contains(page, "<b onmouseover") || strings.Contains(page, "alert(1)</script") {
		t.Error("recorded text became markup")
	}
}

func TestCastDoesNotLeaveTheDecksFolder(t *testing.T) {
	cfg := castCfg(map[string]string{"/etc/secret.cast": testCast, "../x.cast": testCast})
	b, _ := Render([]byte("![cast](../x.cast)\n\n![cast](/etc/secret.cast)\n"), cfg, deck.FrameBox)
	if strings.Contains(b.Slides[0].HTML, "slides-cast-screen") {
		t.Error("read a file outside the deck's folder")
	}
}

func TestViewerNeedsThePlayerOnlyForCasts(t *testing.T) {
	if viewer.UsesCast("<p>x</p>") || !viewer.UsesCast(`<script class="slides-cast-data">`) {
		t.Error("UsesCast")
	}
	page, _, _ := HTML([]byte("# no recording\n"), Config{})
	if strings.Contains(page, "slides-cast") {
		t.Error("the player is in a deck without a recording")
	}
}

func TestMoments(t *testing.T) {
	for in, want := range map[string]float64{"12.5": 12.5, "0": 0, "1:23": 83, "2:00.5": 120.5} {
		if got, ok := parseMoment(in); !ok || got != want {
			t.Errorf("%q: %v %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "-1", "NaN", "Inf", "1e999", "a:b", "1:2:3", ":5"} {
		if _, ok := parseMoment(in); ok {
			t.Errorf("%q should not parse", in)
		}
	}
}

func FuzzCastImage(f *testing.F) {
	f.Add([]byte(testCast))
	f.Add([]byte("{\"version\":3,\"term\":{\"cols\":5,\"rows\":2}}\n[0.1,\"o\",\"\\u001b[31m<x>\"]\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		b, err := Render([]byte("![cast](f.cast)\n"), castCfg(map[string]string{"f.cast": string(data)}), deck.FrameSVG)
		if err != nil {
			t.Fatal(err)
		}
		h := b.Slides[0].HTML
		if strings.ContainsAny(h, "\x1b\x00") {
			t.Fatalf("a control character in the slide: %q", h)
		}
		if strings.Count(strings.ToLower(h), "<script") > 1 {
			t.Fatal("more than the one data element")
		}
	})
}
