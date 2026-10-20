package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// inDir makes the files of a tree and moves the working directory to cwd in
// it, with HOME at its root so that the search for a configuration file stops
// there. It returns the root.
func inTree(t *testing.T, files map[string]string, cwd string) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wd := filepath.Join(root, filepath.FromSlash(cwd))
	if err := os.MkdirAll(wd, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Chdir(wd)
	return root
}

// TestMarpConfigOracle compares what the configuration file does to the page
// with marp-cli's (testdata/oracle/gen-cli.js): where the file is found, the
// command line over it, --config-file, --no-config-file.
func TestMarpConfigOracle(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "oracle", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name                                                                  string
		Files                                                                 map[string]string
		Cwd                                                                   string
		Args                                                                  []string
		Title, Author, Description, Keywords, Canonical, OgImage, Lang, Theme *string
	}
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	// the page of marp-cli carries the locale as its language when nothing
	// says otherwise; here, nothing
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			inTree(t, c.Files, c.Cwd)
			os.WriteFile("deck.md", []byte("# H\n\n---\n\n# I\n"), 0o644)
			args := append([]string{"build", "deck.md", "-o", "out.html"}, c.Args...)
			if _, stderr, err := run(t, args...); err != nil {
				t.Fatal(err, stderr)
			}
			page, err := os.ReadFile("out.html")
			if err != nil {
				t.Fatal(err)
			}
			head, _, _ := strings.Cut(string(page), "</head>")
			meta := func(attr, key string) *string {
				return field(head, `<meta `+attr+`="`+key+`" content="([^"]*)"`)
			}
			for _, f := range []struct {
				name      string
				got, want *string
			}{
				{"title", field(head, `<title>([^<]*)</title>`), c.Title},
				{"author", meta("name", "author"), c.Author},
				{"description", meta("name", "description"), c.Description},
				{"keywords", meta("name", "keywords"), c.Keywords},
				{"canonical", field(head, `<link rel="canonical" href="([^"]*)"`), c.Canonical},
				{"og:image", meta("property", "og:image"), c.OgImage},
				{"theme", field(string(page), `data-theme="([^"]*)"`), c.Theme},
			} {
				if !same(f.got, f.want) {
					t.Errorf("%s = %s, marp-cli writes %s", f.name, show(f.got), show(f.want))
				}
			}
			if c.Lang != nil && *c.Lang != "C" {
				if got := field(string(page), `<html lang="([^"]*)"`); !same(got, c.Lang) {
					t.Errorf("lang = %s, marp-cli writes %s", show(got), show(c.Lang))
				}
			}
		})
	}
}

func TestMarpConfigProgramsAreSaidNotRun(t *testing.T) {
	inTree(t, map[string]string{"marp.config.js": "module.exports = {title: 'from js'}", ".marprc.yaml": "title: from yaml\n"}, "")
	os.WriteFile("deck.md", []byte("# H\n"), 0o644)
	_, stderr, err := run(t, "build", "deck.md", "-o", "out.html")
	if err != nil {
		t.Fatal(err, stderr)
	}
	// .marprc.yaml comes before marp.config.js in cosmiconfig's order, and wins
	if page, _ := os.ReadFile("out.html"); !strings.Contains(string(page), "<title>from yaml</title>") {
		t.Errorf("the yaml file should be read:\n%.300s", page)
	}
	// with only the program there is nothing to read, and a warning
	os.Remove(".marprc.yaml")
	_, stderr, err = run(t, "build", "deck.md", "-o", "out.html")
	if err != nil || !strings.Contains(stderr, "marp.config.js is a program") {
		t.Errorf("a program: %v %q", err, stderr)
	}
	if page, _ := os.ReadFile("out.html"); strings.Contains(string(page), "from js") {
		t.Error("a JavaScript configuration was not run")
	}
}

