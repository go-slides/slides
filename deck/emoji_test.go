package deck

import (
	"encoding/base64"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/go-icons/twemoji"
)

var emojiImg = regexp.MustCompile(`<img class="emoji" draggable="false" alt="([^"]*)" src="data:image/svg\+xml;base64,([A-Za-z0-9+/=]+)" data-marp-twemoji=""/>`)

// emojiAlts lists the alt of every emoji image in html, after checking that
// each carries the SVG of its own graphic.
func emojiAlts(t *testing.T, html string) []string {
	t.Helper()
	var alts []string
	for _, m := range emojiImg.FindAllStringSubmatch(html, -1) {
		svg, err := base64.StdEncoding.DecodeString(m[2])
		if err != nil {
			t.Fatal(err)
		}
		want, ok := twemoji.SVG(m[1])
		if !ok || want != string(svg) {
			t.Errorf("the image of %q is not its graphic", m[1])
		}
		alts = append(alts, m[1])
	}
	return alts
}

func emojiOf(t *testing.T, src string, opts Options) string {
	t.Helper()
	opts.NoTemplate = true
	d := parseDeck(t, src, opts)
	return d.Render(RenderOptions{Frame: FramePlain})[0].HTML
}

// EmojiCSS is the rule Marp Core wrote in testdata/oracle/gen.js's run.
func TestEmojiCSSIsMarpCores(t *testing.T) {
	want, err := os.ReadFile("../testdata/oracle/emoji.rule.css")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(want)); got != EmojiCSS {
		t.Errorf("EmojiCSS\n got: %s\nwant: %s", EmojiCSS, got)
	}
}

func TestEmojify(t *testing.T) {
	cases := []struct {
		in         string
		shortcodes bool
		want       []string // alts, in order
		rest       string   // what is left of the text, images removed
	}{
		{"plain text", true, nil, "plain text"},
		{"café déjà vu", true, nil, "café déjà vu"},
		{":smile:", true, []string{"😄"}, ""},
		{"a :smile: b :+1: c", true, []string{"😄", "👍"}, "a  b  c"},
		{":smile:", false, nil, ":smile:"},
		{":nope: :smile", true, nil, ":nope: :smile"},
		{"12:30:45", true, nil, "12:30:45"},
		{":" + strings.Repeat("x", 100) + ":smile:", true, []string{"😄"}, ":" + strings.Repeat("x", 100)},
		{"😄 and 👩‍💻 and 🇫🇷", true, []string{"😄", "👩‍💻", "🇫🇷"}, " and  and "},
		{"1️⃣ 12 #️⃣ *", true, []string{"1️⃣", "#️⃣"}, " 12  *"},
		{"© ™ ✔ ✔️", true, []string{"✔️"}, "© ™ ✔ "},
		{"☺︎ ❤ ❤️", true, []string{"❤", "❤️"}, "☺︎  "},
		{"\xff😄", true, []string{"😄"}, "\xff"},
	}
	for _, c := range cases {
		got := emojify(c.in, c.shortcodes)
		alts := emojiAlts(t, got)
		rest := emojiImg.ReplaceAllString(got, "")
		if strings.Join(alts, "|") != strings.Join(c.want, "|") || rest != c.rest {
			t.Errorf("emojify(%q, %v): images %q, rest %q; want %q, %q", c.in, c.shortcodes, alts, rest, c.want, c.rest)
		}
	}
}

func TestEmojifyHTML(t *testing.T) {
	got := emojifyHTML(`<span class="x" title="😄">😄 &lt;b&gt;</span> 🚀<` + "\n")
	if alts := emojiAlts(t, got); strings.Join(alts, "") != "😄🚀" {
		t.Errorf("alts %q in %.200s", alts, got)
	}
	if !strings.Contains(got, `<span class="x" title="😄">`) {
		t.Errorf("a tag was rewritten: %.200s", got)
	}
	if !strings.HasSuffix(got, "<\n") {
		t.Errorf("an unclosed < was lost: %q", got[len(got)-5:])
	}
	if emojifyHTML("<b>ascii</b>") != "<b>ascii</b>" {
		t.Error("ascii HTML changed")
	}
}

