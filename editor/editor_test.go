package editor

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/interp"
)

var dir = filepath.FromSlash("/nonexistent-go-slides-editor")

func path(parts ...string) string { return filepath.Join(append([]string{dir}, parts...)...) }

func req(src string, files map[string]string) Request {
	return Request{Path: path("deck.md"), Source: src, Files: files}
}

func TestRender(t *testing.T) {
	r := Render(req("---\nslides: true\npaginate: true\n---\n\n# One\n\n---\n\n# Two {{ page }}/{{ total }}\n", nil))
	if len(r.Diagnostics) != 0 {
		t.Fatalf("diagnostics %+v", r.Diagnostics)
	}
	if r.Slides != 2 || strings.Count(r.HTML, "<svg data-marpit-svg") != 2 {
		t.Fatalf("slides %d, html %.300s", r.Slides, r.HTML)
	}
	for _, want := range []string{`<style id="go-slides-style">`, `id="go-slides-preview"`, "Two 2/2", PreviewCSS} {
		if !strings.Contains(r.HTML, want) {
			t.Errorf("preview lacks %q", want)
		}
	}
	// the parts are the whole, slide by slide
	if len(r.SlideHTML) != 2 || r.Head+strings.Join(r.SlideHTML, "")+"</div>" != r.HTML ||
		!strings.HasPrefix(r.SlideHTML[1], "<svg data-marpit-svg") || !strings.Contains(r.SlideHTML[1], "Two 2/2") ||
		!strings.HasSuffix(r.Head, `data-theme="default">`) {
		t.Fatalf("parts: head %.80q…, %d slides", r.Head, len(r.SlideHTML))
	}
	// an error has no parts
	if e := Render(req("---\nslides: true\n---\n{{ nope }}", nil)); e.HTML != "" || e.Head != "" || e.SlideHTML != nil {
		t.Fatal("parts of a failed render")
	}
}

// line returns the 0-based line of the first line of src containing s.
func line(t *testing.T, src, s string) int {
	t.Helper()
	for i, l := range strings.Split(src, "\n") {
		if strings.Contains(l, s) {
			return i
		}
	}
	t.Fatalf("%q not in source", s)
	return -1
}

func one(t *testing.T, r Result) Diagnostic {
	t.Helper()
	if len(r.Diagnostics) == 0 {
		t.Fatal("no diagnostic")
	}
	return r.Diagnostics[0]
}

func TestDiagnosticLines(t *testing.T) {
	cases := []struct {
		name, src, at string
		files         map[string]string
	}{
		{name: "template after a code block",
			src: "---\nslides: true\n---\n\n```go\nfunc main() {\n}\n```\n\n# {{ .missing }}\n", at: "{{ .missing }}"},
		{name: "template parse error",
			src: "---\nslides: true\n---\n\nok\n\n# {{ if }}\n", at: "{{ if }}"},
		{name: "front matter value",
			src: "---\nslides: true\ntitle: x\nfooter: \"{{ .nope }}\"\n---\n\n# A\n", at: "footer:"},
		{name: "unknown kind",
			src: "---\nslides: true\n---\n\n# A\n\n---\n\n<!-- _kind: nope -->\n# B\n", at: "_kind: nope"},
		{name: "hcl front matter",
			src: "---hcl\nslides = true\ntheme = nope(1)\n---\n\n# A\n", at: "theme = nope"},
		{name: "kinds key",
			src: "---\nslides: true\nkinds: k.yaml\n---\n\n# A\n", at: "kinds: k.yaml",
			files: map[string]string{path("k.yaml"): "a:\n  class: [\n"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Render(req(c.src, c.files))
			if r.HTML != "" {
				t.Fatal("an error left a preview")
			}
			var onDeck []Diagnostic
			for _, d := range r.Diagnostics {
				if d.File == path("deck.md") {
					onDeck = append(onDeck, d)
				}
			}
			if len(onDeck) == 0 {
				t.Fatalf("nothing on the deck: %+v", r.Diagnostics)
			}
			d := onDeck[0]
			if want := line(t, c.src, c.at); d.Line != want || d.Severity != "error" {
				t.Fatalf("line %d (%s), want %d: %+v", d.Line, strings.Split(c.src, "\n")[d.Line], want, d)
			}
		})
	}
}