func TestMarpConfigPathsAreRelativeToTheFile(t *testing.T) {
	inTree(t, map[string]string{
		"conf/.marprc.json": `{"inputDir":"../decks","output":"../site","themeSet":["themes"],"bespoke":{"osc":false,"progress":true},"parallel":1}`,
		"conf/themes/x.css": "/* @theme x */\nsection { color: red; }",
		"decks/a.md":        "---\ntheme: x\n---\n# A\n",
		"decks/sub/b.md":    "# B\n",
	}, "conf")
	if _, stderr, err := run(t, "build"); err != nil {
		t.Fatal(err, stderr)
	}
	root := filepath.Dir(mustGetwd(t))
	a, err := os.ReadFile(filepath.Join(root, "site", "a.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(a), "color: red") && !strings.Contains(string(a), "color:red") {
		t.Errorf("the theme set of the file is not used:\n%.300s", a)
	}
	if !strings.Contains(string(a), `{"osc":false,"progress":true}`) {
		t.Error("bespoke options of the file are not used")
	}
	if !exists(filepath.Join(root, "site", "sub", "b.html")) {
		t.Error("inputDir and output of the file mirror the tree")
	}
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return wd
}

func TestMarpConfigCommandLineWins(t *testing.T) {
	inTree(t, map[string]string{
		".marprc.yaml": "title: file\nthemeSet: [one]\nbespoke: {transition: false}\nparallel: 3\nhtml: true\n",
		"one/a.css":    "/* @theme a */",
		"two/b.css":    "/* @theme b */",
	}, "")
	os.WriteFile("deck.md", []byte("---\ntheme: b\n---\n# H\n"), 0o644)
	if _, stderr, err := run(t, "build", "deck.md", "-o", "out.html", "--theme-set", "two", "--title", "flag"); err != nil {
		t.Fatal(err, stderr)
	}
	page, _ := os.ReadFile("out.html")
	if !strings.Contains(string(page), "<title>flag</title>") {
		t.Error("--title wins")
	}
	// the command line gave --theme-set: the file's is not added to it
	if strings.Contains(string(page), "theme \"b\" is not known") {
		t.Error("theme b is from two")
	}
}

func TestMarpConfigErrorsAndIgnoredKeys(t *testing.T) {
	inTree(t, map[string]string{
		"bad.json": `{"title": `,
		"ok.yaml":  "title: ok\nengine: ./engine.js\nbrowser: firefox\nnonsense: 1\ntitle2: x\nhtml: {a: [href]}\nkeywords: 5\n",
		"seq.yaml": "- a\n- b\n",
		"deck.md":  "# H\n",
	}, "")
	if _, _, err := run(t, "build", "deck.md", "--config-file", "bad.json"); err == nil || !strings.Contains(err.Error(), "bad.json") {
		t.Errorf("bad JSON: %v", err)
	}
	if _, _, err := run(t, "build", "deck.md", "--config-file", "missing.json"); err == nil || !strings.Contains(err.Error(), "could not find or parse configuration file") {
		t.Errorf("a missing file: %v", err)
	}
	if _, _, err := run(t, "build", "deck.md", "-c", "seq.yaml"); err == nil {
		t.Error("a list is not a configuration")
	}
	_, stderr, err := run(t, "build", "deck.md", "-o", "o.html", "-c", "ok.yaml", "--browser", "chrome", "--browser-timeout", "5")
	if err != nil {
		t.Fatal(err, stderr)
	}
	for _, want := range []string{`"engine" has no meaning here`, `"browser" has no meaning here`, `"nonsense" is not an option`, `"html": a list of allowed tags`, `"keywords": want a string or a list`, "--browser is ignored", "--browser-timeout is ignored"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("no warning %q in:\n%s", want, stderr)
		}
	}
	if page, _ := os.ReadFile("o.html"); !strings.Contains(string(page), "<title>ok</title>") {
		t.Error("the rest of the file is read")
	}
}

