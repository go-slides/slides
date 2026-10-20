package interp

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/go-slides/slides/vfs"
)

// TestExpandLinesKeepsTheOutput is the invariant: tracing lines never
// changes what a template writes.
func TestExpandLinesKeepsTheOutput(t *testing.T) {
	data := map[string]any{"x": "X", "items": []any{"a", "b"}}
	srcs := []string{
		"plain\ntext\n",
		"{{ .x }}\n{{ range .items }}\n- {{ . }}\n{{ end }}\n",
		"a\n{{- .x }}\nb\n  {{- .x -}}\n  c\n",
		"```go\n{{ .Name }}\nline\n```\n{{ .x }}\n",
		"{{ if\n  .x }}yes{{ end }}\nafter\n",
		"{{ \"}}\\n\" }}\nz\n",
		"{{/* a\ncomment */}}\nq\n",
		"`{{ .y }}` {{ printf `%s\n` .x }}tail\n",
		"no trailing newline",
		"",
	}
	for _, src := range srcs {
		want, err := Expand(src, Options{Data: data})
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		got, lines, err := ExpandLines(src, Options{Data: data})
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if got != want {
			t.Errorf("%q: ExpandLines wrote %q, Expand %q", src, got, want)
		}
		if n := strings.Count(got, "\n") + 1; len(lines) != n {
			t.Errorf("%q: %d line entries for %d lines", src, len(lines), n)
		}
		if strings.Contains(got, lineOpen) || strings.Contains(got, lineClose) {
			t.Errorf("%q: a mark survived", src)
		}
	}
}

func TestExpandLinesMap(t *testing.T) {
	dir := filepath.FromSlash("/nonexistent-go-slides-interp")
	fs := vfs.NewMap(map[string][]byte{filepath.Join(dir, "part.md"): []byte("p1\np2\np3")})
	src := "zero\n{{ include \"part.md\" }}\nafter include\n{{ range seq 2 }}\n# gen {{ . }}\n{{ end }}\nlast\n```\ncode\n```\nend\n"
	out, lines, err := ExpandLines(src, Options{BaseDir: dir, FS: fs})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(out, "\n")
	at := func(text string) int {
		i := slices.Index(got, text)
		if i < 0 {
			t.Fatalf("%q not in output %q", text, out)
		}
		return lines[i]
	}
	cases := map[string]int{
		"zero": 0, "p1": 1, "p2": 1, "p3": 1, // the include's lines are its line
		"after include": 2,
		"# gen 1":       4, "# gen 2": 4, // the range body, twice
		"last": 6, "```": 7, "code": 8, "end": 10,
	}
	for text, want := range cases {
		if l := at(text); l != want {
			t.Errorf("%q: line %d, want %d", text, l, want)
		}
	}
}

// FuzzExpandLines checks the invariant on any template: same output, same
// error, as Expand, and one map entry per output line.
func FuzzExpandLines(f *testing.F) {
	for _, s := range []string{
		"a\n{{- .x }}\nb\n  {{- .x -}}\n  c\n", "{{ range .items }}\n- {{ . }}\n{{ end }}",
		"```\n{{ .x }}\n```\n{{ .x }}", "{{ \"}}\" }}\n", "`a` {{ printf `}}` }}\n", "{{/*\n*/}}x",
	} {
		f.Add(s)
	}
	data := map[string]any{"x": "X", "items": []any{"a", "b"}}
	f.Fuzz(func(t *testing.T, src string) {
		want, err1 := Expand(src, Options{Data: data})
		got, lines, err2 := ExpandLines(src, Options{Data: data})
		if (err1 == nil) != (err2 == nil) {
			t.Fatalf("%q: Expand %v, ExpandLines %v", src, err1, err2)
		}
		if err1 != nil {
			return
		}
		if got != want {
			t.Fatalf("%q: ExpandLines %q, Expand %q", src, got, want)
		}
		if lines != nil && len(lines) != strings.Count(got, "\n")+1 {
			t.Fatalf("%q: %d entries for %d lines", src, len(lines), strings.Count(got, "\n")+1)
		}
	})
}

// TestExpandLinesErrorsAsExpand: an error reports Expand's position,
// columns included, not the marked template's.
func TestExpandLinesErrorsAsExpand(t *testing.T) {
	src := "a\nb {{ .nope }}\n"
	_, err1 := Expand(src, Options{Data: map[string]any{}, Name: "d.md"})
	_, _, err2 := ExpandLines(src, Options{Data: map[string]any{}, Name: "d.md"})
	if err1 == nil || err2 == nil || err1.Error() != err2.Error() {
		t.Fatalf("Expand: %v\nExpandLines: %v", err1, err2)
	}
}
