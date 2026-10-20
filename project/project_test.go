package project

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-slides/slides"
	"github.com/go-slides/slides/deck"
	"github.com/go-slides/slides/vfs"
)

// root is an absolute directory that exists nowhere: everything below is
// read from a vfs.Map, so a test that reached the disk would fail.
var root = filepath.FromSlash("/nonexistent-go-slides-project")

func p(parts ...string) string { return filepath.Join(append([]string{root}, parts...)...) }

func TestLoadFromMap(t *testing.T) {
	m := vfs.NewMap(map[string][]byte{
		p("slides.yaml"):          []byte("kinds: shared/kinds.yaml\nthemes: [themes]\ndata: {who: World}\nlang: fr\n"),
		p("shared", "kinds.yaml"): []byte("intro:\n  class: lead\n  paginate: false\n"),
		p("themes", "corp.css"):   []byte("/* @theme corp */\nsection { color: red }\n"),
		p("themes", "README"):     []byte("not a theme"),
		p("parts", "greeting.md"): []byte("Hello {{ .who }}"),
	})
	o := Options{FS: m}
	if got := ConfigPath(o, root); got != p("slides.yaml") {
		t.Fatalf("ConfigPath = %q", got)
	}
	cfg, err := Load(p("deck.md"), o)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Lang != "fr" || !cfg.Themes.Has("corp") {
		t.Fatalf("lang %q, themes %v", cfg.Lang, cfg.Themes.Names())
	}
	if _, ok := cfg.Parse.Kinds["intro"]; !ok {
		t.Fatalf("kinds %v", cfg.Parse.Kinds.Names())
	}

	src := "---\ntheme: corp\n---\n\n<!-- _kind: intro -->\n\n{{ include \"parts/greeting.md\" }}\n"
	page, b, err := slides.HTML([]byte(src), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page, "Hello World") || b.Theme.Name != "corp" {
		t.Fatalf("include or theme lost: theme %q", b.Theme.Name)
	}
	if b.Deck.Slides[0].Kind != "intro" {
		t.Fatalf("kind %q", b.Deck.Slides[0].Kind)
	}
}

func TestLoadHCLConfigFromMap(t *testing.T) {
	m := vfs.NewMap(map[string][]byte{
		p("slides.hcl"): []byte("data = { n = 3 }\nkind \"big\" {\n  class = \"big\"\n  footer = file(\"foot.txt\")\n}\n"),
		p("foot.txt"):   []byte("the footer"),
	})
	cfg, err := Load(p("deck.md"), Options{FS: m})
	if err != nil {
		t.Fatal(err)
	}
	k := cfg.Parse.Kinds["big"]
	if k == nil || k.Dirs.Get("footer") != "the footer" {
		t.Fatalf("kind big = %+v", k)
	}
	if cfg.Parse.Data["n"] != 3 {
		t.Fatalf("data %v", cfg.Parse.Data)
	}
}