func TestTemplateAndBespokeFlags(t *testing.T) {
	dir := t.TempDir()
	md := write(t, dir, "d.md", "<!-- transition: fade -->\n\n# A\n\n---\n\n# B\n")
	page := func(args ...string) string {
		t.Helper()
		out := filepath.Join(dir, "o.html")
		if _, stderr, err := run(t, append([]string{"build", md, "-o", out}, args...)...); err != nil {
			t.Fatal(err, stderr)
		}
		b, _ := os.ReadFile(out)
		return string(b)
	}
	if p := page(); !strings.Contains(p, `{"osc":true,"progress":false}`) || !strings.Contains(p, "startViewTransition") {
		t.Error("marp-cli's defaults: the controller, no progress bar, transitions")
	}
	if p := page("--bespoke.progress", "--bespoke.osc=false", "--bespoke.transition=false"); !strings.Contains(p, `{"osc":false,"progress":true}`) || strings.Contains(p, "startViewTransition") {
		t.Error("the bespoke flags")
	}
	if p := page("--no-bespoke.osc", "--no-bespoke.transition"); !strings.Contains(p, `{"osc":false,"progress":false}`) || strings.Contains(p, "startViewTransition") {
		t.Error("marp-cli's --no-bespoke.* spellings")
	}
	if p := page("--template", "bare"); strings.Contains(p, "<script>") || !strings.Contains(p, "data-marpit-svg") {
		t.Error("--template bare is --bare: no script")
	}
	if _, _, err := run(t, "build", md, "-o", filepath.Join(dir, "x.html"), "--template", "fancy"); err == nil || !strings.Contains(err.Error(), "bespoke or bare") {
		t.Errorf("--template fancy: %v", err)
	}
	// a deck with no transition does not carry the script for them
	md2 := write(t, dir, "e.md", "# A\n")
	out := filepath.Join(dir, "e.html")
	if _, _, err := run(t, "build", md2, "-o", out); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); strings.Contains(string(b), "startViewTransition") {
		t.Error("no transition, no transition script")
	}
}

func TestCompatibilityFlags(t *testing.T) {
	dir := t.TempDir()
	md := write(t, dir, "d.md", "# A\n\n<!-- note -->\n")
	out := filepath.Join(dir, "d.pptx")
	if _, stderr, err := run(t, "build", md, "--pptx-editable", "-o", out); err != nil {
		t.Fatal(err, stderr)
	}
	if b, _ := os.ReadFile(out); !strings.HasPrefix(string(b), "PK") {
		t.Error("--pptx-editable writes the PowerPoint")
	}
	if _, stderr, err := run(t, "build", md, "--no-parallel", "--no-config-file", "-o", filepath.Join(dir, "p.html")); err != nil {
		t.Fatal(err, stderr)
	}
}

func FuzzMarpConfig(f *testing.F) {
	f.Add(`{"title":"x","keywords":["a"],"bespoke":{"osc":false},"pdfOutlines":{"pages":false},"themeSet":"t"}`, "x.json")
	f.Add("title: x\nparallel: true\nhtml: {a: [b]}\n", ".marprc")
	f.Add(`{"marp":{"title":1}}`, "package.json")
	f.Add("- a\n", ".marprc")
	f.Fuzz(func(t *testing.T, content, name string) {
		m, err := parseMarpConfig([]byte(content), name)
		if err != nil || m == nil {
			return
		}
		as, warns := marpConfigFlags(m, "/conf")
		_ = warns
		for _, a := range as {
			if a.flag == "" || strings.ContainsAny(a.flag, " =") {
				t.Fatalf("flag %q from %q", a.flag, content)
			}
		}
		// the paths of the file stay under its directory unless absolute
		for _, a := range as {
			if slices.Contains([]string{"input-dir", "output", "theme-set"}, a.flag) && a.value != "-" && !filepath.IsAbs(a.value) {
				t.Fatalf("%s = %q is not absolute", a.flag, a.value)
			}
		}
	})
}