func TestDiagnosticInAnotherFile(t *testing.T) {
	src := "---\nslides: true\nkinds: k.yaml\n---\n\n# A\n"
	r := Render(req(src, map[string]string{path("k.yaml"): "a:\n  class: x\n b: [\n"}))
	i := slices.IndexFunc(r.Diagnostics, func(d Diagnostic) bool { return d.File == path("k.yaml") })
	if i < 0 {
		t.Fatalf("nothing on k.yaml: %+v", r.Diagnostics)
	}
	if d := r.Diagnostics[i]; d.Line < 1 {
		t.Fatalf("k.yaml diagnostic on line %d: %+v", d.Line, d)
	}
}

func TestMissingFiles(t *testing.T) {
	src := "---\nslides: true\n---\n\n{{ include \"parts/a.md\" }}\n"
	r := Render(req(src, map[string]string{path("slides.yaml"): "kinds: k.yaml\n"}))
	if !slices.Contains(r.Missing, path("k.yaml")) {
		t.Fatalf("missing %v", r.Missing)
	}
	// Hand the kinds file over: the build gets further and asks for the
	// include.
	r = Render(req(src, map[string]string{path("slides.yaml"): "kinds: k.yaml\n", path("k.yaml"): "a: {class: x}\n"}))
	if !slices.Contains(r.Missing, path("parts", "a.md")) {
		t.Fatalf("missing %v", r.Missing)
	}
	d := one(t, r)
	if d.Line != 4 {
		t.Fatalf("include error on line %d", d.Line)
	}
	r = Render(req(src, map[string]string{path("parts", "a.md"): "# Included"}))
	if len(r.Missing) != 0 && !slices.Equal(r.Missing, []string{path("slides.hcl"), path("slides.yaml"), path("slides.yml")}) {
		t.Fatalf("missing %v", r.Missing)
	}
	if !strings.Contains(r.HTML, "Included") {
		t.Fatal("include not rendered")
	}
}

// TestThemeDirectoryListing is the loop for a theme directory: the build
// asks for the directory, the editor lists it, the build asks for the CSS.
func TestThemeDirectoryListing(t *testing.T) {
	src := "---\nslides: true\ntheme: corp\n---\n\n# A\n"
	files := map[string]string{path("slides.yaml"): "themes: [themes]\n"}
	r := Render(req(src, files))
	if !slices.Contains(r.Missing, path("themes")) {
		t.Fatalf("missing %v", r.Missing)
	}
	rq := req(src, files)
	rq.Dirs = map[string][]string{path("themes"): {"corp.css", "README", "old/"}}
	r = Render(rq)
	if !slices.Contains(r.Missing, path("themes", "corp.css")) || slices.Contains(r.Missing, path("themes", "README")) {
		t.Fatalf("missing %v", r.Missing)
	}
	rq.Files[path("themes", "corp.css")] = "/* @theme corp */\nsection{color:#123456}\n"
	r = Render(rq)
	if len(r.Diagnostics) != 0 || !strings.Contains(r.HTML, `data-theme="corp"`) || !strings.Contains(r.HTML, "#123456") {
		t.Fatalf("diagnostics %+v missing %v", r.Diagnostics, r.Missing)
	}
}

// TestHTMLFollowsTheRequest: raw HTML outside Marp's allowlist stays out
// unless the editor asks, whatever the workspace's slides.yaml says.
func TestHTMLFollowsTheRequest(t *testing.T) {
	src := "---\nslides: true\n---\n\n<script>alert(1)</script>\n\n<b>bold</b>\n"
	files := map[string]string{path("slides.yaml"): "html: true\n"}
	r := Render(req(src, files))
	if strings.Contains(r.HTML, "<script>alert") {
		t.Fatal("a workspace file enabled raw HTML")
	}
	if !strings.Contains(r.HTML, "<b>bold</b>") {
		t.Fatal("the allowlist dropped <b>")
	}
	rq := req(src, files)
	rq.HTML = true
	if r := Render(rq); !strings.Contains(r.HTML, "<script>alert") {
		t.Fatal("HTML: true did not allow raw HTML")
	}
}

func TestStyleCannotCloseThePreviewStyle(t *testing.T) {
	src := "---\nslides: true\nstyle: \"section{color:red}</style><img src=x onerror=alert(1)>\"\n---\n\n# A\n"
	r := Render(req(src, nil))
	if strings.Contains(r.HTML, "</style><img") {
		t.Fatal("a style directive closed the style element")
	}
}

