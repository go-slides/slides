package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// oracleCase is one deck and command line of testdata/oracle/cli.json with
// what marp-cli 4.5.1 wrote into the head of the page. A null is a tag marp
// left out.
type oracleCase struct {
	Name        string
	MD          string
	Args        []string
	Title       *string
	Description *string
	Author      *string
	Keywords    *string
	OgURL       *string
	OgImage     *string
	Canonical   *string
}

func field(head, re string) *string {
	m := regexp.MustCompile(re).FindStringSubmatch(head)
	if m == nil {
		return nil
	}
	return &m[1]
}

func same(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func show(s *string) string {
	if s == nil {
		return "(absent)"
	}
	return *s
}

// TestCLIOracle pins the page metadata, with and without the command-line
// flags, to what marp-cli writes.
func TestCLIOracle(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "oracle", "cli.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []oracleCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			dir := t.TempDir()
			md := write(t, dir, c.Name+".md", c.MD)
			out := filepath.Join(dir, "out.html")
			args := append([]string{"build", md, "-o", out}, c.Args...)
			if _, stderr, err := run(t, args...); err != nil {
				t.Fatal(err, stderr)
			}
			page, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			head, _, _ := strings.Cut(string(page), "</head>")
			meta := func(attr, key string) *string {
				return field(head, `<meta `+attr+`="`+key+`" content="([^"]*)"`)
			}
			got := oracleCase{
				Title:       field(head, `<title>([^<]*)</title>`),
				Description: meta("name", "description"),
				Author:      meta("name", "author"),
				Keywords:    meta("name", "keywords"),
				OgURL:       meta("property", "og:url"),
				OgImage:     meta("property", "og:image"),
				Canonical:   field(head, `<link rel="canonical" href="([^"]*)"`),
			}
			for _, f := range []struct {
				name      string
				got, want *string
			}{
				{"title", got.Title, c.Title},
				{"description", got.Description, c.Description},
				{"author", got.Author, c.Author},
				{"keywords", got.Keywords, c.Keywords},
				{"og:url", got.OgURL, c.OgURL},
				{"og:image", got.OgImage, c.OgImage},
				{"canonical", got.Canonical, c.Canonical},
			} {
				if !same(f.got, f.want) {
					t.Errorf("%s = %s, marp-cli writes %s", f.name, show(f.got), show(f.want))
				}
			}
		})
	}
}

func TestThemeFlag(t *testing.T) {
	dir := t.TempDir()
	md := write(t, dir, "d.md", "---\ntheme: gaia\n---\n\n# H\n")
	css := write(t, dir, "mine.css", "/* @theme mine */\nsection { background: #123456; }")

	page := func(args ...string) string {
		t.Helper()
		out := filepath.Join(dir, "o.html")
		stdout, stderr, err := run(t, append([]string{"build", md, "-o", out}, args...)...)
		if err != nil {
			t.Fatal(err, stdout, stderr)
		}
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if p := page(); strings.Contains(p, "#123456") || !strings.Contains(p, `data-theme="gaia"`) {
		t.Error("without --theme the deck's theme applies")
	}
	// a CSS file replaces the deck's theme, whatever name it declares
	if p := page("--theme", css); !strings.Contains(p, "#123456") || strings.Contains(p, `data-theme="gaia"`) {
		t.Error("--theme file.css should be the theme of the deck")
	}
	// anything that is not a file is a theme name
	if p := page("--theme", "uncover"); !strings.Contains(p, `data-theme="uncover"`) {
		t.Error("--theme uncover should name the built-in theme")
	}
	// a directory is what --theme-set is for
	if _, _, err := run(t, "build", md, "-o", filepath.Join(dir, "o.html"), "--theme", dir); err == nil {
		t.Error("--theme dir should be refused")
	}
}
