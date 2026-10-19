package deck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-slides/slides/internal/hclx"
	"github.com/go-slides/slides/interp"
)

var hclClock = func() time.Time { return time.Date(2026, 10, 19, 9, 30, 0, 0, time.UTC) }

const hclDeck = `---hcl
# Directives keep their Marp names.
theme           = "gaia"
paginate        = true
class           = ["lead", "invert"]
backgroundColor = "#eee"
title           = "${data.company} by ${data.who}"
header          = format("%s %d", data.company, data.year)
footer          = "${upper(data.company)} — ${page()}/${total()}"
date            = formatdate("D MMMM YYYY", now())
data = {
  company = "Acme"
  year    = 2026
  ratio   = 1.5
  open    = true
  tags    = ["a", "b"]
}
kind "hero" {
  extends  = "title"
  class    = "hero ${lower(data.company)}"
  paginate = false
}
---
# {{ .data.company }} {{ add .data.year 1 }}

{{ printf "%T %T %T %T" .data.year .data.ratio .data.open .data.tags }} {{ join ", " .data.tags }} {{ .date }}

---

<!-- _kind: hero -->
b
`

func parseHCLDeck(t *testing.T, src string) *Deck {
	t.Helper()
	d, err := Parse([]byte(src), Options{Name: "deck.md", Data: map[string]any{"who": "Ada"}, Now: hclClock})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestHCLFrontMatter(t *testing.T) {
	d := parseHCLDeck(t, hclDeck)
	if got := d.Global.Theme(); got != "gaia" {
		t.Errorf("theme = %q", got)
	}
	if got := d.Global.Get("title"); got != "Acme by Ada" {
		t.Errorf("title = %q: data mixes the front matter's and the caller's", got)
	}
	s := d.Slides[0]
	for k, want := range map[string]string{
		"paginate":        "true",
		"class":           "lead invert",
		"backgroundColor": "#eee",
		"header":          "Acme 2026",
	} {
		if got := s.Dirs.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if got := interp.Resolve(s.Dirs.Get("footer"), s.Page, s.Total); got != "ACME — 1/2" {
		t.Errorf("footer = %q: page() and total() paginate like the template functions", got)
	}
	if !strings.Contains(string(d.source), "# Acme 2027") {
		t.Errorf("templates see typed data:\n%s", d.source)
	}
	if !strings.Contains(string(d.source), "int float64 bool []interface {} a, b 19 October 2026") {
		t.Errorf("data keeps its types, now() reads the clock:\n%s", d.source)
	}
	hero := d.Slides[1]
	if hero.Kind != "hero" || hero.Dirs.Get("class") != "hero acme" || hero.Dirs.Get("paginate") != "false" {
		t.Errorf("kind block: %q %q %q", hero.Kind, hero.Dirs.Get("class"), hero.Dirs.Get("paginate"))
	}
	if hero.Dirs.Get("footer") != "" {
		t.Errorf("hero extends title, which clears the footer: %q", hero.Dirs.Get("footer"))
	}
}

// TestHCLFrontMatterNotReexpanded checks that a value an HCL expression
// produced is final: Go templates do not run over it again.
func TestHCLFrontMatterNotReexpanded(t *testing.T) {
	d, err := Parse([]byte("---hcl\nfooter = data.raw\n---\nx\n"), Options{Data: map[string]any{"raw": "{{ .nope }}"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Slides[0].Dirs.Get("footer"); got != "{{ .nope }}" {
		t.Errorf("footer = %q", got)
	}
}

func TestHCLExpandRoundTrip(t *testing.T) {
	d := parseHCLDeck(t, hclDeck)
	md, err := d.Markdown()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(md, "---\n") || strings.Contains(md, "---hcl") {
		t.Fatalf("expand writes a YAML front matter:\n%s", md)
	}
	for _, want := range []string{`theme: "gaia"`, `footer: "ACME — 1/2"`, `class: ["lead", "invert"]`} {
		if !strings.Contains(md, want) {
			t.Errorf("expanded deck lacks %q:\n%s", want, md)
		}
	}
	for _, unwanted := range []string{"data:", "kind"} {
		if strings.Contains(md, unwanted) {
			t.Errorf("expanded deck still holds %q:\n%s", unwanted, md)
		}
	}
	plain, err := Parse([]byte(md), Options{NoTemplate: true})
	if err != nil {
		t.Fatal(err)
	}
	// Only the data-kind attribute, which names the kind, may differ.
	render := func(d *Deck) string {
		var b strings.Builder
		for _, r := range d.Render(RenderOptions{Frame: FramePlain}) {
			b.WriteString(dataKind.ReplaceAllString(r.HTML, ""))
		}
		return normalizeHTML(b.String())
	}
	if g, w := render(plain), render(d); g != w {
		t.Errorf("the expanded deck renders differently:\n got: %s\nwant: %s", g, w)
	}
}

func TestHCLErrors(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"syntax", "---hcl\ntheme = \"a\"\nfooter =\n---\nx\n", "deck.md:3:"},
		{"unknown data", "---hcl\ntheme = \"a\"\nfooter = data.missing\n---\nx\n", "deck.md:3:14: Unsupported attribute"},
		{"unknown function", "---hcl\nfooter = nope()\n---\nx\n", "deck.md:2:10: Call to unknown function"},
		{"attributes do not see each other", "---hcl\ntheme = \"a\"\nfooter = theme\n---\nx\n", "deck.md:3:10: Unknown variable"},
		{"data is an object", "---hcl\ndata = 3\n---\nx\n", "deck.md:2:8: Invalid data"},
		{"stray block", "---hcl\nslide {\n}\n---\nx\n", "deck.md:2:1: Unsupported block type"},
		{"kind label", "---hcl\nkind {\n}\n---\nx\n", "deck.md:2:1: Invalid kind block"},
		{"kind directive", "---hcl\nkind \"k\" {\n  theme = \"a\"\n}\n---\nx\n", `deck.md:3:3: Unsupported directive: kind "k": "theme" is not a local directive`},
		{"function error", "---hcl\nfooter = file(\"missing.txt\")\n---\nx\n", "deck.md:2:10: Error in function call"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.src), Options{Name: "deck.md", BaseDir: t.TempDir()})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %v, want %q", err, tc.want)
			}
		})
	}
}