func TestWarningOnThemeLine(t *testing.T) {
	src := "---\nslides: true\ntheme: nope\n---\n\n# A\n"
	r := Render(req(src, nil))
	d := one(t, r)
	if d.Severity != "warning" || d.Line != 2 {
		t.Fatalf("%+v", d)
	}
}

func TestDetect(t *testing.T) {
	cases := []struct {
		src        string
		marp, want bool
	}{
		{"---\nslides: true\n---\n", false, true},
		{"---\nslides: true # a deck\n---\n", false, true},
		{"---\nmarp: true\n---\n", false, false},
		{"---\nmarp: true\n---\n", true, true},
		{"---\nslides: false\n---\n", true, false},
		{"---\ndata:\n  slides: true\n---\n", false, false},
		{"---hcl\nslides = true\n---\n", false, true},
		{"# no front matter\nslides: true\n", false, false},
		{"---\ntitle: x\n---\nslides: true\n", false, false},
		{"--\nslides: true\n--\n", false, false},
		{"---\r\nslides: true\r\n---\r\n", false, true},
	}
	for _, c := range cases {
		if got := Detect(c.src, c.marp); got != c.want {
			t.Errorf("Detect(%q, %v) = %v", c.src, c.marp, got)
		}
	}
}

func labels(items []Item) []string {
	var out []string
	for _, i := range items {
		out = append(out, i.Label)
	}
	return out
}

func TestComplete(t *testing.T) {
	src := "---\nslides: true\ntheme: \nkinds:\n  intro: {class: lead}\ndata: {who: me}\nkind: intro\n\n---\n\n<!-- _ -->\n\n# {{ up }} {{ .w }}\n"
	r := req(src, nil)
	at := func(s string, off int) (int, int) {
		l := line(t, src, s)
		return l, strings.Index(strings.Split(src, "\n")[l], s) + off
	}

	l, c := at("theme: ", 7)
	if got := labels(Complete(r, l, c)); !slices.Contains(got, "gaia") || !slices.Contains(got, "default") {
		t.Errorf("theme values %v", got)
	}
	l, c = at("kind: ", 6)
	got := labels(Complete(r, l, c))
	if !slices.Contains(got, "intro") || !slices.Contains(got, "title") {
		t.Errorf("kind values %v", got)
	}
	// Keys at the start of an empty front-matter line.
	keys := labels(Complete(r, line(t, src, "kind: ")+1, 0))
	for _, k := range []string{"theme", "paginate", "kind", "kinds", "slides"} {
		if !slices.Contains(keys, k) {
			t.Errorf("front-matter keys lack %s: %v", k, keys)
		}
	}
	// A spot directive in a comment: local ones only.
	l, c = at("<!-- _", 6)
	spot := labels(Complete(r, l, c))
	if !slices.Contains(spot, "kind") || !slices.Contains(spot, "footer") || slices.Contains(spot, "theme") {
		t.Errorf("spot keys %v", spot)
	}
	l, c = at("{{ up", 5)
	if got := labels(Complete(r, l, c)); !slices.Contains(got, "upper") || !slices.Contains(got, "include") {
		t.Errorf("functions %v", got)
	}
	l, c = at("{{ .w", 5)
	if got := labels(Complete(r, l, c)); !slices.Contains(got, "who") {
		t.Errorf("data %v", got)
	}
	// Plain Markdown: nothing.
	if got := Complete(r, line(t, src, "# {{"), 0); got != nil {
		t.Errorf("Markdown completions %v", labels(got))
	}
	// Out of range.
	if Complete(r, 999, 0) != nil || HoverAt(r, -1, 0) != nil {
		t.Error("out of range answered")
	}
}

func TestCompleteValues(t *testing.T) {
	src := "---\nslides: true\npaginate: \nsize: \nheadingDivider: \nmath: \n---\n"
	r := req(src, nil)
	for key, want := range map[string]string{"paginate: ": "hold", "size: ": "4:3", "headingDivider: ": "3", "math: ": "katex"} {
		l := line(t, src, key)
		if got := labels(Complete(r, l, len(key))); !slices.Contains(got, want) {
			t.Errorf("%s values %v lack %s", key, got, want)
		}
	}
	hcl := "---hcl\nslides = true\n\n---\n"
	items := Complete(req(hcl, nil), 2, 0)
	if len(items) == 0 || items[0].Insert != items[0].Label+" = " {
		t.Errorf("HCL keys %+v", items)
	}
}

