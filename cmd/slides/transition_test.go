package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var (
	scopeKey  = regexp.MustCompile(`data-marpit-scope-[A-Za-z0-9]+`)
	keyedName = regexp.MustCompile(`(&quot;(?:foo|fade))-[A-Za-z0-9]+(&quot;)`)
)

// TestTransitionOracle compares the slides' <section> tags, the speaker notes
// and the built-in keyframes a page carries with what marp-cli writes for the
// transition directive (testdata/oracle/gen-cli.js).
func TestTransitionOracle(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "oracle", "transitions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, MD string
		Sections []string
		Builtins []string
		Notes    string
	}
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	section := regexp.MustCompile(`<section[^>]*>`)
	keyframes := regexp.MustCompile(`@keyframes marp-(?:incoming-|outgoing-)?transition-(?:backward-)?__builtin__([a-z-]+)`)
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			dir := t.TempDir()
			md := write(t, dir, c.Name+".md", c.MD)
			out := filepath.Join(dir, "o.html")
			if _, stderr, err := run(t, "build", md, "-o", out, "--lang", "C"); err != nil {
				t.Fatal(err, stderr)
			}
			page, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			// marp-cli draws the scope key at random, here it is the slide's;
			// what is compared is that it is the same in the attribute and in
			// the name of the transition
			norm := func(s string) string {
				s = scopeKey.ReplaceAllString(s, "data-marpit-scope-K")
				return keyedName.ReplaceAllString(s, "$1-K$2")
			}
			var got []string
			for _, m := range section.FindAllString(string(page), -1) {
				got = append(got, norm(m))
			}
			for i := range c.Sections {
				c.Sections[i] = norm(c.Sections[i])
			}
			if !slices.Equal(got, c.Sections) {
				t.Errorf("sections\n got %q\nwant %q", got, c.Sections)
			}
			var builtins []string
			for _, m := range keyframes.FindAllStringSubmatch(string(page), -1) {
				if !slices.Contains(builtins, m[1]) {
					builtins = append(builtins, m[1])
				}
			}
			if !slices.Equal(builtins, c.Builtins) {
				t.Errorf("built-in keyframes %q, marp-cli writes %q", builtins, c.Builtins)
			}
			stdout, stderr, err := run(t, "build", md, "--notes", "-o", "-")
			if err != nil {
				t.Fatal(err, stderr)
			}
			if stdout != c.Notes {
				t.Errorf("notes %q, marp-cli writes %q", strings.TrimSpace(stdout), strings.TrimSpace(c.Notes))
			}
		})
	}
}