func TestHCLOpener(t *testing.T) {
	// Only "---hcl" opens an HCL front matter; other text after the dashes
	// is a YAML front matter, as in Marp.
	d, err := Parse([]byte("---yaml\ntheme: gaia\n---\nx\n"), Options{})
	if err != nil || d.Global.Theme() != "gaia" {
		t.Errorf("---yaml: %v %q", err, d.Global.Theme())
	}
	d, err = Parse([]byte("---hcl  \r\ntheme = \"gaia\"\r\n---\r\nx\n"), Options{})
	if err != nil || d.Global.Theme() != "gaia" {
		t.Errorf("---hcl with trailing spaces and CRLF: %v", err)
	}
	// null leaves a directive unset.
	d, err = Parse([]byte("---hcl\nfooter = null\n---\nx\n"), Options{})
	if err != nil || d.Slides[0].Dirs.Has("footer") {
		t.Errorf("null: %v", err)
	}
}

func TestHCLKindsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kinds.hcl")
	src := `
kind "base" {
  class  = "base"
  footer = "${data.company} · ${page()}"
}
kind "chapter" {
  extends  = "base"
  paginate = true
  class    = ["chapter", "dark"]
}
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	k, err := LoadKindsFileEnv(path, HCLEnv{Data: map[string]any{"company": "Acme"}})
	if err != nil {
		t.Fatal(err)
	}
	dirs, err := k.Resolve("chapter")
	if err != nil {
		t.Fatal(err)
	}
	if dirs.Get("class") != "chapter dark" || dirs.Get("paginate") != "true" || dirs.Get("footer") != "Acme · "+interp.PageSentinel {
		t.Errorf("chapter: class %q paginate %q footer %q", dirs.Get("class"), dirs.Get("paginate"), dirs.Get("footer"))
	}

	// A deck's kinds key names it; the kinds see the deck's data.
	deck := "---\nkinds: kinds.hcl\ndata: {company: Initech}\n---\n<!-- kind: chapter -->\nx\n"
	d, err := Parse([]byte(deck), Options{BaseDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if got := interp.Resolve(d.Slides[0].Dirs.Get("footer"), 1, 1); got != "Initech · 1" {
		t.Errorf("footer = %q", got)
	}

	// Without data, the error names the file and position.
	if _, err := LoadKindsFile(path); err == nil || !strings.Contains(err.Error(), "kinds.hcl:4:") {
		t.Errorf("error %v", err)
	}
	// Top-level attributes are ignored: slides.hcl serves as a kinds file.
	k, err = ParseKindsHCL([]byte("themes = [\"x.css\"]\nkind \"a\" {\n  class = \"a\"\n}\n"), "slides.hcl", HCLEnv{})
	if err != nil || k["a"].Dirs.Get("class") != "a" {
		t.Errorf("%v", err)
	}
}

func TestHCLDataFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.hcl")
	if err := os.WriteFile(filepath.Join(dir, "motto.txt"), []byte("Ship it"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SLIDES_HCL_TEST", "from env")
	src := `
