package main

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"rsc.io/pdf"
)

// run drives RootCmd with args, capturing standard output and error.
func run(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	c := RootCmd()
	var o, e bytes.Buffer
	c.SetArgs(args)
	c.SetOut(&o)
	c.SetErr(&e)
	err = c.Execute()
	return o.String(), e.String(), err
}

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// fixture writes a deck with a configuration file, a kinds file, a theme
// and a data file next to it.
func fixture(t *testing.T) (dir, deckPath string) {
	dir = t.TempDir()
	write(t, dir, "slides.yaml", "kinds: kinds.yaml\ndata: {who: config}\nthemes: [mine.css]\nlang: de\n")
	write(t, dir, "kinds.yaml", "hero:\n  class: hero\n  footer: \"{{ .who }}\"\n")
	write(t, dir, "mine.css", "/* @theme mine */ section { background: pink; }")
	write(t, dir, "data.json", `{"who": "data"}`)
	deckPath = write(t, dir, "deck.md", "---\ntheme: mine\n---\n<!-- _kind: hero -->\n\n# {{ .who }}\n\n<!-- a note -->\n\n---\n\nb <!-- another -->\n")
	return dir, deckPath
}

func TestBuildIsTheDefault(t *testing.T) {
	dir, deckPath := fixture(t)
	_, stderr, err := run(t, deckPath)
	if err != nil {
		t.Fatal(err, stderr)
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
	if !strings.Contains(stderr, "(2 slides)") {
		t.Errorf("stderr %q", stderr)
	}
}

func TestBuildFlags(t *testing.T) {
	dir, deckPath := fixture(t)
	// Flags before or after the deck, long or short.
	out, _, err := run(t, "build", deckPath, "-o", "-", "--data", filepath.Join(dir, "data.json"), "--set", "extra=1", "--bare", "--html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "<footer>data</footer>") || strings.Contains(out, "<script>") {
		t.Error("--data overrides the configuration; --bare drops the script")
	}
	other := filepath.Join(dir, "elsewhere.html")
	if _, _, err := run(t, deckPath, "--output", other, "--lang", "fr"); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(other); err != nil || !strings.Contains(string(b), `lang="fr"`) {
		t.Errorf("--lang overrides the configuration's lang: %v", err)
	}
}

func TestExpandNotesKinds(t *testing.T) {
	_, deckPath := fixture(t)
	out, _, err := run(t, "expand", deckPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `_class: "hero"`) || strings.Contains(out, "kind") {
		t.Errorf("expand:\n%s", out)
	}
	out, _, err = run(t, "notes", deckPath)
	if err != nil || out != "a note\n\n---\n\nanother\n" {
		t.Errorf("notes %q, %v", out, err)
	}
	out, _, err = run(t, "kinds", deckPath)
	if err != nil || !strings.Contains(out, `hero       class="hero" footer="config"`) {
		t.Errorf("kinds:\n%s %v", out, err)
	}
}

func TestThemes(t *testing.T) {
	dir, _ := fixture(t)
	out, _, err := run(t, "themes", "--theme-set", filepath.Join(dir, "mine.css"))
	if err != nil || !strings.Contains(out, "mine") || !strings.Contains(out, "gaia") {
		t.Errorf("themes:\n%s %v", out, err)
	}
	if _, _, err := run(t, "themes", "--theme-set", filepath.Join(dir, "missing.css")); err == nil {
		t.Error("a missing theme file fails")
	}
}

func TestNoConfig(t *testing.T) {
	_, deckPath := fixture(t)
	_, _, err := run(t, "build", "--no-config", "-o", "-", deckPath)
	if err == nil || !strings.Contains(err.Error(), "deck.md:6:") || !strings.Contains(err.Error(), `"who"`) {
		t.Errorf("--no-config ignores slides.yaml, so .who is missing at line 6: %v", err)
	}
}

func TestUsageAndErrors(t *testing.T) {
	out, _, err := run(t)
	if err != nil || !strings.Contains(out, "Usage:") {
		t.Errorf("no argument prints help: %v", err)
	}
	for _, args := range [][]string{{"version"}, {"--version"}} {
		if out, _, err := run(t, args...); err != nil || !strings.HasPrefix(out, "slides ") {
			t.Errorf("%v: %q %v", args, out, err)
		}
	}
	for _, args := range [][]string{
		{"build", "a.md", "b.md"}, {"--bogus"}, {"build", "--set", "noequals", "x.md"}, {"missing.md"}, {"expand"},
	} {
		if _, _, err := run(t, args...); err == nil {
			t.Errorf("%v: want an error", args)
		}
	}
}

func TestExport(t *testing.T) {
	dir := t.TempDir()
	deckPath := write(t, dir, "talk.md", "---\ntitle: T\n---\n# One\n\n---\n\n# Two\n")
	cases := []struct {
		args  []string
		files []string
		magic string
	}{
		{[]string{deckPath, "--pdf"}, []string{"talk.pdf"}, "%PDF-"},
		{[]string{deckPath, "-o", filepath.Join(dir, "out", "deck.pdf")}, []string{"out/deck.pdf"}, "%PDF-"},
		{[]string{deckPath, "--images", "png", "-o", filepath.Join(dir, "img", "s")}, []string{"img/s.001.png", "img/s.002.png"}, "\x89PNG"},
		{[]string{deckPath, "--image", "jpeg"}, []string{"talk.jpg"}, "\xff\xd8"},
		{[]string{deckPath, "-o", filepath.Join(dir, "first.png")}, []string{"first.png"}, "\x89PNG"},
		{[]string{deckPath, "--pptx"}, []string{"talk.pptx"}, "PK"},
		{[]string{deckPath, "--odp"}, []string{"talk.odp"}, "PK"},
		{[]string{deckPath, "-o", filepath.Join(dir, "deck.odp"), "--image-scale", "1"}, []string{"deck.odp"}, "PK"},
		{[]string{deckPath, "-o", filepath.Join(dir, "deck.pptx"), "--image-scale", "1"}, []string{"deck.pptx"}, "PK"},
	}
	for _, c := range cases {
		if _, stderr, err := run(t, c.args...); err != nil {
			t.Fatalf("%v: %v %s", c.args, err, stderr)
		}
		for _, f := range c.files {
			b, err := os.ReadFile(filepath.Join(dir, f))
			if err != nil || !strings.HasPrefix(string(b), c.magic) {
				t.Errorf("%v: %s missing or not %q: %v", c.args, f, c.magic, err)
			}
		}
	}
	out, _, err := run(t, deckPath, "--pdf", "-o", "-")
	if err != nil || !strings.HasPrefix(out, "%PDF-") {
		t.Errorf("-o - writes the PDF to standard output: %v", err)
	}
	// --pdf draws vectors: the text is in an embedded font. --pdf-raster
	// (which alone chooses a PDF) draws each page as one image, no font.
	raster, _, err := run(t, deckPath, "--pdf-raster", "-o", "-")
	if err != nil || !strings.HasPrefix(raster, "%PDF-") {
		t.Fatalf("--pdf-raster: %v", err)
	}
	fonts := func(s string) int {
		r, err := pdf.NewReader(strings.NewReader(s), int64(len(s)))
		if err != nil {
			t.Fatal(err)
		}
		return len(r.Page(1).V.Key("Resources").Key("Font").Keys())
	}
	if fonts(raster) != 0 || fonts(out) == 0 {
		t.Error("--pdf must embed the fonts its text is set in, --pdf-raster none")
	}
	// --pptx and --odp are editable: text boxes; their -raster flags (which
	// alone choose the format) are one picture a slide.
	for _, c := range []struct {
		flag, textBox string
		editable      bool
	}{
		{"--pptx", `txBox="1"`, true}, {"--pptx-raster", `txBox="1"`, false},
		{"--odp", `<draw:text-box>`, true}, {"--odp-raster", `<draw:text-box>`, false},
	} {
		data, _, err := run(t, deckPath, c.flag, "-o", "-", "--image-scale", "1")
		if err != nil || !strings.HasPrefix(data, "PK") {
			t.Fatalf("%s: %v", c.flag, err)
		}
		zr, err := zip.NewReader(strings.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, f := range zr.File {
			if f.Name != "ppt/slides/slide1.xml" && f.Name != "content.xml" {
				continue
			}
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			found = strings.Contains(string(b), c.textBox)
		}
		if found != c.editable {
			t.Errorf("%s: text boxes %v, want %v", c.flag, found, c.editable)
		}
	}
	for _, args := range [][]string{
		{deckPath, "--pdf", "--images", "png"},
		{deckPath, "--pdf-raster", "--pptx"},
		{deckPath, "--pptx-raster", "--odp"},
		{deckPath, "--images", "gif"},
		{deckPath, "--image", "tiff"},
	} {
		if _, _, err := run(t, args...); err == nil {
			t.Errorf("%v: want an error", args)
		}
	}
}

func TestMainExitsOnError(t *testing.T) {
	code := 0
	osExit = func(c int) { code = c }
	defer func() { osExit = os.Exit }()
	args := os.Args
	os.Args = []string{"slides", "missing.md"}
	defer func() { os.Args = args }()
	stderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	main()
	w.Close()
	os.Stderr = stderr
	var b bytes.Buffer
	_, _ = b.ReadFrom(r)
	if code != 1 || !strings.Contains(b.String(), "slides: open missing.md") {
		t.Errorf("exit %d, stderr %q", code, b.String())
	}
}