func TestHover(t *testing.T) {
	src := "---\nslides: true\npaginate: true\nkinds:\n  intro: {class: lead, extends: title}\n---\n\n<!-- _kind: intro -->\n# {{ upper \"x\" }}\n\nplain\n"
	r := req(src, nil)
	h := HoverAt(r, line(t, src, "paginate"), 2)
	if h == nil || !strings.Contains(h.Doc, "page number") || h.Column != 0 || h.EndColumn != len("paginate") {
		t.Fatalf("paginate hover %+v", h)
	}
	l := line(t, src, "_kind: intro")
	if h := HoverAt(r, l, strings.Index(src[strings.Index(src, "<!--"):], "_kind")+2); h == nil || !strings.Contains(h.Doc, "spot") {
		t.Fatalf("_kind hover %+v", h)
	}
	if h := HoverAt(r, l, strings.Index(strings.Split(src, "\n")[l], "intro")+1); h == nil || !strings.Contains(h.Doc, "extends title") || !strings.Contains(h.Doc, "`class`") {
		t.Fatalf("kind value hover %+v", h)
	}
	l = line(t, src, "{{ upper")
	if h := HoverAt(r, l, 5); h == nil || !strings.Contains(h.Doc, "upper S") {
		t.Fatalf("function hover %+v", h)
	}
	if h := HoverAt(r, line(t, src, "plain"), 2); h != nil {
		t.Fatalf("Markdown hover %+v", h)
	}
}

// TestCatalogCoversInterp is the control on the catalog: a template
// function added to interp without documentation here fails.
func TestCatalogCoversInterp(t *testing.T) {
	for name := range interp.Funcs(interp.Options{}) {
		if _, ok := funcDoc(name); !ok {
			t.Errorf("template function %s is not documented", name)
		}
	}
	for _, d := range Directives() {
		if d.Doc == "" {
			t.Errorf("directive %s has no documentation", d.Name)
		}
	}
	if len(Funcs()) != len(funcDocs) {
		t.Fatal("Funcs")
	}
	all := append(deck.GlobalDirectives(), deck.LocalDirectives()...)
	if len(Directives()) != len(all)+6 {
		t.Fatalf("%d directives", len(Directives()))
	}
}

func TestKindList(t *testing.T) {
	k := deck.BuiltinKinds()
	extra, err := deck.ParseKindsYAML([]byte("hero:\n  extends: title\n  footer: x\nloop:\n  extends: loop\n"), "k.yaml")
	if err != nil {
		t.Fatal(err)
	}
	list, err := KindList(k.Merge(deck.Kinds{"hero": extra["hero"]}))
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(list, func(i KindInfo) bool { return i.Name == "hero" })
	if i < 0 || list[i].Extends != "title" || list[i].Directives[0] != (KV{"class", "lead"}) {
		t.Fatalf("%+v", list)
	}
	if _, err := KindList(extra); err == nil {
		t.Fatal("a kind extending itself listed")
	}
}

func TestDiagnoseShapes(t *testing.T) {
	src := "line0\nkinds: shared/k.hcl\nline2\n"
	p := path("deck.md")
	ds := Diagnose(errors.New(p+":1: "+path("shared", "k.hcl")+":3:9: Unsupported directive: x"), p, src)
	if len(ds) != 2 || ds[0].File != path("shared", "k.hcl") || ds[0].Line != 2 || ds[1].Line != 1 {
		t.Fatalf("%+v", ds)
	}
	ds = Diagnose(errors.Join(errors.New(p+":3:1: a"), errors.New(p+":1:1: b")), p, src)
	if len(ds) != 2 || ds[0].Line != 2 || ds[1].Line != 0 {
		t.Fatalf("%+v", ds)
	}
	ds = Diagnose(errors.New("open "+path("shared", "k.hcl")+": file does not exist"), p, src)
	if len(ds) != 1 || ds[0].Line != 1 {
		t.Fatalf("unpositioned error not located: %+v", ds)
	}
	if Diagnose(nil, p, src) != nil {
		t.Fatal("nil error diagnosed")
	}
	// Columns count UTF-16 units: "é" is one, "😀" two.
	d := at(p, []string{"é😀x"}, 0, 1+len("é😀"), "error", "m")
	if d.Column != 3 || d.EndColumn != 4 {
		t.Fatalf("columns %+v", d)
	}
}