company = "Acme"
count   = 3
ratio   = 0.25
big     = 1e3
staff   = { lead = "Ada", size = length(["a", "b"]) }
motto   = trimspace(file("motto.txt"))
envvar  = env("SLIDES_HCL_TEST")
when    = timestamp()
prior   = data.before
`
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := LoadDataFile(path, HCLEnv{Data: map[string]any{"before": "x"}, Now: hclClock})
	if err != nil {
		t.Fatal(err)
	}
	staff, _ := m["staff"].(map[string]any)
	if m["company"] != "Acme" || m["count"] != 3 || m["ratio"] != 0.25 || m["big"] != 1000 ||
		staff["lead"] != "Ada" || staff["size"] != 2 || m["motto"] != "Ship it" ||
		m["envvar"] != "from env" || m["when"] != "2026-10-19T09:30:00Z" || m["prior"] != "x" {
		t.Errorf("data = %#v", m)
	}
	if err := os.WriteFile(path, []byte("slide {\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDataFile(path, HCLEnv{}); err == nil || !strings.Contains(err.Error(), "data.hcl:1:1: Unexpected block") {
		t.Errorf("error %v", err)
	}
}

func TestHCLFileFunctionBounds(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(big, make([]byte, hclx.MaxFileSize+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin"), []byte{0xff, 0xfe}, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ arg, want string }{
		{"big.txt", "larger than"},
		{".", "not a regular file"},
		{"bin", "not UTF-8"},
	} {
		src := "---hcl\nfooter = file(\"" + tc.arg + "\")\n---\nx\n"
		if _, err := Parse([]byte(src), Options{BaseDir: dir}); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("file(%q): %v, want %q", tc.arg, err, tc.want)
		}
	}
}

func TestHCLValueForms(t *testing.T) {
	// Directive values take the failsafe string forms the YAML path yields.
	src := "---hcl\nheadingDivider = [2, 3]\nsize = \"4:3\"\npaginate = \"hold\"\nheader = 3.5\nmath = \"katex\"\nmarp = true\n---\n# a\n## b\n"
	d, err := Parse([]byte(src), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if d.Global.Get("headingDivider") != "[2,3]" || d.Global.Get("size") != "4:3" || d.Global.Get("math") != "katex" || d.Global.Get("marp") != "true" {
		t.Errorf("globals %q %q %q %q", d.Global.Get("headingDivider"), d.Global.Get("size"), d.Global.Get("math"), d.Global.Get("marp"))
	}
	if len(d.Slides) != 2 || d.Slides[0].Dirs.Get("header") != "3.5" || d.Slides[0].Paginate() != "hold" {
		t.Errorf("%d slides, header %q", len(d.Slides), d.Slides[0].Dirs.Get("header"))
	}
	if n := d.Front["marp"]; n != true {
		t.Errorf("the decoded front matter keeps types like YAML's: %#v", n)
	}
}
