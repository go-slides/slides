package deck

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/go-icons/twemoji"
	"golang.org/x/net/html"
)

// TestOracle renders every deck in testdata/oracle and compares it with
// what Marp Core produced for it (see testdata/oracle/gen.js), after
// normalizing what may legitimately differ: whitespace between tags,
// attribute order, and the random key of scoped styles.
func TestOracle(t *testing.T) {
	decks, _ := filepath.Glob("../testdata/oracle/*.md")
	if len(decks) == 0 {
		t.Fatal("no oracle decks")
	}
	for _, path := range decks {
		if strings.HasSuffix(path, ".src.md") {
			continue // a source for a generated deck, not a Marp deck
		}
		name := strings.TrimSuffix(filepath.Base(path), ".md")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".html")
			if err != nil {
				t.Fatal(err)
			}
			// Marp ignores an HCL front matter: what it renders is
			// what the body alone renders.
			if s := normalizer.Replace(string(src)); isHCLFrontMatter(s) {
				_, body, _, ok := splitFrontMatter(s)
				if !ok {
					t.Fatal("no front matter")
				}
				src = []byte(body)
			}
			// gen.js runs Marp Core without emoji, except on emoji.md.
			d, err := Parse(src, Options{NoTemplate: true, NoEmoji: name != "emoji"})
			if err != nil {
				t.Fatal(err)
			}
			var got strings.Builder
			for _, r := range d.Render(RenderOptions{Frame: FramePlain}) {
				got.WriteString(r.HTML)
			}
			g, w := normalizeHTML(got.String()), normalizeHTML(inlineTwemoji(t, string(want)))
			if g != w {
				gl, wl := strings.Split(g, "\n"), strings.Split(w, "\n")
				for i := range max(len(gl), len(wl)) {
					var a, b string
					if i < len(gl) {
						a = gl[i]
					}
					if i < len(wl) {
						b = wl[i]
					}
					if a != b {
						t.Fatalf("first difference at token %d:\n got: %s\nwant: %s", i, abbreviate(a), abbreviate(b))
					}
				}
			}

			var notes [][]string
			if b, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".notes.json"); err == nil {
				if err := json.Unmarshal(b, &notes); err != nil {
					t.Fatal(err)
				}
			}
			for i, s := range d.Slides {
				var wantNotes []string
				if i < len(notes) {
					wantNotes = notes[i]
				}
				if !slices.Equal(s.Notes, wantNotes) && !(len(s.Notes) == 0 && len(wantNotes) == 0) {
					t.Errorf("slide %d notes = %q, want %q", i+1, s.Notes, wantNotes)
				}
			}
		})
	}
}

var scopeKey = regexp.MustCompile(`data-marpit-scope-[A-Za-z0-9]+`)

var twemojiCDN = regexp.MustCompile(`https://cdn\.jsdelivr\.net/gh/jdecked/twemoji@[0-9.]+/assets/svg/([0-9a-f-]+)\.svg`)

var dataURI = regexp.MustCompile(`data:image/svg\+xml;base64,[A-Za-z0-9+/=]{24}[A-Za-z0-9+/=]*`)

// abbreviate shortens the data: URIs of a token for a failure message.
func abbreviate(s string) string {
	return dataURI.ReplaceAllStringFunc(s, func(u string) string { return u[:50] + "…" })
}

// inlineTwemoji turns Marp Core's emoji image URLs, which point at a CDN,
// into the data: URIs go-slides writes for the same graphics: the one
// difference that is meant.
func inlineTwemoji(t *testing.T, html string) string {
	return twemojiCDN.ReplaceAllStringFunc(html, func(url string) string {
		stem := twemojiCDN.FindStringSubmatch(url)[1]
		svg, ok := twemoji.SVGByStem(stem)
		if !ok {
			t.Errorf("Marp Core draws %s, which the embedded set lacks", stem)
		}
		return "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg))
	})
}

// normalizeHTML writes one token per line: tags with sorted attributes,
// text with its whitespace collapsed, comments dropped.
func normalizeHTML(s string) string {
	s = scopeKey.ReplaceAllString(s, "data-marpit-scope-KEY")
	z := html.NewTokenizer(strings.NewReader(s))
	var out []string
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return strings.Join(out, "\n")
		case html.TextToken:
			if t := strings.Join(strings.Fields(string(z.Text())), " "); t != "" {
				out = append(out, "TEXT "+t)
			}
		case html.StartTagToken, html.SelfClosingTagToken, html.EndTagToken:
			tok := z.Token()
			var attrs []string
			for _, a := range tok.Attr {
				attrs = append(attrs, a.Key+"="+a.Val)
			}
			slices.Sort(attrs)
			kind := "<"
			if tt == html.EndTagToken {
				kind = "</"
			}
			out = append(out, kind+tok.Data+" "+strings.Join(attrs, " "))
		}
	}
}

// TestOracleSVG compares Marp Core's inline SVG mode, where backgrounds
// become layers, on testdata/oracle/images.md.
func TestOracleSVG(t *testing.T) {
	src, err := os.ReadFile("../testdata/oracle/images.md")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../testdata/oracle/images.svg.html")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Parse(src, Options{NoTemplate: true, NoEmoji: true})
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	for _, r := range d.Render(RenderOptions{}) {
		got.WriteString(r.HTML)
	}
	g, w := strings.Split(normalizeHTML(got.String()), "\n"), strings.Split(normalizeHTML(string(want)), "\n")
	for i := range max(len(g), len(w)) {
		var a, b string
		if i < len(g) {
			a = g[i]
		}
		if i < len(w) {
			b = w[i]
		}
		if a != b {
			t.Fatalf("first difference at token %d:\n got: %s\nwant: %s", i, a, b)
		}
	}
}
