package glob

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestFilesOracle compares what the patterns find with what marp-cli
// converted for the same patterns on the same tree (testdata/oracle/gen-cli.js
// writes globs.json): the outputs are the inputs with .html for their
// extension.
func TestFilesOracle(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "oracle", "globs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var o struct {
		Tree  []string
		Globs []struct {
			Pattern string
			Failed  bool
			Outputs []string
		}
	}
	if err := json.Unmarshal(b, &o); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, f := range o.Tree {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("# x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	for _, g := range o.Globs {
		t.Run(g.Pattern, func(t *testing.T) {
			files, err := Files(g.Pattern)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, f := range files {
				f = filepath.ToSlash(filepath.Clean(f))
				got = append(got, strings.TrimSuffix(f, filepath.Ext(f))+".html")
			}
			slices.Sort(got)
			if !slices.Equal(got, nilIfEmpty(g.Outputs)) && !(len(got) == 0 && len(g.Outputs) == 0) {
				t.Errorf("found\n %q\nmarp-cli converts\n %q", got, g.Outputs)
			}
		})
	}
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

func TestMatch(t *testing.T) {
	for _, c := range []struct {
		pattern, name string
		want          bool
	}{
		{"*.md", "a.md", true},
		{"*.md", "sub/a.md", false},
		{"**/*.md", "a.md", true},
		{"**/*.md", "x/y/a.md", true},
		{"**/*.md", ".hid/a.md", false},
		{"**/*.md", "node_modules/a.md", false},
		{"**/*.md", "x/.dot.md", false},
		{".*/*.md", ".hid/a.md", true},
		{"a/**", "a/b/c.txt", true},
		{"a/**/b", "a/b", true},
		{"a/**/b", "a/x/y/b", true},
		{"{a,b}/*.{md,txt}", "b/z.txt", true},
		{"{a,{b,c}}.md", "c.md", true},
		{"{a}.md", "{a}.md", true}, // no comma: literal
		{"[ab].md", "c.md", false},
		{"?.md", "ab.md", false},
		{"**/**/x", "p/q/x", true},
	} {
		if got := Match(c.pattern, c.name); got != c.want {
			t.Errorf("Match(%q, %q) = %v", c.pattern, c.name, got)
		}
	}
}

func TestExpandBound(t *testing.T) {
	p := strings.Repeat("{a,b}", 9) // 512 alternatives
	if _, err := Expand(p); err != ErrTooMany {
		t.Errorf("512 alternatives: %v", err)
	}
	got, err := Expand("x{a,b}{c,d}")
	if err != nil || !slices.Equal(got, []string{"xac", "xad", "xbc", "xbd"}) {
		t.Errorf("Expand = %q, %v", got, err)
	}
	for _, lit := range []string{"{", "a{b", "a}b", "{a}", "{,"} {
		if g, err := Expand(lit); err != nil || len(g) != 1 {
			t.Errorf("Expand(%q) = %q, %v", lit, g, err)
		}
	}
}

func FuzzMatch(f *testing.F) {
	f.Add("**/*.md", "a/b.md")
	f.Add("{a,{b,c}}/**/[x-z]?", "b/q/r/yz")
	f.Add("{{{{{{{{{{{{", "}}}}}")
	f.Fuzz(func(t *testing.T, pattern, name string) {
		_ = Match(pattern, name)
		// a pattern with nothing to expand matches exactly itself
		if !Magic(pattern) && !strings.Contains(pattern, `\`) && !strings.HasPrefix(pattern, ".") && pattern != "" {
			if !Match(pattern, pattern) && !strings.Contains(pattern, "/.") && !strings.Contains(pattern, "node_modules") && !strings.Contains(pattern, "//") {
				t.Fatalf("%q does not match itself", pattern)
			}
		}
	})
}
