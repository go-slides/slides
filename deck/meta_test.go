package deck

import (
	"slices"
	"strings"
	"testing"
)

func TestKeywordList(t *testing.T) {
	for _, c := range []struct {
		in   string
		want []string
	}{
		{"a, b,a,,c", []string{"a", "b", "c"}},
		{"", nil},
		{" , ", nil},
		{keywordSep + " c " + keywordSep + "a" + keywordSep + " c ", []string{" c ", "a"}},
		{keywordSep + "a,b", []string{"a,b"}},
	} {
		if got := KeywordList(c.in); !slices.Equal(got, c.want) {
			t.Errorf("KeywordList(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func FuzzKeywordList(f *testing.F) {
	f.Add("a, b,a")
	f.Add(keywordSep + "x" + keywordSep)
	f.Fuzz(func(t *testing.T, s string) {
		got := KeywordList(s)
		seen := map[string]bool{}
		for _, k := range got {
			if k == "" || seen[k] {
				t.Fatalf("KeywordList(%q) = %q: empty or repeated item", s, got)
			}
			seen[k] = true
		}
	})
}

func TestValidURL(t *testing.T) {
	for in, want := range map[string]bool{
		"https://example.org/x": true,
		"mailto:a@b.c":          true,
		"not a url":             false,
		"nope":                  false,
		"/relative":             false,
		"https://a b/":          false,
	} {
		if ValidURL(in) != want {
			t.Errorf("ValidURL(%q) = %v", in, !want)
		}
	}
}

func FuzzValidURL(f *testing.F) {
	f.Add("https://example.org")
	f.Add("%zz:")
	f.Fuzz(func(t *testing.T, s string) {
		if ValidURL(s) && strings.ContainsAny(s, " \t\r\n") {
			t.Fatalf("ValidURL(%q) accepts whitespace", s)
		}
	})
}

func TestDetectTitle(t *testing.T) {
	d, err := Parse([]byte("### c\n\n## B\n\n---\n\n## A\n\n---\n\n# **Top** `x`\n"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.DetectTitle(); got != "**Top** `x`" {
		t.Errorf("DetectTitle = %q", got)
	}
}
