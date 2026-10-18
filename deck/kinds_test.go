package deck

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestKindsAgainstMarp is the end-to-end proof of kinds: the deck expanded
// to plain Marp Markdown, rendered by Marp Core (kinds-expanded.html, see
// gen.js), must equal this package's rendering of the original deck.
func TestKindsAgainstMarp(t *testing.T) {
	src, err := os.ReadFile("../testdata/oracle/kinds.src.md")
	if err != nil {
		t.Fatal(err)
	}
	d, err := Parse(src, Options{BaseDir: "../testdata/oracle"})
	if err != nil {
		t.Fatal(err)
	}
	md, err := d.Markdown()
	if err != nil {
		t.Fatal(err)
	}
	wantMD, _ := os.ReadFile("../testdata/oracle/kinds-expanded.md")
	if md != string(wantMD) {
		t.Errorf("expand output drifted from testdata/oracle/kinds-expanded.md:\n%s", md)
	}
	var got strings.Builder
	for _, r := range d.Render(RenderOptions{Frame: FramePlain}) {
		got.WriteString(r.HTML)
	}
	want, _ := os.ReadFile("../testdata/oracle/kinds-expanded.html")
	g := normalizeHTML(dataKind.ReplaceAllString(got.String(), ""))
	if w := normalizeHTML(string(want)); g != w {
		t.Fatalf("rendering differs from Marp's rendering of the expanded deck:\n got: %s\nwant: %s", g, w)
	}
	kinds := []string{"title", "content", "content", "quote", "quote", "section", "section", "content"}
	for i, s := range d.Slides {
		if s.Kind != kinds[i] {
			t.Errorf("slide %d kind = %q, want %q", i+1, s.Kind, kinds[i])
		}
	}
}

var dataKind = regexp.MustCompile(` data-kind="[^"]*"`)

func parseDeck(t *testing.T, src string, opts Options) *Deck {
	t.Helper()
	d, err := Parse([]byte(src), opts)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestSpotKindDoesNotStick(t *testing.T) {
	d := parseDeck(t, "---\nfooter: F\n---\n<!-- _kind: blank -->\n\na\n\n---\n\nb\n", Options{})
	if d.Slides[0].Dirs.Get("footer") != "" || d.Slides[0].Kind != "blank" {
		t.Errorf("slide 1: footer %q kind %q", d.Slides[0].Dirs.Get("footer"), d.Slides[0].Kind)
	}
	if d.Slides[1].Dirs.Get("footer") != "F" || d.Slides[1].Kind != "" {
		t.Errorf("slide 2: footer %q kind %q", d.Slides[1].Dirs.Get("footer"), d.Slides[1].Kind)
	}
}

func TestSpotKindResetsStickyKind(t *testing.T) {
	src := "---\nkinds:\n  dark: {class: invert, color: white}\n  plain: {color: black}\nkind: dark\n---\n<!-- _kind: plain -->\n\na\n\n---\n\nb\n"
	d := parseDeck(t, src, Options{})
	if c := d.Slides[0].Dirs.Get("class"); c != "" {
		t.Errorf("a spot kind must clear what the sticky kind set: class %q", c)
	}
	if c := d.Slides[0].Dirs.Get("color"); c != "black" {
		t.Errorf("color %q", c)
	}
	if c := d.Slides[1].Dirs.Get("class"); c != "invert" {
		t.Errorf("the sticky kind resumes after the spot: class %q", c)
	}
}

func TestExplicitDirectiveBeatsKindInSameComment(t *testing.T) {
	d := parseDeck(t, "<!--\nclass: mine\nkind: title\n-->\n\nx\n", Options{})
	if c := d.Slides[0].Dirs.Get("class"); c != "mine" {
		t.Errorf("class %q, want the hand-written one", c)
	}
}

func TestKindRestoresFrontMatterBase(t *testing.T) {
	d := parseDeck(t, "---\nkinds:\n  k: {footer: K}\nfooter: Base\n---\n<!-- kind: k -->\n\na\n\n---\n<!-- kind: content -->\n\nb\n", Options{})
	if f := d.Slides[1].Dirs.Get("footer"); f != "Base" {
		t.Errorf("footer %q, want the front matter's", f)
	}
}

func TestKindErrors(t *testing.T) {
	cases := map[string]string{
		"---\ntitle: x\n---\n\na\n\n<!-- kind: nope -->\n":                           `stdin:7: unknown kind "nope"`,
		"---\nkinds:\n  a: {extends: b}\n  b: {extends: a}\n---\n<!-- kind: a -->\n": "extends itself",
		"---\nkinds:\n  a: {bogus: 1}\n---\n":                                        `"bogus" is not a local directive`,
		"<!-- kind: [x] -->\n":                                                       "kind wants a kind name",
	}
	for src, want := range cases {
		_, err := Parse([]byte(src), Options{Name: "stdin"})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %v, want %q", src, err, want)
		}
	}
}

func TestKindsFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "k.yaml"), []byte("kinds:\n  hero:\n    class: hero\n    paginate: skip\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "k2.yaml"), []byte("hero2:\n  extends: hero\n  color: red\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := parseDeck(t, "---\nkinds: [k.yaml, k2.yaml]\n---\n<!-- kind: hero2 -->\n\na\n\n---\n\nb\n", Options{BaseDir: dir})
	s := d.Slides[0]
	if s.Dirs.Get("class") != "hero" || s.Dirs.Get("color") != "red" || s.Paginate() != "skip" {
		t.Errorf("dirs %v", s.Dirs.vals)
	}
	if d.Slides[1].Page != 1 {
		t.Errorf("skip must not count: page %d", d.Slides[1].Page)
	}
	if _, err := Parse([]byte("---\nkinds: missing.yaml\n---\n"), Options{BaseDir: dir}); err == nil {
		t.Error("a missing kinds file must fail")
	}
	k, err := LoadKindsFile(filepath.Join(dir, "k.yaml"))
	if err != nil || k["hero"] == nil {
		t.Fatalf("LoadKindsFile: %v %v", k, err)
	}
	// Options.Kinds sit under the deck's own kinds and are not mutated.
	opts := Options{Kinds: Kinds{"x": {Name: "x"}}}
	opts.Kinds["x"].Dirs.Set("footer", "{{ .who }}")
	d = parseDeck(t, "---\nwho: me\n---\n<!-- kind: x -->\n", opts)
	if f := d.Slides[0].Dirs.Get("footer"); f != "me" {
		t.Errorf("kind values are templates: footer %q", f)
	}
	if opts.Kinds["x"].Dirs.Get("footer") != "{{ .who }}" {
		t.Error("parsing must not modify the caller's kinds")
	}
}

func TestBuiltinKinds(t *testing.T) {
	k := BuiltinKinds()
	for _, n := range []string{"title", "section", "content", "blank"} {
		if _, err := k.Resolve(n); err != nil {
			t.Error(err)
		}
	}
	if got := strings.Join(k.Names(), ","); got != "blank,content,section,title" {
		t.Errorf("names %s", got)
	}
}

func TestTemplatesInDeck(t *testing.T) {
	src := "---\ndelims: [\"<%\", \"%>\"]\ntitle: T\n---\n# <% .title %> {{ .x }}\n\n<% page %>/<% total %>\n\n---\n\n<% page %>\n"
	d := parseDeck(t, src, Options{})
	rs := d.Render(RenderOptions{Frame: FramePlain})
	if !strings.Contains(rs[0].HTML, "T {{ .x }}") || !strings.Contains(rs[0].HTML, "<p>1/2</p>") || !strings.Contains(rs[1].HTML, "<p>2</p>") {
		t.Errorf("got %s %s", rs[0].HTML, rs[1].HTML)
	}
	d = parseDeck(t, "---\ntemplate: false\n---\n{{ .nope }}\n", Options{})
	if !strings.Contains(d.Render(RenderOptions{Frame: FramePlain})[0].HTML, "{{ .nope }}") {
		t.Error("template: false must leave the deck alone")
	}
	if _, err := Parse([]byte("a\n\n{{ .nope }}\n"), Options{Name: "d.md"}); err == nil || !strings.Contains(err.Error(), "d.md:") {
		t.Errorf("template errors name the deck: %v", err)
	}
	d = parseDeck(t, "---\nx: fm\ndata: {x: data}\n---\n{{ .x }}\n", Options{Data: map[string]any{"y": "opt"}})
	if !strings.Contains(d.Render(RenderOptions{Frame: FramePlain})[0].HTML, "data") {
		t.Error("the data mapping overrides front matter keys")
	}
}