func TestExpandShortcodes(t *testing.T) {
	for in, want := range map[string]string{
		"no colon":      "no colon",
		"a :tada: b":    "a 🎉 b",
		":nope:":        ":nope:",
		":heart_eyes:!": "😍!",
	} {
		if got := expandShortcodes(in); got != want {
			t.Errorf("expandShortcodes(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderEmoji(t *testing.T) {
	html := emojiOf(t, "# Title :tada:\n\nText :heart_eyes: and 😄, `code 🚀 :smile:`.\n\n```go\nfmt.Println(\"🎉 :smile:\")\n```\n\n    indented 🐱\n\n> quote :cat:\n\n[l :dog: 🐶](https://example.com) <https://example.com/:smile:>\n\n![alt 😄](a.png)\n", Options{})
	got := strings.Join(emojiAlts(t, html), " ")
	want := "🎉 😍 😄 🚀 🎉 🐱 🐱 🐶 🐶"
	if got != want {
		t.Errorf("emoji = %q, want %q\n%s", got, want, emojiImg.ReplaceAllString(html, "[IMG]"))
	}
	if !strings.Contains(html, `alt="alt 😄"`) {
		t.Errorf("alt text was converted:\n%s", emojiImg.ReplaceAllString(html, "[IMG]"))
	}
	if !strings.Contains(html, `>https://example.com/:smile:</a>`) {
		t.Error("an autolink's text was converted")
	}
	if !strings.Contains(html, ":smile:</code>") {
		t.Error("a shortcode in code was converted")
	}
}

func TestRenderEmojiEntityAndBreaks(t *testing.T) {
	html := emojiOf(t, "entity &#x1F604; and\nsecond :smile: line  \nthird\n", Options{})
	if got := strings.Join(emojiAlts(t, html), " "); got != "😄 😄" {
		t.Errorf("emoji = %q", got)
	}
	if strings.Count(html, "<br />") != 2 {
		t.Errorf("line breaks changed:\n%s", emojiImg.ReplaceAllString(html, "[IMG]"))
	}
}

func TestRenderEmojiHeaderFooterAndSlug(t *testing.T) {
	d := parseDeck(t, "<!-- header: H :tada: -->\n<!-- footer: 🚀 -->\n\n# A :heart_eyes: b\n", Options{NoTemplate: true})
	html := d.Render(RenderOptions{Frame: FramePlain})[0].HTML
	if got := strings.Join(emojiAlts(t, html), " "); got != "🎉 😍 🚀" {
		t.Errorf("emoji = %q", got)
	}
	if id := d.Slides[0].Headings[0].ID; id != "a-%F0%9F%98%8D-b" {
		t.Errorf("slug %q: Marp Core slugs the emoji a shortcode stands for", id)
	}
	if text := d.Slides[0].Headings[0].Text; text != "A 😍 b" {
		t.Errorf("outline text %q", text)
	}
}

func TestNoEmoji(t *testing.T) {
	src := "# T :tada:\n\nText :smile: 😄 `c 🚀`\n\n```\n🎉\n```\n\n    🐱\n"
	d := parseDeck(t, src, Options{NoTemplate: true, NoEmoji: true})
	html := d.Render(RenderOptions{Frame: FramePlain})[0].HTML
	if strings.Contains(html, "<img") {
		t.Errorf("images despite NoEmoji:\n%s", html)
	}
	for _, s := range []string{":smile: 😄", "c 🚀", "🎉", "🐱"} {
		if !strings.Contains(html, s) {
			t.Errorf("%q missing:\n%s", s, html)
		}
	}
	if id := d.Slides[0].Headings[0].ID; id != "t-tada" {
		t.Errorf("slug %q keeps the shortcode's name without emoji", id)
	}
}

func TestEmojiHighlightedCode(t *testing.T) {
	d := parseDeck(t, "```go\nx := \"🚀\"\n```\n", Options{NoTemplate: true})
	html := d.Render(RenderOptions{Frame: FramePlain, Highlight: func(code, lang string) (string, bool) {
		return `<span class="hljs-string">` + strings.TrimSpace(code) + `</span>`, true
	}})[0].HTML
	if got := emojiAlts(t, html); len(got) != 1 || got[0] != "🚀" {
		t.Errorf("alts %q in %s", got, emojiImg.ReplaceAllString(html, "[IMG]"))
	}
	if !strings.Contains(html, `<span class="hljs-string">x := "`) {
		t.Errorf("the highlighting tags were damaged:\n%s", emojiImg.ReplaceAllString(html, "[IMG]"))
	}
}

// The image carries the graphic once per distinct emoji, from a cache.
func TestEmojiSrcCached(t *testing.T) {
	a, b := emojiSrc("😄"), emojiSrc("😄️")
	if a == "" || a != b {
		t.Errorf("sources differ: %.40q %.40q", a, b)
	}
	if emojiSrc("not an emoji") != "" {
		t.Error("a source for no emoji")
	}
}
