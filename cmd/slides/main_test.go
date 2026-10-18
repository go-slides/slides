package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCommands(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "slides.yaml", "kinds: kinds.yaml\ndata: {who: config}\nthemes: [mine.css]\nlang: de\n")
	write(t, dir, "kinds.yaml", "hero:\n  class: hero\n  footer: \"{{ .who }}\"\n")
	write(t, dir, "mine.css", "/* @theme mine */ section { background: pink; }")
	write(t, dir, "data.json", `{"who": "data"}`)
	deckPath := write(t, dir, "deck.md", "---\ntheme: mine\n---\n<!-- _kind: hero -->\n\n# {{ .who }}\n\n<!-- a note -->\n\n---\n\nb <!-- another -->\n")

	var out, errOut bytes.Buffer
	if err := run([]string{deckPath}, &out, &errOut); err != nil {
		t.Fatal(err, errOut.String())
	}
	page, err := os.ReadFile(filepath.Join(dir, "deck.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"background: pink", `lang="de"`, "<footer>config</footer>", `data-kind="hero"`} {
		if !strings.Contains(string(page), want) {
			t.Errorf("missing %q", want)
		}
	}
	if !strings.Contains(errOut.String(), "(2 slides)") {
		t.Errorf("stderr %q", errOut.String())
	}

	out.Reset()
	if err := run([]string{"build", "-o", "-", "-data", filepath.Join(dir, "data.json"), "-set", "extra=1", "-bare", "-html", deckPath}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "<footer>data</footer>") || strings.Contains(out.String(), "<script>") {
		t.Error("-data overrides the configuration; -bare drops the script")
	}

	out.Reset()
	if err := run([]string{"expand", deckPath}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `_class: "hero"`) || strings.Contains(out.String(), "kind") {
		t.Errorf("expand:\n%s", out.String())
	}

	out.Reset()
	if err := run([]string{"notes", deckPath}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if out.String() != "a note\n\n---\n\nanother\n" {
		t.Errorf("notes %q", out.String())
	}

	out.Reset()
	if err := run([]string{"kinds", deckPath}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `hero       class="hero" footer="config"`) {
		t.Errorf("kinds:\n%s", out.String())
	}

	out.Reset()
	if err := run([]string{"themes", "-theme-set", filepath.Join(dir, "mine.css")}, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "mine") || !strings.Contains(out.String(), "gaia") {
		t.Errorf("themes:\n%s", out.String())
	}

	err = run([]string{"build", "-no-config", "-o", "-", deckPath}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "deck.md:6:") || !strings.Contains(err.Error(), `"who"`) {
		t.Errorf("-no-config ignores slides.yaml, so .who is missing at line 6: %v", err)
	}
}

func TestUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	for _, args := range [][]string{{"help"}, {"version"}} {
		out.Reset()
		if err := run(args, &out, &errOut); err != nil || out.Len() == 0 {
			t.Errorf("%v: %v", args, err)
		}
	}
	for _, args := range [][]string{{}, {"build", "a", "b"}, {"-bogus"}, {"build", "-set", "noequals", "x.md"}} {
		if err := run(args, &out, &errOut); err == nil {
			t.Errorf("%v: want an error", args)
		}
	}
	if err := run([]string{"missing.md"}, &out, &errOut); err == nil {
		t.Error("a missing deck fails")
	}
}

func TestFlagsAfterTheDeck(t *testing.T) {
	dir := t.TempDir()
	deckPath := write(t, dir, "talk.md", "# x\n")
	out := filepath.Join(dir, "elsewhere.html")
	var o, e bytes.Buffer
	if err := run([]string{deckPath, "-o", out, "-lang", "fr"}, &o, &e); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil || !strings.Contains(string(b), `lang="fr"`) {
		t.Fatalf("flags after the deck were ignored: %v", err)
	}
	if err := run([]string{"expand", "--", "-o"}, &o, &e); err == nil || strings.Contains(err.Error(), "flag") {
		t.Errorf("after --, -o is a file name: %v", err)
	}
}
