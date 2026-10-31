package editor

import (
	"errors"
	"strings"
	"testing"
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
	cases := map[string]string{
		`unknown kind "x"`:                              "kind.unknown",
		`function "foo" not defined`:                    "template.function",
		`template: deck.md:3:5: executing "deck.md" at`: "template.exec",
		`yaml: line 4: mapping values are not allowed`:  "yaml.syntax",
		`Unsupported directive: x`:                      "hcl.unsupported_placeholder",
		`something unheard of`:                          "",
	}
	for msg, want := range cases {
		got := CodeOf(msg)
		if want == "hcl.unsupported_placeholder" {
			want = "directive.unknown" // a go-slides directive the file does not know
		}
		if got != want {
			t.Errorf("%q: %q, want %q", msg, got, want)
		}
	}
	for code := range Codes {
		if code == "" {
			t.Error("empty code")
		}
	}
	for _, r := range codeRules {
		if _, ok := Codes[r.code]; !ok {
			t.Errorf("rule %q has no entry in Codes", r.code)
		}
	}
	ds := Diagnose(errors.New(`deck.md:2: unknown kind "x"`), "deck.md", "---\n_kind: x\n---\n")
	if len(ds) != 1 || ds[0].Code != "kind.unknown" || !strings.Contains(ds[0].Message, "unknown kind") {
		t.Errorf("diagnose: %+v", ds)
	}
}
