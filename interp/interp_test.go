package interp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExpandData(t *testing.T) {
	got, err := Expand("# {{ .title | upper }}\n{{ range .items }}\n---\n## {{ . }}\n{{ end }}", Options{
		Data: map[string]any{"title": "deck", "items": []any{"a", "b"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "# DECK\n\n---\n## a\n\n---\n## b\n"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestMissingKeyIsAnError(t *testing.T) {
	if _, err := Expand("{{ .nope }}", Options{Data: map[string]any{}}); err == nil {
		t.Fatal("a missing key must fail rather than print <no value>")
	}
}

func TestCodeIsLeftAlone(t *testing.T) {
	src := "{{ .x }} `{{ .y }}` and {{ printf `%s` .x }}\n```go\n{{ .Name }}\n```\n~~~~\n{{ .z }}\n~~~~\n> ```\n> {{ .q }}\n> ```\n"
	got, err := Expand(src, Options{Data: map[string]any{"x": "X"}})
	if err != nil {
		t.Fatal(err)
	}
	want := "X `{{ .y }}` and X\n```go\n{{ .Name }}\n```\n~~~~\n{{ .z }}\n~~~~\n> ```\n> {{ .q }}\n> ```\n"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

// TestErrorLineAfterCode: masking a code block keeps its line breaks, so an
// error after it names the line the author sees.
func TestErrorLineAfterCode(t *testing.T) {
	src := "a\n```\none\ntwo\nthree\n```\n{{ .nope }}\n"
	_, err := Expand(src, Options{Data: map[string]any{}, Name: "deck.md"})
	if err == nil || !strings.Contains(err.Error(), "deck.md:7:") {
		t.Fatalf("error %v, want line 7", err)
	}
}

func TestCodeOption(t *testing.T) {
	got, err := Expand("`{{ .x }}`", Options{Data: map[string]any{"x": "X"}, Code: true})
	if err != nil || got != "`X`" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestTemplateCanWrapAFence(t *testing.T) {
	src := "{{ if .show }}\n```\nkept {{ .secret }}\n```\n{{ end }}"
	got, err := Expand(src, Options{Data: map[string]any{"show": false}})
	if err != nil || got != "" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestUnclosedFenceRunsToEnd(t *testing.T) {
	got, err := Expand("```\n{{ .x }}\n", Options{Data: map[string]any{}})
	if err != nil || got != "```\n{{ .x }}\n" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestDelims(t *testing.T) {
	got, err := Expand("<< .x >> {{ .x }}", Options{Data: map[string]any{"x": "X"}, Delims: [2]string{"<<", ">>"}})
	if err != nil || got != "X {{ .x }}" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestSentinels(t *testing.T) {
	got, err := Expand("{{ page }}/{{ total }}", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !HasSentinel(got) {
		t.Fatal("page and total must stay unresolved until pagination")
	}
	if r := Resolve(got, 3, 9); r != "3/9" {
		t.Fatalf("got %q", r)
	}
	if Resolve("plain", 1, 1) != "plain" {
		t.Fatal("text without sentinels must pass through")
	}
}

func TestFuncs(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC) }
	cases := map[string]string{
		`{{ now | date "2006-01-02" }}`:                    "2026-10-17",
		`{{ title "hello wörld" }}`:                        "Hello Wörld",
		`{{ lower "AB" }}{{ trim "  c " }}`:                "abc",
		`{{ replace "a" "b" "aa" }}`:                       "bb",
		`{{ join ", " (split "," "a,b") }}`:                "a, b",
		`{{ join "-" (list 1 2) }}`:                        "1-2",
		`{{ default "x" "" }}{{ default "x" "y" }}`:        "xy",
		`{{ range seq 3 }}{{ . }}{{ end }}`:                "123",
		`{{ add 1 2 }}{{ sub 5 1 }}{{ mul 2 3 }}`:          "346",
		`{{ (dict "a" 1).a }}`:                             "1",
		`{{ (merge (dict "a" 1) (dict "a" 2)).a }}`:        "2",
		`{{ quote "a" }}`:                                  `"a"`,
		`{{ repeat 2 "ab" }}`:                              "abab",
		`{{ contains "b" "abc" }}{{ hasPrefix "a" "ab" }}`: "truetrue",
		`{{ hasSuffix "b" "ab" }}`:                         "true",
	}
	for src, want := range cases {
		got, err := Expand(src, Options{Now: clock})
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		if got != want {
			t.Errorf("%s: got %q want %q", src, got, want)
		}
	}
	for _, src := range []string{`{{ dict "a" }}`, `{{ dict 1 2 }}`, `{{ join "," 3 }}`} {
		if _, err := Expand(src, Options{}); err == nil {
			t.Errorf("%s: want an error", src)
		}
	}
}

func TestInclude(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "part.md"), []byte("part {{ .x }} `{{ .y }}`"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Expand(`{{ include "part.md" }}`, Options{Data: map[string]any{"x": "X"}, BaseDir: dir})
	if err != nil || got != "part X `{{ .y }}`" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := Expand(`{{ include "missing.md" }}`, Options{BaseDir: dir}); err == nil || !strings.Contains(err.Error(), "missing.md") {
		t.Fatalf("want a missing-file error, got %v", err)
	}
}