// TestMapRecordsWhatTheBuildWanted is the editor's loop: a build over an
// empty Map fails, and says which files it would have read.
func TestMapRecordsWhatTheBuildWanted(t *testing.T) {
	m := vfs.NewMap(map[string][]byte{p("slides.yaml"): []byte("kinds: k.yaml\n")})
	_, err := Load(p("deck.md"), Options{FS: m})
	if err == nil {
		t.Fatal("a missing kinds file did not fail")
	}
	if !slices.Contains(m.Missing(), p("k.yaml")) {
		t.Fatalf("Missing = %v", m.Missing())
	}

	// A template include outside the Map is not read from the disk.
	m2 := vfs.NewMap(nil)
	cfg, err := Load(p("deck.md"), Options{FS: m2})
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs("project.go")
	_, err = deck.Parse([]byte(`{{ include "`+filepath.ToSlash(abs)+`" }}`), cfg.Parse)
	if err == nil {
		t.Fatal("include read the disk through a Map")
	}
	if !slices.Contains(m2.Missing(), abs) {
		t.Fatalf("Missing = %v", m2.Missing())
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]map[string][]byte{
		"bad yaml":       {p("slides.yaml"): []byte("kinds: [")},
		"bad hcl attr":   {p("slides.hcl"): []byte("nope = 1\n")},
		"bad hcl block":  {p("slides.hcl"): []byte("block {}\n")},
		"bad themes":     {p("slides.hcl"): []byte("themes = 3\n")},
		"bad html":       {p("slides.hcl"): []byte("html = \"x\"\n")},
		"bad lang":       {p("slides.hcl"): []byte("lang = 1\n")},
		"bad theme list": {p("slides.hcl"): []byte("themes = [1]\n")},
		"bad kinds":      {p("slides.yaml"): []byte("kinds: 3\n")},
		"bad kinds list": {p("slides.yaml"): []byte("kinds: [3]\n")},
		"bad data":       {p("slides.yaml"): []byte("data: 3\n")},
		"missing theme":  {p("slides.yaml"): []byte("themes: [nope.css]\n")},
		"bad theme css":  {p("slides.yaml"): []byte("themes: [t.css]\n"), p("t.css"): []byte("section{}")},
		"bad hcl syntax": {p("slides.hcl"): []byte("= =\n")},
		"bad hcl kind":   {p("slides.hcl"): []byte("kind \"a\" {\n nope = 1\n}\n")},
		"bad kinds file": {p("slides.yaml"): []byte("kinds: [k.yaml]\n"), p("k.yaml"): []byte("a: 3\n")},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(p("deck.md"), Options{FS: vfs.NewMap(files)}); err == nil {
				t.Fatal("no error")
			}
		})
	}
	if _, err := Load(p("deck.md"), Options{FS: vfs.NewMap(nil), Sets: []string{"novalue"}}); err == nil {
		t.Fatal("--set without = accepted")
	}
	if _, err := Load(p("deck.md"), Options{FS: vfs.NewMap(nil), ConfigFile: p("gone.yaml")}); err == nil {
		t.Fatal("missing --config accepted")
	}
}

func TestLoadOptions(t *testing.T) {
	m := vfs.NewMap(map[string][]byte{
		p("slides.yaml"): []byte("html: true\nkinds: {a: {class: x}}\ndata: [d1.yaml, d2.json]\n"),
		p("d1.yaml"):     []byte("x: 1\n"),
		p("d2.json"):     []byte(`{"y": 2}`),
		p("k.yaml"):      []byte("b:\n  class: y\n"),
		p("t", "t.css"):  []byte("/* @theme tt */\n"),
		p("v.yaml"):      []byte("z: 3\n"),
	})
	cfg, err := Load(p("deck.md"), Options{
		FS: m, KindFiles: []string{p("k.yaml")}, ThemeSets: []string{p("t")},
		DataFiles: []string{p("v.yaml")}, Sets: []string{"w=4"}, Lang: "de", Bare: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTML != deck.HTMLAll || cfg.Lang != "de" || !cfg.Bare || !cfg.Themes.Has("tt") {
		t.Fatalf("cfg %+v", cfg)
	}
	for _, k := range []string{"x", "y", "z", "w"} {
		if _, ok := cfg.Parse.Data[k]; !ok {
			t.Fatalf("data %v lacks %s", cfg.Parse.Data, k)
		}
	}
	if cfg.Parse.Kinds["a"] == nil || cfg.Parse.Kinds["b"] == nil {
		t.Fatalf("kinds %v", cfg.Parse.Kinds.Names())
	}
	if c, err := Load(p("deck.md"), Options{FS: m, NoConfig: true, HTML: true}); err != nil || c.Lang != "" || c.HTML != deck.HTMLAll {
		t.Fatalf("NoConfig: %+v %v", c, err)
	}
	if ConfigPath(Options{NoConfig: true}, root) != "" {
		t.Fatal("NoConfig found a file")
	}
}
