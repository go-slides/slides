package theme

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/go-slides/slides/internal/css"
)

// TestPackAgainstMarpit packs testdata/oracle/theme-case.css with the
// styles of theme-case.src.md and compares the result, rule by rule, with
// what Marpit produced (theme-case.packed.css, see gen.js).
func TestPackAgainstMarpit(t *testing.T) {
	src, err := os.ReadFile("../testdata/oracle/theme-case.css")
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../testdata/oracle/theme-case.packed.css")
	if err != nil {
		t.Fatal(err)
	}
	s := &Set{}
	th, err := s.Add(string(src))
	if err != nil {
		t.Fatal(err)
	}
	s.Default = th
	p := s.Pack(th, PackOptions{
		Printable: true,
		After: []string{
			"section { color: navy; }\n",
			"p { margin: 0; }",
			Scope("h1 { color: green; }\nsection { background: url(\"x.png\"); }", "data-marpit-scope-s1"),
		},
	})
	got, exp := normalizeCSS(p.CSS), normalizeCSS(string(want))
	gl, wl := strings.Split(got, "\n"), strings.Split(exp, "\n")
	for i := range max(len(gl), len(wl)) {
		var a, b string
		if i < len(gl) {
			a = gl[i]
		}
		if i < len(wl) {
			b = wl[i]
		}
		if a != b {
			t.Fatalf("rule %d differs:\n got: %s\nwant: %s", i, a, b)
		}
	}
	if p.Width != 960 || p.Height != 540 {
		t.Errorf("size %vx%v", p.Width, p.Height)
	}
}

var (
	scopeKeyRe = regexp.MustCompile(`data-marpit-scope-[A-Za-z0-9]+`)
	spaceRe    = regexp.MustCompile(`\s+`)
	combRe     = regexp.MustCompile(`\s*([>+~,])\s*`)
)

// normalizeCSS writes one rule per line with its at-rule context, its
// selectors and declarations, whitespace collapsed and comments dropped.
func normalizeCSS(s string) string {
	s = scopeKeyRe.ReplaceAllString(s, "data-marpit-scope-KEY")
	var lines []string
	var walk func(nodes []css.Node, ctx string)
	walk = func(nodes []css.Node, ctx string) {
		for _, n := range nodes {
			switch x := n.(type) {
			case *css.AtRule:
				head := ctx + "@" + x.Name + " " + spaceRe.ReplaceAllString(x.Params, " ")
				if !x.Block {
					lines = append(lines, head+";")
					continue
				}
				if strings.HasSuffix(x.Name, "keyframes") || x.Name == "page" {
					lines = append(lines, head+" {"+declText(x.Nodes)+"}")
					continue
				}
				walk(x.Nodes, head+" > ")
			case *css.Rule:
				sel := combRe.ReplaceAllString(spaceRe.ReplaceAllString(x.Selector, " "), "$1")
				lines = append(lines, ctx+sel+" {"+declText(x.Nodes)+"}")
			}
		}
	}
	walk(css.Parse(s).Nodes, "")
	return strings.Join(lines, "\n")
}

func declText(nodes []css.Node) string {
	var b strings.Builder
	for _, n := range nodes {
		switch x := n.(type) {
		case *css.Decl:
			b.WriteString(x.Prop + ":" + spaceRe.ReplaceAllString(x.Value, " "))
			if x.Important {
				b.WriteString("!important")
			}
			b.WriteString(";")
		case *css.Rule:
			b.WriteString(spaceRe.ReplaceAllString(x.Selector, " ") + "{" + declText(x.Nodes) + "}")
		}
	}
	return b.String()
}

func TestBuiltinThemes(t *testing.T) {
	s := NewSet()
	for _, n := range []string{"default", "gaia", "uncover"} {
		th := s.Get(n)
		if th == nil {
			t.Fatalf("missing %s", n)
		}
		if w, h := s.Size(th); w != "1280px" || h != "720px" {
			t.Errorf("%s size %s %s", n, w, h)
		}
		if w, h, ok := s.SizePreset(th, "4:3"); !ok || w != "960px" || h != "720px" {
			t.Errorf("%s 4:3 preset %s %s %v", n, w, h, ok)
		}
		p := s.Pack(th, PackOptions{Size: "4:3", Slide: []string{"svg", "foreignObject", "section"}, InlineSVG: true, ContainerQuery: true})
		if p.Width != 960 || p.Height != 720 {
			t.Errorf("%s packed 4:3 as %vx%v", n, p.Width, p.Height)
		}
		if !strings.Contains(p.CSS, "div.marpit > svg > foreignObject > :where(section)") {
			t.Errorf("%s: container query rule missing", n)
		}
		if strings.Contains(p.CSS, ":marpit-") {
			t.Errorf("%s: a Marpit pseudo-class leaked into the output", n)
		}
	}
	if s.Resolve("nope") != s.Default || s.Has("nope") {
		t.Error("an unknown theme falls back to the default")
	}
	if got := strings.Join(s.Names(), ","); got != "default,gaia,uncover" {
		t.Errorf("names %s", got)
	}
}

func TestParseErrorsAndImports(t *testing.T) {
	if _, err := Parse("section { color: red; }"); err == nil {
		t.Error("a theme without @theme must be refused")
	}
	s := &Set{}
	if _, err := s.Add("/* @theme base\n * @size wide 1600px 900px */ section { width: 1000px; height: 500px; } h1 { color: red; }"); err != nil {
		t.Fatal(err)
	}
	child, err := s.Add(`/* @theme child */ @import "base"; h2 { color: blue; }`)
	if err != nil {
		t.Fatal(err)
	}
	p := s.Pack(child, PackOptions{})
	if !strings.Contains(p.CSS, "div.marpit > section h1") || !strings.Contains(p.CSS, "div.marpit > section h2") {
		t.Errorf("import not inlined:\n%s", p.CSS)
	}
	if p.Width != 1000 {
		t.Errorf("size comes through the import: %v", p.Width)
	}
	if _, _, ok := s.SizePreset(child, "wide"); !ok {
		t.Error("size presets come through the import")
	}
	// A deck's style cannot import a theme.
	p = s.Pack(child, PackOptions{After: []string{`@import "base"; p { x: y; }`}})
	if strings.Count(p.CSS, "section h1") != 1 {
		t.Errorf("a theme import in deck CSS must be suppressed:\n%s", p.CSS)
	}
}

func TestGeneratedContentLowering(t *testing.T) {
	s := NewSet()
	p := s.Pack(s.Get("gaia"), PackOptions{Slide: []string{"div.marpit-svg", "div.marpit-fo", "section"}, GeneratedContent: true})
	if strings.Contains(p.CSS, "div.marpit-fo > section::after") || strings.Contains(p.CSS, "div.marpit-fo > section:after") {
		t.Error("the slide's ::after rules must be lowered")
	}
	if !strings.Contains(p.CSS, `section > [data-slides-pseudo="after"]`) {
		t.Error("lowered rules select the pseudo element")
	}
	if p.After != "attr(data-marpit-pagination)" {
		t.Errorf("content expression %q", p.After)
	}
}

func TestScopeKeyframes(t *testing.T) {
	out := Scope("@keyframes k { from { a: b; } }\np { animation: k 1s; }\n@media print { section.x { c: d; } }", "data-marpit-scope-s3")
	for _, want := range []string{"@keyframes k-s3", "animation: k-s3 1s", "section[data-marpit-scope-s3].x"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}
