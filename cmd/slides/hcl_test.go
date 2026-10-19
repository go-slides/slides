package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hclFixture writes a deck with slides.hcl (and a slides.yaml it must win
// over), an HCL kinds file and an HCL data file.
func hclFixture(t *testing.T) (dir, deckPath string) {
	dir = t.TempDir()
	write(t, dir, "slides.yaml", "lang: en\n")
	write(t, dir, "slides.hcl", `
lang   = "de"
themes = ["mine.css"]
kinds  = "kinds.hcl"
data = {
  who  = "config"
  year = 2026
}
kind "quiet" {
  class  = "quiet"
  footer = "${data.who} · ${page()}/${total()}"
}
`)
	write(t, dir, "kinds.hcl", "kind \"hero\" {\n  class = \"hero ${data.who}\"\n}\n")
	write(t, dir, "data.hcl", "who = upper(\"data\")\nnext = data.year + 1\n")
	write(t, dir, "mine.css", "/* @theme mine */ section { background: pink; }")
	deckPath = write(t, dir, "deck.md", "---hcl\ntheme = \"mine\"\nheader = \"${data.who} ${data.year}\"\n---\n<!-- _kind: hero -->\n\n# {{ .who }} {{ .year }}\n\n---\n\n<!-- _kind: quiet -->\nb\n")
	return dir, deckPath
}

func TestHCLConfig(t *testing.T) {
	dir, deckPath := hclFixture(t)
	out, stderr, err := run(t, "build", deckPath, "-o", "-")
	if err != nil {
		t.Fatal(err, stderr)
	}
	for _, want := range []string{"background: pink", `lang="de"`, `class="hero config"`, "<header>config 2026</header>", "<footer>config · 2/2</footer>", "config 2026</h1>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}

	// --data data.hcl: typed values, which see the data read before.
	out, _, err = run(t, "build", deckPath, "-o", "-", "--data", filepath.Join(dir, "data.hcl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `class="hero DATA"`) || !strings.Contains(out, "<header>DATA 2026</header>") {
		t.Error("--data data.hcl overrides the configuration's data")
	}
	write(t, dir, "deck2.md", "{{ add .next 1 }}\n")
	out, _, err = run(t, "build", filepath.Join(dir, "deck2.md"), "-o", "-", "--data", filepath.Join(dir, "data.hcl"))
	if err != nil || !strings.Contains(out, "2028") {
		t.Errorf("an HCL number is an int in templates: %v", err)
	}

	// expand writes a YAML front matter.
	out, _, err = run(t, "expand", deckPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "---\ntheme: \"mine\"\nheader: \"config 2026\"\n---\n") {
		t.Errorf("expand:\n%s", out)
	}

	// --kinds file.hcl.
	write(t, dir, "more.hcl", "kind \"extra\" {\n  paginate = true\n}\n")
	out, _, err = run(t, "kinds", deckPath, "--kinds", filepath.Join(dir, "more.hcl"))
	if err != nil || !strings.Contains(out, `extra      paginate="true"`) || !strings.Contains(out, `quiet      class="quiet"`) {
		t.Errorf("kinds:\n%s %v", out, err)
	}
}

func TestHCLConfigErrors(t *testing.T) {
	for _, tc := range []struct{ config, want string }{
		{"lang = 3\n", "slides.hcl:1:8: Invalid lang: lang wants a string, got number"},
		{"html = \"yes\"\n", "slides.hcl:1:8: Invalid html"},
		{"themes = \"a.css\"\n", "slides.hcl:1:10: Invalid themes"},
		{"colour = \"red\"\n", `slides.hcl:1:1: Unsupported attribute: "colour" is not a configuration key`},
		{"slide {\n}\n", "slides.hcl:1:1: Unsupported block type"},
		{"lang = \"de\"\nkind \"k\" {\n  class = data.nope\n}\n", "slides.hcl:3:15: Unsupported attribute"},
		{"lang = \n", "slides.hcl:1:"},
	} {
		dir := t.TempDir()
		write(t, dir, "slides.hcl", tc.config)
		deckPath := write(t, dir, "deck.md", "x\n")
		_, _, err := run(t, "build", deckPath, "-o", "-")
		if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, tc.want)) {
			t.Errorf("%q: error %v, want %q", tc.config, err, tc.want)
		}
	}
	// --config picks the format by extension.
	dir := t.TempDir()
	cfg := write(t, dir, "other.hcl", "lang = \"fr\"\n")
	deckPath := write(t, dir, "deck.md", "x\n")
	out, _, err := run(t, "build", deckPath, "-o", "-", "--config", cfg)
	if err != nil || !strings.Contains(out, `lang="fr"`) {
		t.Errorf("--config other.hcl: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "deck.html")); err == nil {
		t.Error("-o - writes no file")
	}
}
