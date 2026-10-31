package editor

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-slides/slides/diag"
)

func TestHelpIsEnglishUnlessRegistered(t *testing.T) {
	src := "---\npaginate: true\n---\n\n# A\n"
	en := HoverAt(Request{Source: src}, 1, 2)
	if en == nil || !strings.Contains(en.Doc, "Shows the page number") {
		t.Fatalf("English hover: %+v", en)
	}
	// a language with no help, and an unknown language, read English
	for _, lang := range []string{"", "sv", "sv-SE"} {
		if h := HoverAt(Request{Source: src, Lang: lang}, 1, 2); h.Doc != en.Doc {
			t.Errorf("%q: %q", lang, h.Doc)
		}
	}
}

func TestRegisteredHelpIsUsedAndPartial(t *testing.T) {
	RegisterHelp("xx", map[string]string{"dir.paginate": "NUMÉROTE", "ui.scope.local": "directive locale", "func.upper": "MAJUSCULES"})
	src := "---\npaginate: true\ntheme: gaia\n---\n"
	h := HoverAt(Request{Source: src, Lang: "xx-YY"}, 1, 2)
	if h == nil || !strings.Contains(h.Doc, "NUMÉROTE") || !strings.Contains(h.Doc, "directive locale") {
		t.Fatalf("hover: %+v", h)
	}
	// a key the language lacks is English
	if h := HoverAt(Request{Source: src, Lang: "xx"}, 2, 2); !strings.Contains(h.Doc, "The theme the deck uses") {
		t.Errorf("partial catalogue: %q", h.Doc)
	}
	// the completion's Doc follows too
	var found bool
	for _, it := range Complete(Request{Source: "---\npag", Lang: "xx"}, 1, 3) {
		if it.Label == "paginate" {
			found = strings.Contains(it.Doc, "NUMÉROTE")
		}
	}
	if !found {
		t.Error("the completion of a directive is not in the language")
	}
	// Lang is not part of the English output
	if h := HoverAt(Request{Source: src}, 1, 2); strings.Contains(h.Doc, "NUMÉROTE") {
		t.Error("English leaked the other language")
	}
	if !contains(HelpLanguages(), "xx") {
		t.Error("HelpLanguages")
	}
}

// every language the package ships has the help of every directive and
// function, and no key the catalog does not have
func TestShippedHelpIsComplete(t *testing.T) {
	want := map[string]bool{"ui.scope.global": true, "ui.scope.local": true, "ui.scope.deck": true, "ui.scope.spot": true}
	for _, d := range Directives() {
		want["dir."+d.Name] = true
	}
	for _, f := range Funcs() {
		want["func."+f.Name] = true
	}
	langs := 0
	for _, l := range HelpLanguages() {
		if l == "xx" {
			continue
		}
		langs++
		have := help[l]
		for k := range want {
			if have[k] == "" {
				t.Errorf("%s: no help for %q", l, k)
			}
		}
		for k := range have {
			if !want[k] {
				t.Errorf("%s: %q is not a directive, function or scope", l, k)
			}
		}
	}
	if langs != 11 {
		t.Errorf("%d languages have help, want 11", langs)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func TestDiagnosticCodes(t *testing.T) {
	// the real failures of go-slides, through the real pipeline: each user-facing
	// error carries its code from where it is made, whatever its wording
	cases := []struct {
		name, src, code string
		files           map[string]string
	}{
		{"unknown kind", "---\nslides: true\n---\n\n# A\n\n---\n\n<!-- _kind: nope -->\n# B\n", "kind.unknown", nil},
		{"template function", "---\nslides: true\n---\n\n# A {{ nofunc 1 }}\n", "template.function", nil},
		{"template syntax", "---\nslides: true\n---\n\n# A {{ if }}\n", "template.syntax", nil},
		{"template exec", "---\nslides: true\n---\n\n# A {{ .nope }}\n", "template.exec", nil},
		{"front matter template", "---\nslides: true\ntitle: x\nfooter: \"{{ .nope }}\"\n---\n\n# A\n", "template.exec", nil},
		{"kinds yaml", "---\nslides: true\nkinds: k.yaml\n---\n\n# A\n", "yaml.syntax", map[string]string{path("k.yaml"): "a:\n  class: [\n"}},
		{"include missing", "---\nslides: true\n---\n\n{{ include \"gone.md\" }}\n", "file.missing", nil},
		{"hcl kinds", "---\nslides: true\nkinds: k.hcl\n---\n\n# A\n", "directive.unknown", map[string]string{path("k.hcl"): "kind \"a\" {\n  nosuchdirective = 1\n}\n"}},
		{"theme", "---\nslides: true\ntheme: nothere\n---\n\n# A\n", "theme.unknown", nil},
		{"hcl syntax", "---hcl\nslides = = true\n---\n\n# A\n", "hcl.syntax", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Render(req(c.src, c.files))
			if len(r.Diagnostics) == 0 {
				t.Fatal("no diagnostic")
			}
			found := false
			for _, d := range r.Diagnostics {
				if d.Code == "" {
					t.Errorf("a diagnostic with no code: %+v", d)
				}
				found = found || d.Code == c.code
			}
			if !found {
				t.Errorf("no %q among %+v", c.code, r.Diagnostics)
			}
		})
	}
	// every code a diagnostic can carry is documented, and the other way round
	used := map[string]bool{}
	for _, c := range cases {
		used[c.code] = true
	}
	for code, doc := range Codes {
		if code == "" || doc == "" {
			t.Errorf("code %q: %q", code, doc)
		}
	}
	for _, code := range []string{"hcl.syntax", "hcl.unsupported"} {
		if _, ok := Codes[code]; !ok {
			t.Errorf("%q is not in Codes", code)
		}
	}
	for code := range used {
		if _, ok := Codes[code]; !ok {
			t.Errorf("%q is not in Codes", code)
		}
	}
	// a leaf of a join keeps its own code
	e := errors.Join(diag.Errorf("yaml.syntax", "a.yaml: bad"), errors.New("deck.md:2: plain"))
	ds := Diagnose(e, "deck.md", "---\nx: 1\n---\n")
	if len(ds) != 2 || ds[0].Code != "yaml.syntax" || ds[1].Code != "" {
		t.Errorf("join: %+v", ds)
	}
}
